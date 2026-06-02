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
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

type DynamicProxyLease struct {
	AccountID string
	LeaseID   string
	ProxyURL  string
}

type DynamicProxyRuntime struct {
	baseURL string
	client  *http.Client
	ids     IDGenerator
}

func NewDynamicProxyRuntime(baseURL string, ids IDGenerator) *DynamicProxyRuntime {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	return &DynamicProxyRuntime{baseURL: baseURL, client: &http.Client{Timeout: 20 * time.Second}, ids: ids}
}

func (p *DynamicProxyRuntime) AcquireUSDynamic(ctx context.Context, purpose string, correlationID string, leaseTTL time.Duration) (DynamicProxyLease, error) {
	if p == nil || p.baseURL == "" {
		return DynamicProxyLease{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "PROXY_RUNTIME_API_BASE_URL is required", false)
	}
	endpoint, err := p.endpoint("/leases/acquire")
	if err != nil {
		return DynamicProxyLease{}, err
	}
	purpose = firstNonEmpty(purpose, "WA_DYNAMIC_PROXY")
	ttl := "600s"
	if leaseTTL > 0 {
		ttl = fmt.Sprintf("%ds", int(leaseTTL.Round(time.Second)/time.Second))
	}
	accountID := "wa-dynamic-" + p.ids.NewID("")
	requestBody := map[string]any{
		"account_id": accountID,
		"purpose":    purpose,
		"force_new":  true,
		"policy": map[string]any{
			"mode":       "PROXY_SESSION_MODE_STICKY",
			"region":     "US",
			"sticky_ttl": ttl,
			"labels": map[string]string{
				"country_code": "US",
				"correlation":  correlationID,
			},
		},
		"chain_policy": map[string]any{
			"country_code":                 "US",
			"purpose":                      purpose,
			"strategy":                     "PROXY_CHAIN_STRATEGY_REGION_AWARE",
			"max_attempts":                 3,
			"allow_direct_dynamic_gateway": false,
			"prefer_line_proxy":            true,
		},
	}
	data, _ := json.Marshal(requestBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return DynamicProxyLease{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return DynamicProxyLease{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DynamicProxyLease{}, fmt.Errorf("proxy-runtime acquire returned HTTP %d", resp.StatusCode)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return DynamicProxyLease{}, err
	}
	lease := objectField(raw, "lease")
	egress := objectField(raw, "egress")
	if len(egress) == 0 {
		egress = objectField(lease, "egress")
	}
	proxyURL, err := proxyURLFromDynamicEndpoint(p.baseURL, egress)
	if err != nil {
		return DynamicProxyLease{}, err
	}
	return DynamicProxyLease{AccountID: firstNonEmpty(textField(lease, "account_id"), accountID), LeaseID: textField(lease, "lease_id"), ProxyURL: proxyURL}, nil
}

func (p *DynamicProxyRuntime) FixedProxyURL(ctx context.Context) (string, error) {
	if p == nil || p.baseURL == "" {
		return "", NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "PROXY_RUNTIME_API_BASE_URL is required", false)
	}
	endpoint, err := p.endpoint("/gateway")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("proxy-runtime gateway returned HTTP %d", resp.StatusCode)
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", err
	}
	listener := fixedEgressListener(objectField(raw, "gateway"))
	if len(listener) == 0 {
		return "", fmt.Errorf("proxy-runtime fixed egress listener is unavailable")
	}
	return proxyURLFromEgressListener(p.baseURL, listener)
}

func (p *DynamicProxyRuntime) Release(ctx context.Context, accountID string) {
	if p == nil || strings.TrimSpace(accountID) == "" {
		return
	}
	endpoint, err := p.endpoint("/leases/release")
	if err != nil {
		return
	}
	data, _ := json.Marshal(map[string]string{"account_id": accountID})
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err == nil && resp != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
	}
}

func (p *DynamicProxyRuntime) endpoint(path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(p.baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid PROXY_RUNTIME_API_BASE_URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func proxyURLFromDynamicEndpoint(baseURL string, endpoint map[string]any) (string, error) {
	port := int(numberField(endpoint, "port"))
	if port <= 0 {
		return "", fmt.Errorf("dynamic proxy lease has no egress port")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	host := textField(endpoint, "host")
	if isLocalDynamicProxyHost(host) {
		host = base.Hostname()
	}
	if host == "" {
		return "", fmt.Errorf("dynamic proxy lease has no egress host")
	}
	return (&url.URL{Scheme: proxyScheme(textField(endpoint, "protocol")), Host: net.JoinHostPort(host, fmt.Sprintf("%d", port))}).String(), nil
}

func fixedEgressListener(gateway map[string]any) map[string]any {
	listeners := objectListField(gateway, "listeners")
	for _, listener := range listeners {
		if strings.EqualFold(textField(listener, "listener_id"), "direct-egress") && listenerRouteLabel(listener) == "upstream" {
			return listener
		}
	}
	for _, listener := range listeners {
		if listenerRouteLabel(listener) == "upstream" && !strings.Contains(strings.ToLower(textField(listener, "listener_id")), "checkout") {
			return listener
		}
	}
	for _, listener := range listeners {
		if listenerRouteLabel(listener) == "upstream" {
			return listener
		}
	}
	return nil
}

func listenerRouteLabel(listener map[string]any) string {
	return strings.ToLower(textField(objectField(listener, "labels"), "route"))
}

func proxyURLFromEgressListener(baseURL string, listener map[string]any) (string, error) {
	addr := textField(listener, "listen_addr")
	if addr == "" {
		return "", fmt.Errorf("proxy-runtime fixed egress listener has no address")
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(addr, ":") {
			host = ""
			portText = strings.TrimPrefix(addr, ":")
		} else if _, parseErr := strconv.Atoi(addr); parseErr == nil {
			host = ""
			portText = addr
		} else {
			return "", fmt.Errorf("parse proxy-runtime fixed egress listener address: %w", err)
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return "", fmt.Errorf("proxy-runtime fixed egress listener has invalid port")
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if isLocalDynamicProxyHost(host) {
		host = base.Hostname()
	}
	if host == "" {
		return "", fmt.Errorf("proxy-runtime fixed egress listener has no host")
	}
	return (&url.URL{Scheme: proxyScheme(textField(listener, "protocol")), Host: net.JoinHostPort(host, fmt.Sprintf("%d", port))}).String(), nil
}

func proxyScheme(protocol string) string {
	if strings.Contains(strings.ToUpper(protocol), "SOCKS5") {
		return "socks5"
	}
	return "http"
}

func isLocalDynamicProxyHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	return host == "" || host == "0.0.0.0" || host == "127.0.0.1" || host == "localhost" || host == "::" || host == "::1"
}
