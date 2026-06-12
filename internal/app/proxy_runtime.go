package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type DynamicProxyRoute struct {
	AccountID   string
	RouteID     string
	LeaseID     string
	Username    string
	ProfileID   string
	ProxyURL    string
	ProxyMode   string
	CountryCode string
	ExpiresAt   time.Time
}

type DynamicProxySessionMode string

const (
	DynamicProxySessionModeRotating DynamicProxySessionMode = "rotating"
	DynamicProxySessionModeSticky   DynamicProxySessionMode = "sticky"
)

type DynamicProxyRouteRequest struct {
	Purpose       string
	CorrelationID string
	SessionID     string
	CountryCode   string
	TTL           time.Duration
	Mode          DynamicProxySessionMode
	ForceNew      bool
}

type DynamicProxyRuntime struct {
	baseURL       string
	gatewayScheme string
	client        *http.Client
	mu            sync.Mutex
	rules         []proxyIngressRuleSettings
	rulesExpireAt time.Time
}

type gatewayProxyRule struct {
	Username  string
	Password  string
	ProfileID string
}

type releaseProxyLeaseRequest struct {
	LeaseID string `json:"lease_id"`
}

type acquireProxyLeaseRequest struct {
	AccountID       string                        `json:"account_id"`
	Purpose         string                        `json:"purpose"`
	Policy          proxyLeaseSessionPolicy       `json:"policy"`
	ForceNew        bool                          `json:"force_new,omitempty"`
	SelectionPolicy proxyDynamicIPSelectionPolicy `json:"selection_policy"`
}

type proxyLeaseSessionPolicy struct {
	Mode         string            `json:"mode"`
	Region       string            `json:"region,omitempty"`
	StickyTTL    string            `json:"sticky_ttl,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	UpstreamKind string            `json:"upstream_kind"`
	RotationMode string            `json:"rotation_mode"`
}

type proxyDynamicIPSelectionPolicy struct {
	CountryCode string `json:"country_code,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	MaxAttempts uint32 `json:"max_attempts,omitempty"`
}

type acquireProxyLeaseResponse struct {
	Lease  proxyDynamicLease    `json:"lease"`
	Egress proxyRuntimeEndpoint `json:"egress"`
}

type proxyDynamicLease struct {
	LeaseID           string               `json:"lease_id"`
	AccountID         string               `json:"account_id"`
	Purpose           string               `json:"purpose"`
	ProviderAccountID string               `json:"provider_account_id"`
	Session           proxyRuntimeSession  `json:"session"`
	Egress            proxyRuntimeEndpoint `json:"egress"`
	Listener          proxyRuntimeListener `json:"listener"`
	ExpiresAt         string               `json:"expires_at"`
}

type proxyRuntimeSession struct {
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
}

type proxyRuntimeEndpoint struct {
	Protocol  string            `json:"protocol"`
	Host      string            `json:"host"`
	Port      uint32            `json:"port"`
	SessionID string            `json:"session_id"`
	Labels    map[string]string `json:"labels"`
}

type proxyRuntimeListener struct {
	ListenerID string            `json:"listener_id"`
	Labels     map[string]string `json:"labels"`
}

type proxyRuntimeSettingsResponse struct {
	Settings proxyRuntimeSettings `json:"settings"`
}

type proxyRuntimeSettings struct {
	IngressRules []proxyIngressRuleSettings `json:"ingress_rules"`
}

type proxyIngressRuleSettings struct {
	Enabled       bool   `json:"enabled"`
	Username      string `json:"username"`
	PasswordValue string `json:"password_value"`
	ProfileID     string `json:"profile_id"`
}

const (
	proxyRuntimeGatewayPort      = "10810"
	proxyRuntimeRequestTimeout   = 5 * time.Second
	proxyRuntimeSettingsCacheTTL = 5 * time.Minute
)

func NewDynamicProxyRuntime(baseURL string, gatewayProtocol string) *DynamicProxyRuntime {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &DynamicProxyRuntime{baseURL: baseURL, gatewayScheme: gatewayProxyScheme(gatewayProtocol), client: &http.Client{Timeout: 20 * time.Second}}
}

func (p *DynamicProxyRuntime) GatewayProxyRoute(ctx context.Context, username string, routeReq DynamicProxyRouteRequest) (DynamicProxyRoute, error) {
	rule, err := p.gatewayProxyRule(ctx, username)
	if err != nil {
		return DynamicProxyRoute{}, err
	}
	proxyURL, err := p.gatewayProxyURL(rule.Username, rule.Password)
	if err != nil {
		return DynamicProxyRoute{}, err
	}
	now := time.Now().UTC()
	expiresAt := time.Time{}
	if routeReq.TTL > 0 {
		expiresAt = now.Add(routeReq.TTL)
	}
	routeID := proxyRouteID(rule.Username, routeReq)
	accountID := routeID
	return DynamicProxyRoute{
		AccountID:   accountID,
		RouteID:     routeID,
		Username:    rule.Username,
		ProfileID:   rule.ProfileID,
		ProxyURL:    proxyURL,
		ProxyMode:   proxyRouteMode(routeReq.Mode),
		CountryCode: "US",
		ExpiresAt:   expiresAt,
	}, nil
}

func (p *DynamicProxyRuntime) GatewayProxyURL(ctx context.Context, username string) (string, error) {
	rule, err := p.gatewayProxyRule(ctx, username)
	if err != nil {
		return "", err
	}
	return p.gatewayProxyURL(rule.Username, rule.Password)
}

func (p *DynamicProxyRuntime) GatewayLeaseProxyRoute(ctx context.Context, username string, routeReq DynamicProxyRouteRequest) (DynamicProxyRoute, error) {
	rule, err := p.gatewayProxyRule(ctx, username)
	if err != nil {
		return DynamicProxyRoute{}, err
	}
	profileID := strings.TrimSpace(rule.ProfileID)
	if profileID == "" {
		return DynamicProxyRoute{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, fmt.Sprintf("proxy-runtime gateway user %q has no dynamic profile", username), true)
	}
	countryCode := proxyRuntimeCountryCode(routeReq.CountryCode)
	purpose := firstNonEmpty(strings.TrimSpace(routeReq.Purpose), "WA_REGISTRATION")
	sessionID := firstNonEmpty(strings.TrimSpace(routeReq.SessionID), proxyRouteID(rule.Username, routeReq))
	ttl := routeReq.TTL
	if ttl <= 0 {
		ttl = registrationProxyRouteTTL
	}
	leaseReq := acquireProxyLeaseRequest{
		AccountID: profileID,
		Purpose:   purpose,
		ForceNew:  routeReq.ForceNew,
		Policy: proxyLeaseSessionPolicy{
			Mode:      "PROXY_SESSION_MODE_STICKY",
			Region:    countryCode,
			StickyTTL: proxyRuntimeDurationString(ttl),
			Labels: map[string]string{
				"source":         "wa-app",
				"purpose":        purpose,
				"country_code":   countryCode,
				"session_id":     sessionID,
				"selection_seed": sessionID,
			},
			UpstreamKind: "PROXY_UPSTREAM_KIND_DYNAMIC_IP",
			RotationMode: "PROXY_ROTATION_MODE_STICKY_SESSION",
		},
		SelectionPolicy: proxyDynamicIPSelectionPolicy{CountryCode: countryCode, Purpose: purpose, MaxAttempts: 10},
	}
	resp, err := p.acquireLease(ctx, leaseReq)
	if err != nil {
		return DynamicProxyRoute{}, err
	}
	lease := resp.Lease
	egress := resp.Egress
	if strings.TrimSpace(egress.Host) == "" || egress.Port == 0 {
		egress = lease.Egress
	}
	proxyURL, err := proxyRuntimeEndpointURL(egress)
	if err != nil {
		return DynamicProxyRoute{}, err
	}
	leaseID := strings.TrimSpace(lease.LeaseID)
	if leaseID == "" {
		return DynamicProxyRoute{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime returned lease without lease_id", true)
	}
	now := time.Now().UTC()
	expiresAt := proxyRuntimeTimestamp(firstNonEmpty(lease.ExpiresAt, lease.Session.ExpiresAt))
	if expiresAt.IsZero() {
		expiresAt = now.Add(ttl)
	}
	return DynamicProxyRoute{
		AccountID:   firstNonEmpty(strings.TrimSpace(lease.AccountID), profileID),
		RouteID:     leaseID,
		LeaseID:     leaseID,
		Username:    rule.Username,
		ProfileID:   profileID,
		ProxyURL:    proxyURL,
		ProxyMode:   proxyRouteMode(DynamicProxySessionModeSticky),
		CountryCode: countryCode,
		ExpiresAt:   expiresAt,
	}, nil
}

func (p *DynamicProxyRuntime) ReleaseProxyRoute(ctx context.Context, route DynamicProxyRoute) error {
	leaseID := strings.TrimSpace(route.LeaseID)
	if p == nil || p.baseURL == "" || leaseID == "" {
		return nil
	}
	endpoint, err := p.endpoint("/leases/release")
	if err != nil {
		return err
	}
	payload := releaseProxyLeaseRequest{LeaseID: leaseID}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return proxyRuntimeRouteError("gateway profile release", resp.StatusCode, body)
	}
	return nil
}

func (p *DynamicProxyRuntime) acquireLease(ctx context.Context, payload acquireProxyLeaseRequest) (acquireProxyLeaseResponse, error) {
	endpoint, err := p.endpoint("/leases/acquire")
	if err != nil {
		return acquireProxyLeaseResponse{}, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return acquireProxyLeaseResponse{}, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return acquireProxyLeaseResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return acquireProxyLeaseResponse{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime lease acquire unavailable", true)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return acquireProxyLeaseResponse{}, proxyRuntimeRouteError("lease acquire", resp.StatusCode, body)
	}
	var out acquireProxyLeaseResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return acquireProxyLeaseResponse{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime lease acquire response is invalid", true)
	}
	return out, nil
}

func (p *DynamicProxyRuntime) gatewayProxyRule(ctx context.Context, username string) (gatewayProxyRule, error) {
	if p == nil || p.baseURL == "" {
		return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "WA proxy runtime is not configured", false)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "gateway username is required", false)
	}
	if rule, ok := p.cachedGatewayProxyRule(username, time.Now().UTC()); ok {
		return rule, nil
	}
	rules, err := p.fetchGatewayProxyRules(ctx)
	if err != nil {
		if rule, ok := p.cachedGatewayProxyRule(username, time.Now().UTC()); ok {
			return rule, nil
		}
		return gatewayProxyRule{}, err
	}
	p.cacheGatewayProxyRules(rules, time.Now().UTC().Add(proxyRuntimeSettingsCacheTTL))
	if rule, ok := gatewayProxyRuleFromSettings(username, rules); ok {
		return rule, nil
	}
	return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, fmt.Sprintf("proxy-runtime gateway user %q is unavailable", username), true)
}

func (p *DynamicProxyRuntime) fetchGatewayProxyRules(ctx context.Context) ([]proxyIngressRuleSettings, error) {
	endpoint, err := p.endpoint("/settings/in-user-rules")
	if err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithTimeout(ctx, proxyRuntimeRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime gateway ingress unavailable", true)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, proxyRuntimeRouteError("gateway ingress", resp.StatusCode, body)
	}
	var settings proxyRuntimeSettingsResponse
	if err := json.Unmarshal(body, &settings); err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime gateway ingress response is invalid", true)
	}
	return settings.Settings.IngressRules, nil
}

func gatewayProxyRuleFromSettings(username string, rules []proxyIngressRuleSettings) (gatewayProxyRule, bool) {
	username = strings.TrimSpace(username)
	for _, rule := range rules {
		if !rule.Enabled || strings.TrimSpace(rule.Username) != username {
			continue
		}
		return gatewayProxyRule{Username: username, Password: rule.PasswordValue, ProfileID: strings.TrimSpace(rule.ProfileID)}, true
	}
	return gatewayProxyRule{}, false
}

func (p *DynamicProxyRuntime) cachedGatewayProxyRule(username string, now time.Time) (gatewayProxyRule, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if now.IsZero() || p.rulesExpireAt.IsZero() || !now.Before(p.rulesExpireAt) {
		return gatewayProxyRule{}, false
	}
	return gatewayProxyRuleFromSettings(username, p.rules)
}

func (p *DynamicProxyRuntime) cacheGatewayProxyRules(rules []proxyIngressRuleSettings, expiresAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rules = append([]proxyIngressRuleSettings{}, rules...)
	p.rulesExpireAt = expiresAt
}

func (p *DynamicProxyRuntime) endpoint(path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(p.baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid WA proxy runtime API URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (p *DynamicProxyRuntime) gatewayProxyURL(username string, password string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(p.baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid WA proxy runtime API URL")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid WA proxy runtime API URL")
	}
	gateway := &url.URL{
		Scheme: p.gatewayScheme,
		User:   url.UserPassword(username, password),
		Host:   net.JoinHostPort(host, proxyRuntimeGatewayPort),
	}
	return gateway.String(), nil
}

func gatewayProxyScheme(protocol string) string {
	protocol = strings.TrimSpace(strings.ToLower(protocol))
	protocol = strings.TrimSuffix(protocol, "://")
	switch protocol {
	case "http", "https", "socks5", "socks5h":
		return protocol
	case "mixed":
		return "http"
	case "socks", "socks5-proxy":
		return "socks5"
	default:
		return "socks5"
	}
}

func proxyRouteID(username string, req DynamicProxyRouteRequest) string {
	seed := strings.Join([]string{username, req.Purpose, req.CorrelationID, req.SessionID, proxyRouteMode(req.Mode)}, ":")
	return "gateway-" + safeProxyRouteToken(username) + "-" + stableID(seed)
}

func proxyRouteMode(mode DynamicProxySessionMode) string {
	switch mode {
	case DynamicProxySessionModeRotating:
		return "US_ROTATING_DYNAMIC_IP"
	case DynamicProxySessionModeSticky:
		return "US_STICKY_DYNAMIC_IP"
	default:
		return "GATEWAY_PROFILE"
	}
}

func proxyRuntimeCountryCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "+")
	switch value {
	case "", "1", "USA", "UNITEDSTATES", "UNITED_STATES":
		return "US"
	default:
		return value
	}
}

func proxyRuntimeDurationString(value time.Duration) string {
	if value <= 0 {
		value = registrationProxyRouteTTL
	}
	seconds := int64(value / time.Second)
	if seconds <= 0 {
		seconds = 1
	}
	return fmt.Sprintf("%ds", seconds)
}

func proxyRuntimeTimestamp(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func proxyRuntimeEndpointURL(endpoint proxyRuntimeEndpoint) (string, error) {
	host := strings.TrimSpace(endpoint.Host)
	if host == "" || endpoint.Port == 0 {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy-runtime returned invalid lease egress", true)
	}
	scheme := "http"
	if strings.Contains(strings.ToUpper(endpoint.Protocol), "SOCKS5") {
		scheme = "socks5"
	}
	proxy := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, fmt.Sprintf("%d", endpoint.Port))}
	username := strings.TrimSpace(endpoint.Labels["proxy_username"])
	password := endpoint.Labels["proxy_password"]
	if username != "" || password != "" {
		proxy.User = url.UserPassword(username, password)
	}
	return proxy.String(), nil
}

func safeProxyRouteToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			out.WriteRune(char)
		case char >= '0' && char <= '9':
			out.WriteRune(char)
		case char == '-' || char == '_':
			out.WriteByte('-')
		}
	}
	token := strings.Trim(out.String(), "-")
	if token == "" {
		return "proxy"
	}
	if len(token) > 48 {
		return token[:48]
	}
	return token
}

func proxyRuntimeRouteError(resource string, statusCode int, body []byte) error {
	message := fmt.Sprintf("proxy-runtime %s unavailable: HTTP %d", strings.TrimSpace(resource), statusCode)
	if detail := proxyRuntimeErrorDetail(body); detail != "" {
		message += ": " + detail
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, message, true)
}

func proxyRuntimeErrorDetail(body []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	detail := strings.Join(strings.Fields(payload.Message), " ")
	if detail == "" || strings.Contains(detail, "://") {
		return ""
	}
	const maxDetailLength = 180
	if len(detail) > maxDetailLength {
		return detail[:maxDetailLength]
	}
	return detail
}
