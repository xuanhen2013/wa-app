package app

import (
	"context"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func (s *Server) LoginStateCheckProxyRoute(ctx context.Context, correlationID string, ttl time.Duration) (DynamicProxyRoute, error) {
	if s == nil || s.proxyRuntime == nil {
		return DynamicProxyRoute{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "WA_APP_PROXY_RUNTIME_API_BASE_URL is not configured", false)
	}
	username := strings.TrimSpace(s.loginStateCheckProxyUsername)
	if username == "" {
		return DynamicProxyRoute{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "WA_LOGIN_STATE_CHECK_PROXY_USERNAME is required", false)
	}
	return s.proxyRuntime.GatewayProxyRoute(ctx, username, DynamicProxyRouteRequest{
		Purpose:       "WA_LOGIN_STATE_CHECK",
		CorrelationID: correlationID,
		TTL:           ttl,
		Mode:          DynamicProxySessionModeRotating,
	})
}

func (s *Server) ReleaseProxyRoute(ctx context.Context, route DynamicProxyRoute) {
	if s == nil || s.proxyRuntime == nil {
		return
	}
	_ = s.proxyRuntime.ReleaseProxyRoute(ctx, route)
}
