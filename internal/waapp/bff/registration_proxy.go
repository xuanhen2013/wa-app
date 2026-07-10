package bff

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/waapp/engine"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
	"github.com/byte-v-forge/wa-app/internal/waapp/wamodel"
)

const (
	registrationProxyModeDedicated  = "REGISTRATION_DEDICATED"
	registrationProxySource1024     = "REGISTRATION_1024PROXY"
	registrationProxyEndpoint1024   = "https://white.1024proxy.com/white/api"
	registrationProxyNodeRegion1024 = "Rand"
	registrationProxyEgressCheckURL = "https://api.country.is/"
)

// RegistrationProxyConfig is the runtime configuration for the dedicated WA
// registration egress. It intentionally contains no endpoint override: 1024
// protocol details belong to its source adapter rather than deployment config.
type RegistrationProxyConfig struct {
	Enabled               bool
	SourceOrder           string
	Fallback              string
	StickyMinutes         int
	SourceRetryMax        int
	Source1024Enabled     bool
	Source1024UsernameTpl string
	Source1024Password    string
}

func normalizeRegistrationProxyConfig(input RegistrationProxyConfig) RegistrationProxyConfig {
	input.SourceOrder = strings.TrimSpace(input.SourceOrder)
	if input.SourceOrder == "" {
		input.SourceOrder = "1024proxy"
	}
	input.Fallback = strings.ToLower(strings.TrimSpace(input.Fallback))
	if input.Fallback == "" {
		input.Fallback = "reject"
	}
	if input.StickyMinutes <= 0 {
		input.StickyMinutes = 30
	}
	if input.StickyMinutes > 120 {
		input.StickyMinutes = 120
	}
	if input.SourceRetryMax <= 0 {
		input.SourceRetryMax = 3
	}
	return input
}

type registrationProxyResolver struct {
	config        RegistrationProxyConfig
	now           func() time.Time
	sources       map[string]registrationProxySource
	egressChecker registrationProxyEgressChecker
}

// registrationProxySource owns one provider's node acquisition and credential
// rendering. The resolver only selects an enabled source and exposes a redacted
// WA route to the registration flow.
type registrationProxySource interface {
	name() string
	resolve(context.Context, string, string, int, time.Time) (wacore.WAProxyRoute, error)
}

// registrationProxyEgressChecker verifies that a provider-selected node really
// exits from the requested country before any WA request is sent through it.
type registrationProxyEgressChecker interface {
	check(context.Context, wacore.WAProxyRoute, string) error
}

type httpRegistrationProxyEgressChecker struct {
	endpoint string
	timeout  time.Duration
}

func newRegistrationProxyResolver(config RegistrationProxyConfig) *registrationProxyResolver {
	config = normalizeRegistrationProxyConfig(config)
	client := &http.Client{Timeout: 15 * time.Second}
	resolver := &registrationProxyResolver{
		config:        config,
		now:           func() time.Time { return time.Now().UTC() },
		sources:       map[string]registrationProxySource{},
		egressChecker: httpRegistrationProxyEgressChecker{endpoint: registrationProxyEgressCheckURL, timeout: 15 * time.Second},
	}
	if config.Source1024Enabled {
		source := new1024ProxySource(client, registrationProxyEndpoint1024, config.Source1024UsernameTpl, config.Source1024Password)
		resolver.sources[source.name()] = source
	}
	return resolver
}

func (r *registrationProxyResolver) enabled() bool {
	return r != nil && r.config.Enabled
}

func (r *registrationProxyResolver) resolve(ctx context.Context, countryCode string, sessionID string) (wacore.WAProxyRoute, error) {
	if r == nil || !r.enabled() {
		return wacore.WAProxyRoute{}, nil
	}
	countryCode = normalizeProxyCountryCode(countryCode)
	if countryCode == "" {
		return wacore.WAProxyRoute{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registration proxy country is required", false)
	}
	sessionID = registrationProxySessionID(sessionID)
	triedSource := false
	for _, sourceName := range registrationProxySourceOrder(r.config.SourceOrder) {
		source, ok := r.sources[sourceName]
		if !ok {
			continue
		}
		triedSource = true
		for attempt := 1; attempt <= r.config.SourceRetryMax; attempt++ {
			route, err := source.resolve(ctx, countryCode, sessionID, r.config.StickyMinutes, r.now())
			if err == nil && r.egressChecker != nil {
				err = r.egressChecker.check(ctx, route, countryCode)
			}
			if err == nil {
				return route, nil
			}
			if attempt < r.config.SourceRetryMax && !waitRegistrationProxyRetry(ctx, attempt) {
				return wacore.WAProxyRoute{}, ctx.Err()
			}
		}
	}
	if !triedSource {
		return r.fallbackRoute(countryCode, "registration proxy source is unavailable")
	}
	return r.fallbackRoute(countryCode, "registration proxy source request failed")
}

func (c httpRegistrationProxyEgressChecker) check(ctx context.Context, route wacore.WAProxyRoute, countryCode string) error {
	proxyURL, err := url.Parse(route.ProxyURL)
	if err != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
		return fmt.Errorf("registration proxy egress route is invalid")
	}
	endpoint, err := url.Parse(c.endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return fmt.Errorf("registration proxy egress check is unavailable")
	}
	timeout := c.timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client := &http.Client{Transport: transport, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create registration proxy egress check: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("registration proxy egress check failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("registration proxy egress check failed")
	}
	var result struct {
		Country string `json:"country"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<10)).Decode(&result); err != nil {
		return fmt.Errorf("registration proxy egress check returned invalid data")
	}
	if !strings.EqualFold(strings.TrimSpace(result.Country), countryCode) {
		return fmt.Errorf("registration proxy egress country mismatch")
	}
	return nil
}

func (r *registrationProxyResolver) fallbackRoute(countryCode string, message string) (wacore.WAProxyRoute, error) {
	if r != nil && r.config.Fallback == "direct" {
		return wacore.WAProxyRoute{ProxyMode: waProxyModeDirect, CountryCode: countryCode, Source: waProxySourceDirect, PolicyMode: registrationProxyModeDedicated}, nil
	}
	return wacore.WAProxyRoute{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, message, true)
}

func registrationProxySourceOrder(order string) []string {
	result := make([]string, 0, 1)
	for _, entry := range strings.Split(order, ",") {
		if name := strings.ToLower(strings.TrimSpace(entry)); name != "" {
			result = append(result, name)
		}
	}
	return result
}

func registrationProxySessionID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return shared.RandomIDGenerator{}.NewID("")[:8]
	}
	return shared.StableID(value)[:8]
}

func render1024Username(template string, countryCode string, sessionID string, stickyMinutes int) string {
	template = strings.TrimSpace(template)
	if template == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"{country}", countryCode,
		"{session_id}", sessionID,
		"{sticky_minutes}", strconv.Itoa(stickyMinutes),
		"${country}", countryCode,
		"${session_id}", sessionID,
		"${sticky_minutes}", strconv.Itoa(stickyMinutes),
	)
	return replacer.Replace(template)
}

func waitRegistrationProxyRetry(ctx context.Context, attempt int) bool {
	timer := time.NewTimer(time.Duration(attempt) * 250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

type proxy1024Source struct {
	client           *http.Client
	endpoint         string
	usernameTemplate string
	password         string
}

func new1024ProxySource(client *http.Client, endpoint string, usernameTemplate string, password string) *proxy1024Source {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &proxy1024Source{client: client, endpoint: endpoint, usernameTemplate: usernameTemplate, password: password}
}

func (*proxy1024Source) name() string { return "1024proxy" }

func (s *proxy1024Source) resolve(ctx context.Context, countryCode string, sessionID string, stickyMinutes int, now time.Time) (wacore.WAProxyRoute, error) {
	username := render1024Username(s.usernameTemplate, countryCode, sessionID, stickyMinutes)
	if username == "" || strings.TrimSpace(s.password) == "" {
		return wacore.WAProxyRoute{}, fmt.Errorf("1024proxy credentials are not configured")
	}
	endpoint, err := url.Parse(s.endpoint)
	if err != nil {
		return wacore.WAProxyRoute{}, fmt.Errorf("parse 1024proxy endpoint: %w", err)
	}
	query := endpoint.Query()
	// 1024proxy's white API returns an access relay. The requested exit country
	// is selected only by the authenticated proxy username, not this query.
	query.Set("region", registrationProxyNodeRegion1024)
	query.Set("num", "1")
	query.Set("time", strconv.Itoa(stickyMinutes))
	query.Set("format", "1")
	query.Set("type", "json")
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return wacore.WAProxyRoute{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return wacore.WAProxyRoute{}, fmt.Errorf("request 1024proxy node: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return wacore.WAProxyRoute{}, fmt.Errorf("request 1024proxy node returned HTTP %d", resp.StatusCode)
	}
	var nodes []proxy1024Node
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if err := decoder.Decode(&nodes); err != nil {
		return wacore.WAProxyRoute{}, fmt.Errorf("decode 1024proxy node: %w", err)
	}
	if len(nodes) == 0 {
		return wacore.WAProxyRoute{}, fmt.Errorf("1024proxy returned no usable node")
	}
	address, err := proxy1024NodeAddress(nodes[0])
	if err != nil {
		return wacore.WAProxyRoute{}, err
	}
	proxyURL := &url.URL{Scheme: "http", Host: address, User: url.UserPassword(username, s.password)}
	return wacore.WAProxyRoute{
		ProxyURL:    proxyURL.String(),
		ProxyMode:   registrationProxyModeDedicated,
		CountryCode: countryCode,
		Source:      registrationProxySource1024,
		PolicyMode:  registrationProxyModeDedicated,
		RouteID:     shared.StableID(strings.Join([]string{countryCode, sessionID, address}, ":")),
	}, nil
}

type proxy1024Node struct {
	Host string `json:"host"`
	Port string `json:"port"`
}

func proxy1024NodeAddress(node proxy1024Node) (string, error) {
	host := strings.TrimSpace(node.Host)
	if host == "" || strings.ContainsAny(host, " \t\r\n/@?#\\") {
		return "", fmt.Errorf("1024proxy returned an invalid node host")
	}
	port, err := strconv.Atoi(strings.TrimSpace(node.Port))
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("1024proxy returned an invalid node port")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func registrationProxyWaitFromRoute(route wacore.WAProxyRoute, expiresAt time.Time) *wamodel.RegistrationProxyWait {
	if route.ProxyMode != registrationProxyModeDedicated || strings.TrimSpace(route.ProxyURL) == "" {
		return nil
	}
	return &wamodel.RegistrationProxyWait{
		ProxyURL:      route.ProxyURL,
		ProxyMode:     route.ProxyMode,
		CountryCode:   route.CountryCode,
		Source:        route.Source,
		RouteID:       route.RouteID,
		ExpiresAtUnix: expiresAt.UTC().Unix(),
	}
}

func registrationProxyRouteFromWait(wait *wamodel.RegistrationProxyWait) (wacore.WAProxyRoute, error) {
	if wait == nil || strings.TrimSpace(wait.ProxyURL) == "" {
		return wacore.WAProxyRoute{}, fmt.Errorf("registration proxy wait is empty")
	}
	if wait.ExpiresAtUnix > 0 && time.Now().UTC().Unix() >= wait.ExpiresAtUnix {
		return wacore.WAProxyRoute{}, shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "registration proxy session expired; request OTP again", false)
	}
	return wacore.WAProxyRoute{
		ProxyURL:    wait.ProxyURL,
		ProxyMode:   wait.ProxyMode,
		CountryCode: wait.CountryCode,
		Source:      wait.Source,
		RouteID:     wait.RouteID,
		PolicyMode:  registrationProxyModeDedicated,
	}, nil
}

func (g *actionGateway) registrationRunner(ctx context.Context, payload map[string]any) (*engine.NativeEngine, wacore.WAProxyRoute, bool, error) {
	nativeEngine, err := g.nativeEngine()
	if err != nil {
		return nil, wacore.WAProxyRoute{}, false, err
	}
	if route, found, err := g.registrationProxyRouteFromOTPWait(ctx, payload); err != nil {
		return nil, wacore.WAProxyRoute{}, false, err
	} else if found {
		proxied, err := nativeEngine.WithProxyURL(route.ProxyURL)
		if err != nil {
			return nil, wacore.WAProxyRoute{}, false, err
		}
		return proxied, route, true, nil
	}
	if g.registrationProxy != nil && g.registrationProxy.enabled() {
		phone := wamodel.NormalizePhone(phoneFromAction(payload))
		route, err := g.registrationProxy.resolve(ctx, proxyCountryCodeFromPayload(payload), shared.FirstNonEmpty(phone.GetE164Number(), shared.TextField(payload, "wa_account_id")))
		if err != nil {
			return nil, wacore.WAProxyRoute{}, false, err
		}
		if route.ProxyURL == "" {
			return nativeEngine, route, false, nil
		}
		proxied, err := nativeEngine.WithProxyURL(route.ProxyURL)
		if err != nil {
			return nil, wacore.WAProxyRoute{}, false, err
		}
		return proxied, route, true, nil
	}
	route, useProxy := g.resolveWAProxyRoute(waProxyResolveRequest{
		Payload:     payload,
		CountryCode: proxyCountryCodeFromPayload(payload),
	})
	if !useProxy {
		return nativeEngine, route, false, nil
	}
	proxied, err := nativeEngine.WithProxyURL(route.ProxyURL)
	if err != nil {
		return nil, wacore.WAProxyRoute{}, false, err
	}
	return proxied, route, true, nil
}

func (g *actionGateway) registrationProxyRouteFromOTPWait(ctx context.Context, payload map[string]any) (wacore.WAProxyRoute, bool, error) {
	verificationRequestID := strings.TrimSpace(shared.TextField(payload, "verification_request_id"))
	if verificationRequestID == "" {
		return wacore.WAProxyRoute{}, false, nil
	}
	wait, err := g.loadRegistrationOTPWait(ctx, "", verificationRequestID)
	if err != nil {
		return wacore.WAProxyRoute{}, false, nil
	}
	if wait.RegistrationProxy == nil {
		return wacore.WAProxyRoute{}, false, nil
	}
	route, err := registrationProxyRouteFromWait(wait.RegistrationProxy)
	if err != nil {
		return wacore.WAProxyRoute{}, false, err
	}
	return route, true, nil
}

func (g *actionGateway) saveRegistrationProxyWait(ctx context.Context, waAccountID string, verificationRequestID string, route wacore.WAProxyRoute) error {
	if route.ProxyMode != registrationProxyModeDedicated || strings.TrimSpace(verificationRequestID) == "" {
		return nil
	}
	wait := wamodel.RegistrationOTPWait{
		WAAccountID:           waAccountID,
		VerificationRequestID: verificationRequestID,
		CreatedAtUnix:         time.Now().UTC().Unix(),
		RegistrationProxy:     g.registrationProxyWait(route),
	}
	return g.saveRegistrationOTPWait(ctx, wait, g.registrationProxyWaitTTL(registrationOTPWaitDefaultTTL))
}

func (g *actionGateway) registrationProxyWait(route wacore.WAProxyRoute) *wamodel.RegistrationProxyWait {
	if route.ProxyMode != registrationProxyModeDedicated {
		return nil
	}
	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	if g != nil && g.registrationProxy != nil {
		expiresAt = g.registrationProxy.now().Add(time.Duration(g.registrationProxy.config.StickyMinutes) * time.Minute)
	}
	return registrationProxyWaitFromRoute(route, expiresAt)
}

func (g *actionGateway) registrationProxyWaitTTL(fallback time.Duration) time.Duration {
	if g == nil || g.registrationProxy == nil || !g.registrationProxy.enabled() {
		return fallback
	}
	stickyTTL := time.Duration(g.registrationProxy.config.StickyMinutes+5) * time.Minute
	if stickyTTL > fallback {
		return stickyTTL
	}
	return fallback
}
