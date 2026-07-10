package bff

import (
	"context"
	"net/http"

	"github.com/byte-v-forge/wa-app/internal/waapp/rpc"
)

// RegistrationFeatures wires the dashboard registration flows to their private
// proxy and SMS-provider implementation settings. Neither setting is exposed
// by the gRPC Proto contract.
type RegistrationFeatures struct {
	server *rpc.Server
	proxy  RegistrationProxyConfig
	bulk   *bulkRegistrationManager
}

func NewRegistrationFeatures(server *rpc.Server, proxy RegistrationProxyConfig, bulk BulkRegistrationConfig) *RegistrationFeatures {
	return &RegistrationFeatures{server: server, proxy: normalizeRegistrationProxyConfig(proxy), bulk: newBulkRegistrationManager(server, proxy, bulk)}
}

func (f *RegistrationFeatures) ActionHandler() http.Handler {
	if f == nil {
		return NewActionGateway(nil)
	}
	return NewActionGatewayWithRegistrationProxy(f.server, f.proxy)
}

func (f *RegistrationFeatures) StartRegistration(ctx context.Context, payload map[string]any) (map[string]any, error) {
	if f == nil {
		return StartRegistration(nil, ctx, payload)
	}
	return StartRegistrationWithRegistrationProxy(f.server, f.proxy, ctx, payload)
}

func (f *RegistrationFeatures) BulkRegistrationHandler() http.Handler {
	if f == nil || f.bulk == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(f.bulk.HandleHTTP)
}

func (f *RegistrationFeatures) Run(ctx context.Context) error {
	if f == nil || f.bulk == nil {
		<-ctx.Done()
		return nil
	}
	return f.bulk.Run(ctx)
}
