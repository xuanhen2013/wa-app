package bff

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/bulkregistration"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
)

func Test1024ProxySourceBuildsHTTPRoute(t *testing.T) {
	var requestURL *url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL = r.URL
		_, _ = w.Write([]byte(`[{"host":"165.0.173.183","port":"7098"}]`))
	}))
	defer server.Close()
	source := new1024ProxySource(server.Client(), server.URL, "account-region-{country}-sid-{session_id}-t-{sticky_minutes}", "test-password")
	now := time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC)
	route, err := source.resolve(context.Background(), "US", "Ab12Cd34", 30, now)
	if err != nil {
		t.Fatalf("resolve route: %v", err)
	}
	if requestURL == nil {
		t.Fatal("expected 1024proxy request")
	}
	query := requestURL.Query()
	if query.Get("region") != registrationProxyNodeRegion1024 || query.Get("num") != "1" || query.Get("time") != "30" || query.Get("format") != "1" || query.Get("type") != "json" {
		t.Fatalf("unexpected 1024proxy query: %s", requestURL.RawQuery)
	}
	if route.ProxyMode != registrationProxyModeDedicated || route.Source != registrationProxySource1024 {
		t.Fatalf("unexpected route summary: %#v", route)
	}
	proxyURL, err := url.Parse(route.ProxyURL)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}
	if proxyURL.Scheme != "http" || proxyURL.Host != "165.0.173.183:7098" || proxyURL.User.Username() != "account-region-US-sid-Ab12Cd34-t-30" {
		t.Fatalf("unexpected proxy URL shape: %#v", proxyURL)
	}
}

func TestRegistrationProxyRetriesSourceAndRedactsDashboardSummary(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`[{"host":"165.0.173.183","port":"7098"}]`))
	}))
	defer server.Close()
	resolver := newRegistrationProxyResolver(RegistrationProxyConfig{Enabled: true, Source1024Enabled: true, Source1024UsernameTpl: "account-region-{country}-sid-{session_id}", Source1024Password: "test-password", SourceRetryMax: 2})
	resolver.sources["1024proxy"] = new1024ProxySource(server.Client(), server.URL, resolver.config.Source1024UsernameTpl, resolver.config.Source1024Password)
	resolver.now = func() time.Time { return time.Date(2026, time.July, 10, 12, 0, 0, 0, time.UTC) }
	resolver.egressChecker = registrationProxyEgressCheckerFunc(func(context.Context, wacore.WAProxyRoute, string) error { return nil })
	route, err := resolver.resolve(context.Background(), "US", "+14155550123")
	if err != nil {
		t.Fatalf("resolve after retry: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts=%d, want 2", attempts.Load())
	}
	summary := registrationProxyRouteMap(route, true)
	encoded := strings.ToLower(string(stringifyMap(summary)))
	if strings.Contains(encoded, "165.0.173.183") || strings.Contains(encoded, "test-password") || strings.Contains(encoded, "account-region") {
		t.Fatalf("dashboard proxy summary leaked sensitive route data: %s", encoded)
	}
}

func TestRegistrationProxyWaitReusesRouteAndRejectsExpiredSession(t *testing.T) {
	route := wacore.WAProxyRoute{ProxyURL: "http://user:password@165.0.173.183:7098", ProxyMode: registrationProxyModeDedicated, CountryCode: "PH", Source: registrationProxySource1024, RouteID: "route_1"}
	wait := registrationProxyWaitFromRoute(route, time.Now().UTC().Add(time.Minute))
	reused, err := registrationProxyRouteFromWait(wait)
	if err != nil {
		t.Fatalf("reuse sticky route: %v", err)
	}
	if reused.ProxyURL != route.ProxyURL || reused.RouteID != route.RouteID {
		t.Fatalf("sticky route changed: %#v", reused)
	}
	wait.ExpiresAtUnix = time.Now().UTC().Add(-time.Second).Unix()
	_, err = registrationProxyRouteFromWait(wait)
	if err == nil || !strings.Contains(err.Error(), "request OTP again") {
		t.Fatalf("expired route error=%v, want re-request guidance", err)
	}
}

func TestRegistrationProxyWaitTTLOutlivesStickySession(t *testing.T) {
	gateway := &actionGateway{registrationProxy: newRegistrationProxyResolver(RegistrationProxyConfig{Enabled: true, StickyMinutes: 30})}
	if got := gateway.registrationProxyWaitTTL(registrationOTPWaitDefaultTTL); got != 35*time.Minute {
		t.Fatalf("proxy wait ttl=%s, want 35m", got)
	}
}

func TestRegistrationProxyRejectsMissingSource(t *testing.T) {
	resolver := newRegistrationProxyResolver(RegistrationProxyConfig{Enabled: true, Source1024Enabled: false, Fallback: "reject"})
	_, err := resolver.resolve(context.Background(), "PH", "+639171234567")
	if err == nil {
		t.Fatal("expected dedicated proxy rejection")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected context error: %v", err)
	}
}

func TestRegistrationRunnerPrefersDedicatedProxyOverCommonProxy(t *testing.T) {
	manager, _, _ := newBulkTestManager(t, &bulkTestProvider{}, bulkregistration.ItemStatusQueued)
	manager.server.SetCommonProxyURL("http://common-proxy.invalid:8888")
	var requestURL *url.URL
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestURL = r.URL
		_, _ = w.Write([]byte(`[{"host":"165.0.173.183","port":"7098"}]`))
	}))
	defer provider.Close()
	resolver := newRegistrationProxyResolver(RegistrationProxyConfig{
		Enabled:               true,
		Source1024Enabled:     true,
		Source1024UsernameTpl: "account-region-{country}-sid-{session_id}",
		Source1024Password:    "test-password",
	})
	resolver.sources["1024proxy"] = new1024ProxySource(provider.Client(), provider.URL, resolver.config.Source1024UsernameTpl, resolver.config.Source1024Password)
	resolver.egressChecker = registrationProxyEgressCheckerFunc(func(context.Context, wacore.WAProxyRoute, string) error { return nil })
	gateway := &actionGateway{server: manager.server, registrationProxy: resolver}
	runner, route, managedRoute, err := gateway.registrationRunner(context.Background(), map[string]any{
		"country_iso2": "US",
		"phone":        map[string]any{"e164_number": "+14155550123", "country_iso2": "US"},
	})
	if err != nil {
		t.Fatalf("create dedicated registration runner: %v", err)
	}
	defer runner.CloseIdleConnections()
	if !managedRoute || route.Source != registrationProxySource1024 || strings.Contains(route.ProxyURL, "common-proxy.invalid") {
		t.Fatalf("registration runner selected the wrong route: %#v", route)
	}
	if requestURL == nil || requestURL.Query().Get("region") != registrationProxyNodeRegion1024 {
		t.Fatalf("dedicated source did not request a random 1024proxy access relay: %#v", requestURL)
	}
}

func TestRegistrationProxyRejectsMismatchedEgress(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"host":"165.0.173.183","port":"7098"}]`))
	}))
	defer provider.Close()
	resolver := newRegistrationProxyResolver(RegistrationProxyConfig{
		Enabled:               true,
		Source1024Enabled:     true,
		Source1024UsernameTpl: "account-region-{country}",
		Source1024Password:    "test-password",
		SourceRetryMax:        1,
	})
	resolver.sources["1024proxy"] = new1024ProxySource(provider.Client(), provider.URL, resolver.config.Source1024UsernameTpl, resolver.config.Source1024Password)
	resolver.egressChecker = registrationProxyEgressCheckerFunc(func(_ context.Context, _ wacore.WAProxyRoute, countryCode string) error {
		return fmt.Errorf("egress is not %s", countryCode)
	})
	_, err := resolver.resolve(context.Background(), "US", "+14155550123")
	if err == nil {
		t.Fatal("expected an egress mismatch to reject the dedicated route")
	}
}

func TestHTTPEgressCheckerAcceptsOnlyTheRequestedCountry(t *testing.T) {
	returnedCountry := "US"
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"country":"` + returnedCountry + `"}`))
	}))
	defer proxy.Close()
	checker := httpRegistrationProxyEgressChecker{endpoint: "http://egress-check.invalid/", timeout: time.Second}
	route := wacore.WAProxyRoute{ProxyURL: proxy.URL}
	if err := checker.check(context.Background(), route, "US"); err != nil {
		t.Fatalf("accept US egress: %v", err)
	}
	returnedCountry = "PH"
	if err := checker.check(context.Background(), route, "US"); err == nil {
		t.Fatal("expected a mismatched egress country to be rejected")
	}
}

func Test1024ProxySourceRejectsMalformedNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"host":"165.0.173.183","port":"invalid"}]`))
	}))
	defer server.Close()
	source := new1024ProxySource(server.Client(), server.URL, "account-region-{country}", "test-password")
	_, err := source.resolve(context.Background(), "PH", "sticky-session", 30, time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "invalid node port") {
		t.Fatalf("unexpected malformed node error: %v", err)
	}
}

func TestRegistrationProxySessionIDIsUniqueWithoutTarget(t *testing.T) {
	if first, second := registrationProxySessionID(""), registrationProxySessionID(""); first == second {
		t.Fatalf("empty registration proxy session IDs must not share a sticky route: %q", first)
	} else if len(first) != 8 || len(second) != 8 {
		t.Fatalf("random sticky session lengths=(%d,%d), want 8", len(first), len(second))
	}
	if got := registrationProxySessionID("same-registration"); len(got) != 8 {
		t.Fatalf("sticky session id length=%d, want 8", len(got))
	}
}

func TestRegistrationProxyCapsStickyMinutesAtProviderLimit(t *testing.T) {
	config := normalizeRegistrationProxyConfig(RegistrationProxyConfig{StickyMinutes: 121})
	if config.StickyMinutes != 120 {
		t.Fatalf("sticky minutes=%d, want 120", config.StickyMinutes)
	}
}

func stringifyMap(value map[string]any) []byte {
	parts := make([]string, 0, len(value))
	for key, current := range value {
		parts = append(parts, key+"="+strings.TrimSpace(toString(current)))
	}
	return []byte(strings.Join(parts, ";"))
}

func toString(value any) string {
	return fmt.Sprint(value)
}

type registrationProxyEgressCheckerFunc func(context.Context, wacore.WAProxyRoute, string) error

func (fn registrationProxyEgressCheckerFunc) check(ctx context.Context, route wacore.WAProxyRoute, countryCode string) error {
	return fn(ctx, route, countryCode)
}
