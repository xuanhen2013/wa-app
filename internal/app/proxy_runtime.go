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
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type DynamicProxyRoute struct {
	AccountID   string
	RouteID     string
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
	TTL           time.Duration
	Mode          DynamicProxySessionMode
}

type DynamicProxyRuntime struct {
	baseURL       string
	gatewayScheme string
	client        *http.Client
}

type gatewayProxyRule struct {
	Username  string
	Password  string
	ProfileID string
}

type releaseProxyLeaseRequest struct {
	LeaseID string `json:"lease_id"`
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

const proxyRuntimeGatewayPort = "10810"

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

func (p *DynamicProxyRuntime) ReleaseProxyRoute(ctx context.Context, route DynamicProxyRoute) error {
	routeID := strings.TrimSpace(route.RouteID)
	if p == nil || p.baseURL == "" || !strings.HasPrefix(routeID, "dynamic-profile-") {
		return nil
	}
	endpoint, err := p.endpoint("/leases/release")
	if err != nil {
		return err
	}
	payload := releaseProxyLeaseRequest{LeaseID: routeID}
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

func (p *DynamicProxyRuntime) gatewayProxyRule(ctx context.Context, username string) (gatewayProxyRule, error) {
	if p == nil || p.baseURL == "" {
		return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "WA_APP_PROXY_RUNTIME_API_BASE_URL is not configured", false)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "gateway username is required", false)
	}
	endpoint, err := p.endpoint("/settings/in-user-rules")
	if err != nil {
		return gatewayProxyRule{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return gatewayProxyRule{}, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return gatewayProxyRule{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return gatewayProxyRule{}, proxyRuntimeRouteError("gateway ingress", resp.StatusCode, body)
	}
	var settings proxyRuntimeSettingsResponse
	if err := json.Unmarshal(body, &settings); err != nil {
		return gatewayProxyRule{}, err
	}
	for _, rule := range settings.Settings.IngressRules {
		if !rule.Enabled || strings.TrimSpace(rule.Username) != username {
			continue
		}
		return gatewayProxyRule{Username: username, Password: rule.PasswordValue, ProfileID: strings.TrimSpace(rule.ProfileID)}, nil
	}
	return gatewayProxyRule{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, fmt.Sprintf("proxy-runtime gateway user %q is unavailable", username), true)
}

func (p *DynamicProxyRuntime) endpoint(path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(p.baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid WA_APP_PROXY_RUNTIME_API_BASE_URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func (p *DynamicProxyRuntime) gatewayProxyURL(username string, password string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(p.baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid WA_APP_PROXY_RUNTIME_API_BASE_URL")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", fmt.Errorf("invalid WA_APP_PROXY_RUNTIME_API_BASE_URL")
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
	seed := strings.Join([]string{username, req.Purpose, req.CorrelationID, proxyRouteMode(req.Mode)}, ":")
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
