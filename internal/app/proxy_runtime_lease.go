package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const (
	proxyRuntimeLeasePurpose               = "wa-app-registration"
	proxyRuntimeLeaseHTTPTimeout           = 15 * time.Second
	proxyRuntimeLeaseReleaseTimeout        = 5 * time.Second
	proxyRuntimeLeaseDefaultMinimumTTL     = 30 * time.Second
	proxyRuntimeLeaseDefaultSelectionTries = 1

	registrationProxyLeaseModeDisabled registrationProxyLeaseMode = "disabled"
	registrationProxyLeaseModeOptional registrationProxyLeaseMode = "optional"
	registrationProxyLeaseModeRequired registrationProxyLeaseMode = "required"
)

type registrationProxyLeaseMode string

func normalizeRegistrationProxyLeaseMode(mode string) registrationProxyLeaseMode {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "optional", "best_effort", "best-effort", "try", "enabled", "enable", "on", "true", "1":
		return registrationProxyLeaseModeOptional
	case "disabled", "disable", "off", "false", "0", "none":
		return registrationProxyLeaseModeDisabled
	case "required", "require", "strict", "force", "forced":
		return registrationProxyLeaseModeRequired
	default:
		return registrationProxyLeaseModeOptional
	}
}

func (m registrationProxyLeaseMode) String() string {
	return string(normalizeRegistrationProxyLeaseMode(string(m)))
}

func (m registrationProxyLeaseMode) enabled() bool {
	return normalizeRegistrationProxyLeaseMode(string(m)) != registrationProxyLeaseModeDisabled
}

func (m registrationProxyLeaseMode) required() bool {
	return normalizeRegistrationProxyLeaseMode(string(m)) == registrationProxyLeaseModeRequired
}

func (s *Server) effectiveRegistrationProxyLeaseMode() registrationProxyLeaseMode {
	if s == nil {
		return registrationProxyLeaseModeOptional
	}
	return normalizeRegistrationProxyLeaseMode(string(s.registrationProxyLeaseMode))
}

func (s *Server) registrationProxyLeaseEnabled() bool {
	return s.effectiveRegistrationProxyLeaseMode().enabled()
}

func (s *Server) registrationProxyLeaseRequired() bool {
	return s.effectiveRegistrationProxyLeaseMode().required()
}

type proxyRuntimeLeaseClient struct {
	apiBase   string
	authToken string
	client    *http.Client
}

type registrationProxyLease struct {
	LeaseID       string `json:"lease_id"`
	AccountID     string `json:"account_id"`
	Purpose       string `json:"purpose"`
	ProxyURL      string `json:"proxy_url"`
	ListenerID    string `json:"listener_id,omitempty"`
	CountryCode   string `json:"country_code,omitempty"`
	ExitRegion    string `json:"exit_region,omitempty"`
	ExitCity      string `json:"exit_city,omitempty"`
	ExpiresAtUnix int64  `json:"expires_at_unix,omitempty"`
}

type proxyRuntimeLeaseAcquireInput struct {
	AccountID   string
	Purpose     string
	CountryCode string
	TTL         time.Duration
	JobKey      string
}

func newProxyRuntimeLeaseClient(apiBase string, authToken string) *proxyRuntimeLeaseClient {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		return nil
	}
	return &proxyRuntimeLeaseClient{
		apiBase:   apiBase,
		authToken: strings.TrimSpace(authToken),
		client:    &http.Client{Timeout: proxyRuntimeLeaseHTTPTimeout},
	}
}

func (c *proxyRuntimeLeaseClient) acquire(ctx context.Context, input proxyRuntimeLeaseAcquireInput) (registrationProxyLease, error) {
	if c == nil || c.client == nil || c.apiBase == "" {
		return registrationProxyLease{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease client is not configured", true)
	}
	accountID := strings.TrimSpace(input.AccountID)
	if accountID == "" {
		return registrationProxyLease{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease account is not configured", true)
	}
	purpose := firstNonEmpty(input.Purpose, proxyRuntimeLeasePurpose)
	ttl := input.TTL
	if ttl < proxyRuntimeLeaseDefaultMinimumTTL {
		ttl = proxyRuntimeLeaseDefaultMinimumTTL
	}
	labels := map[string]any{
		"purpose":    purpose,
		"session_id": stableID(firstNonEmpty(input.JobKey, accountID+"|"+purpose)),
	}
	payload := map[string]any{
		"account_id": accountID,
		"purpose":    purpose,
		"force_new":  true,
		"policy": map[string]any{
			"mode":          "PROXY_SESSION_MODE_STICKY",
			"sticky_ttl":    fmt.Sprintf("%ds", int(ttl/time.Second)),
			"upstream_kind": "PROXY_UPSTREAM_KIND_DYNAMIC_IP",
			"rotation_mode": "PROXY_ROTATION_MODE_STICKY_SESSION",
			"labels":        labels,
		},
		"selection_policy": map[string]any{
			"country_code": strings.ToUpper(strings.TrimSpace(input.CountryCode)),
			"purpose":      purpose,
			"max_attempts": proxyRuntimeLeaseDefaultSelectionTries,
		},
	}
	body, err := c.postJSON(ctx, "/leases/acquire", payload)
	if err != nil {
		return registrationProxyLease{}, err
	}
	lease, err := c.parseAcquireResponse(accountID, purpose, body)
	if err != nil {
		return registrationProxyLease{}, err
	}
	return lease, nil
}

func (c *proxyRuntimeLeaseClient) release(ctx context.Context, lease registrationProxyLease) error {
	if c == nil || c.client == nil || c.apiBase == "" || strings.TrimSpace(lease.LeaseID) == "" {
		return nil
	}
	payload := map[string]any{
		"lease_id":   lease.LeaseID,
		"account_id": lease.AccountID,
		"purpose":    firstNonEmpty(lease.Purpose, proxyRuntimeLeasePurpose),
	}
	_, err := c.postJSON(ctx, "/leases/release", payload)
	return err
}

func (c *proxyRuntimeLeaseClient) postJSON(ctx context.Context, path string, payload map[string]any) (map[string]any, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease request failed", true)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease request was rejected", true)
	}
	out := map[string]any{}
	if len(strings.TrimSpace(string(body))) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease response is invalid", true)
	}
	return out, nil
}

func (c *proxyRuntimeLeaseClient) parseAcquireResponse(accountID string, purpose string, body map[string]any) (registrationProxyLease, error) {
	leaseData := objectField(body, "lease")
	egress := objectField(body, "egress")
	if len(egress) == 0 {
		egress = objectField(leaseData, "egress")
	}
	leaseID := firstNonEmpty(textField(leaseData, "lease_id"), textField(leaseData, "leaseId"), textField(objectField(egress, "labels"), "lease_id"), textField(objectField(egress, "labels"), "leaseId"))
	if leaseID == "" {
		return registrationProxyLease{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease id is empty", true)
	}
	proxyURL := proxyRuntimeLeaseProxyURL(egress)
	if proxyURL == "" {
		return registrationProxyLease{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease egress is invalid", true)
	}
	lease := registrationProxyLease{
		LeaseID:       leaseID,
		AccountID:     firstNonEmpty(textField(leaseData, "account_id"), textField(leaseData, "accountId"), accountID),
		Purpose:       firstNonEmpty(textField(leaseData, "purpose"), purpose),
		ProxyURL:      proxyURL,
		ListenerID:    proxyRuntimeLeaseListenerID(leaseData, body),
		CountryCode:   strings.ToUpper(firstNonEmpty(proxyRuntimeLeaseExitText("country_code", egress, leaseData, body), proxyRuntimeLeaseExitText("countryCode", egress, leaseData, body))),
		ExitRegion:    strings.ToUpper(firstNonEmpty(proxyRuntimeLeaseExitText("region", egress, leaseData, body), proxyRuntimeLeaseExitText("exit_state", egress, leaseData, body), proxyRuntimeLeaseExitText("exitState", egress, leaseData, body))),
		ExitCity:      firstNonEmpty(proxyRuntimeLeaseExitText("city", egress, leaseData, body), proxyRuntimeLeaseExitText("exit_city", egress, leaseData, body), proxyRuntimeLeaseExitText("exitCity", egress, leaseData, body)),
		ExpiresAtUnix: unixFromRFC3339(firstNonEmpty(textField(leaseData, "expires_at"), textField(leaseData, "expiresAt"))),
	}
	return lease, nil
}

func proxyRuntimeLeaseProxyURL(egress map[string]any) string {
	host := textField(egress, "host")
	port := textField(egress, "port")
	if host == "" || port == "" || port == "0" {
		return ""
	}
	protocol := strings.ToUpper(textField(egress, "protocol"))
	scheme := "http"
	if strings.Contains(protocol, "SOCKS5") {
		scheme = "socks5"
	}
	labels := objectField(egress, "labels")
	username := firstNonEmpty(textField(labels, "proxy_username"), textField(labels, "proxyUsername"))
	password := firstNonEmpty(textField(labels, "proxy_password"), textField(labels, "proxyPassword"))
	out := &url.URL{Scheme: scheme, Host: host + ":" + port}
	if username != "" || password != "" {
		out.User = url.UserPassword(username, password)
	}
	return out.String()
}

func proxyRuntimeLeaseListenerID(items ...map[string]any) string {
	queue := append([]map[string]any{}, items...)
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item == nil {
			continue
		}
		if value := firstNonEmpty(textField(item, "listener_id"), textField(item, "listenerId")); value != "" {
			return value
		}
		for _, key := range []string{"listener", "egress_listener", "egressListener"} {
			if nested := objectField(item, key); len(nested) > 0 {
				queue = append(queue, nested)
			}
		}
	}
	return ""
}

func proxyRuntimeLeaseExitText(key string, items ...map[string]any) string {
	for _, item := range items {
		if value := textField(item, key); value != "" {
			return value
		}
		if labels := objectField(item, "labels"); len(labels) > 0 {
			if value := textField(labels, key); value != "" {
				return value
			}
		}
	}
	return ""
}

func unixFromRFC3339(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UTC().Unix()
}

func proxyRuntimeLeaseAccountIDFromProxyURL(proxyURL string) string {
	parsed, err := parseOutboundProxyURL(proxyURL)
	if err != nil || parsed == nil || parsed.User == nil {
		return ""
	}
	return strings.TrimSpace(parsed.User.Username())
}

func proxyRuntimeLeaseRoute(lease registrationProxyLease, fallback WAProxyRoute) WAProxyRoute {
	fallback.ProxyURL = lease.ProxyURL
	fallback.ProxyMode = "PROXY_RUNTIME_LEASE"
	fallback.RouteID = "proxy-runtime-lease-" + stableID(lease.LeaseID)
	fallback.AccountID = firstNonEmpty(lease.AccountID, fallback.AccountID)
	fallback.CountryCode = firstNonEmpty(lease.CountryCode, fallback.CountryCode)
	return fallback
}

func (g *actionGateway) acquireRegistrationProxyLease(ctx context.Context, payload map[string]any, route WAProxyRoute, ttl time.Duration) (registrationProxyLease, WAProxyRoute, error) {
	if g == nil || g.server == nil || !g.server.registrationProxyLeaseEnabled() {
		return registrationProxyLease{}, route, nil
	}
	if g.server.proxyRuntimeLease == nil {
		return g.optionalRegistrationProxyLeaseError(route, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease client is not configured", true), "", payload)
	}
	accountID := registrationProxyLeaseAccountID(payload, route)
	if accountID == "" {
		return g.optionalRegistrationProxyLeaseError(route, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "proxy runtime lease account is not configured", true), "", payload)
	}
	lease, err := g.server.proxyRuntimeLease.acquire(ctx, proxyRuntimeLeaseAcquireInput{
		AccountID:   accountID,
		Purpose:     proxyRuntimeLeasePurpose,
		CountryCode: firstNonEmpty(route.CountryCode, proxyCountryCodeFromPayload(payload)),
		TTL:         ttl,
		JobKey:      registrationProxyLeaseJobKey(payload, accountID),
	})
	if err != nil {
		return g.optionalRegistrationProxyLeaseError(route, err, accountID, payload)
	}
	return lease, proxyRuntimeLeaseRoute(lease, route), nil
}

func registrationProxyLeaseAccountID(payload map[string]any, route WAProxyRoute) string {
	return firstNonEmpty(
		textField(payload, "proxy_runtime_account_id"),
		textField(objectField(payload, "proxy"), "proxy_runtime_account_id"),
		proxyRuntimeLeaseAccountIDFromProxyURL(route.ProxyURL),
	)
}

func registrationProxyLeaseJobKey(payload map[string]any, accountID string) string {
	ctxData := actionContext(payload)
	return firstNonEmpty(
		ctxData.GetCorrelationId(),
		ctxData.GetRequestId(),
		textField(payload, "verification_request_id"),
		stableID(firstNonEmpty(textField(objectField(payload, "phone"), "e164_number"), textField(payload, "wa_account_id"), accountID)),
	)
}

func (g *actionGateway) optionalRegistrationProxyLeaseError(route WAProxyRoute, err error, accountID string, payload map[string]any) (registrationProxyLease, WAProxyRoute, error) {
	if g == nil || g.server == nil || g.server.registrationProxyLeaseRequired() {
		return registrationProxyLease{}, route, err
	}
	if accountID != "" {
		log.Printf(
			"wa_registration_proxy_lease_unavailable mode=%s account_hash=%s country_code=%s error=%s",
			g.server.effectiveRegistrationProxyLeaseMode(),
			stableID(accountID),
			probeLogValue(firstNonEmpty(route.CountryCode, proxyCountryCodeFromPayload(payload))),
			probeLogValue(ToProtoError(err).GetMessage()),
		)
	}
	return registrationProxyLease{}, route, nil
}

func (g *actionGateway) releaseRegistrationProxyLease(ctx context.Context, lease registrationProxyLease) {
	if g == nil || g.server == nil || g.server.proxyRuntimeLease == nil || strings.TrimSpace(lease.LeaseID) == "" {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), proxyRuntimeLeaseReleaseTimeout)
	defer cancel()
	if err := g.server.proxyRuntimeLease.release(releaseCtx, lease); err != nil {
		log.Printf("wa_registration_proxy_lease_release_failed lease_hash=%s error=%s", stableID(lease.LeaseID), probeLogValue(ToProtoError(err).GetMessage()))
	}
}

func validRegistrationProxyLease(lease registrationProxyLease) bool {
	return strings.TrimSpace(lease.LeaseID) != "" && strings.TrimSpace(lease.ProxyURL) != ""
}
