package app

import (
	"context"
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// serverCore holds the injected collaborators plus every cross-service helper and
// workflow method. The per-service gRPC handlers embed it; it is the single home
// for shared behaviour so no one handler becomes a god object.
type serverCore struct {
	store   Store
	runtime RuntimeState
	runner  wacore.ProtocolEngine
	clock   shared.Clock
	ids     shared.IDGenerator

	commonProxyURL  string
	longConnections *LongConnectionManager

	// facade points back at the embedding Server so the few helpers that drive the
	// full service surface (e.g. the BFF action gateway) can reach every handler.
	facade *Server
}

// Each handler implements exactly one gRPC service and embeds the shared core.
type discoveryHandler struct {
	waappv1.UnimplementedWaDiscoveryServiceServer
	*serverCore
}
type profileHandler struct {
	waappv1.UnimplementedWaProfileServiceServer
	*serverCore
}
type registrationHandler struct {
	waappv1.UnimplementedWaRegistrationServiceServer
	*serverCore
}
type messagingHandler struct {
	waappv1.UnimplementedWaMessagingServiceServer
	*serverCore
}
type extractionHandler struct {
	waappv1.UnimplementedWaExtractionServiceServer
	*serverCore
}
type contactHandler struct {
	waappv1.UnimplementedWaContactServiceServer
	*serverCore
}
type toolingHandler struct {
	waappv1.UnimplementedWaToolingServiceServer
	*serverCore
}
type accountSettingsHandler struct {
	waappv1.UnimplementedWaAccountSettingsServiceServer
	*serverCore
}

// Server is the facade that embeds the shared core and the eight per-service
// handlers, so it still satisfies all eight gRPC service interfaces (via method
// promotion) for a single registration/wiring surface, without owning 128 methods.
type Server struct {
	*serverCore
	*discoveryHandler
	*profileHandler
	*registrationHandler
	*messagingHandler
	*extractionHandler
	*contactHandler
	*toolingHandler
	*accountSettingsHandler
}

func newServerFacade(core *serverCore) *Server {
	server := &Server{
		serverCore:             core,
		discoveryHandler:       &discoveryHandler{serverCore: core},
		profileHandler:         &profileHandler{serverCore: core},
		registrationHandler:    &registrationHandler{serverCore: core},
		messagingHandler:       &messagingHandler{serverCore: core},
		extractionHandler:      &extractionHandler{serverCore: core},
		contactHandler:         &contactHandler{serverCore: core},
		toolingHandler:         &toolingHandler{serverCore: core},
		accountSettingsHandler: &accountSettingsHandler{serverCore: core},
	}
	core.facade = server
	return server
}

func NewServer(store Store, runtime RuntimeState, runner wacore.ProtocolEngine, clock shared.Clock, ids shared.IDGenerator) *Server {
	if clock == nil {
		clock = shared.SystemClock{}
	}
	if ids == nil {
		ids = shared.RandomIDGenerator{}
	}
	server := newServerFacade(&serverCore{store: store, runtime: runtime, runner: runner, clock: clock, ids: ids})
	server.longConnections = NewLongConnectionManager(server)
	return server
}

func (s *serverCore) SetCommonProxyURL(common string) {
	s.commonProxyURL = strings.TrimSpace(common)
}

func (s *serverCore) PlayIntegrityAPIConfigured() bool {
	if s == nil {
		return false
	}
	engine, ok := s.runner.(*NativeEngine)
	return ok && engine.PlayIntegrityAPIConfigured()
}

func (s *serverCore) PlayIntegrityAPIStatus(ctx context.Context) PlayIntegrityAPIStatus {
	if s == nil {
		return PlayIntegrityAPIStatus{Configured: false, Available: false, RawValuesPrinted: false}
	}
	engine, ok := s.runner.(*NativeEngine)
	if !ok {
		return PlayIntegrityAPIStatus{Configured: false, Available: false, RawValuesPrinted: false}
	}
	return engine.PlayIntegrityAPIStatus(ctx)
}

func (s *serverCore) RunLongConnections(ctx context.Context) error {
	if s == nil || s.longConnections == nil {
		return nil
	}
	return s.longConnections.Run(ctx)
}

func (s *discoveryHandler) RegisterAppArtifact(ctx context.Context, req *waappv1.RegisterAppArtifactRequest) (*waappv1.RegisterAppArtifactResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.RegisterAppArtifactResponse{Error: shared.ToProtoError(err)}, nil
	}
	if strings.TrimSpace(req.GetLabel()) == "" {
		return &waappv1.RegisterAppArtifactResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "artifact label is required", false))}, nil
	}
	now := s.clock.Now()
	artifact := &waappv1.AppArtifact{ArtifactId: s.ids.NewID("waart_"), Label: req.GetLabel(), VersionLabel: req.GetVersionLabel(), Sha256: req.GetSha256(), ObservedAt: timestamppb.New(now)}
	if err := s.store.SaveAppArtifact(ctx, artifact); err != nil {
		return &waappv1.RegisterAppArtifactResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RegisterAppArtifactResponse{Artifact: artifact}, nil
}

func (s *discoveryHandler) RecordProtocolProfile(ctx context.Context, req *waappv1.RecordProtocolProfileRequest) (*waappv1.RecordProtocolProfileResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.store.GetAppArtifact(ctx, req.GetAppArtifactId()); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	profile := &waappv1.ProtocolProfile{
		ProtocolProfileId: s.ids.NewID("waproto_"),
		AppArtifactId:     req.GetAppArtifactId(),
		DisplayName:       shared.FirstNonEmpty(req.GetDisplayName(), "WA protocol profile"),
		AppVersion:        nativeAppVersion(req.GetAppVersion()),
		Status:            waappv1.ProtocolProfileStatus_PROTOCOL_PROFILE_STATUS_ACTIVE,
		Capabilities:      req.GetCapabilities(),
		RegistrationFlows: req.GetRegistrationFlows(),
		MessageTransports: req.GetMessageTransports(),
		DiscoveredAt:      timestamppb.New(now),
		Audit:             &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)},
	}
	if err := s.store.SaveProtocolProfile(ctx, profile); err != nil {
		return &waappv1.RecordProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RecordProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *discoveryHandler) GetProtocolProfile(ctx context.Context, req *waappv1.GetProtocolProfileRequest) (*waappv1.GetProtocolProfileResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.GetProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	profile, err := s.store.GetProtocolProfile(ctx, req.GetProtocolProfileId())
	if err != nil {
		return &waappv1.GetProtocolProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetProtocolProfileResponse{ProtocolProfile: profile}, nil
}

func (s *profileHandler) CreateWAAccount(ctx context.Context, req *waappv1.CreateWAAccountRequest) (*waappv1.CreateWAAccountResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.CreateWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	phone := normalizePhone(req.GetPhone())
	if phone.GetE164Number() == "" {
		return &waappv1.CreateWAAccountResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "phone is required", false))}, nil
	}
	if existing, err := s.store.FindWAAccountByPhone(ctx, phone.GetE164Number()); err == nil {
		return &waappv1.CreateWAAccountResponse{Account: existing}, nil
	}
	now := s.clock.Now()
	account := newWAAccount(s.ids.NewID("waacc_"), "", phone, waappv1.WAAccountStatus_WA_ACCOUNT_STATUS_PENDING_REGISTRATION, &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)})
	account, err := s.saveWAAccount(ctx, account)
	if err != nil {
		return &waappv1.CreateWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.CreateWAAccountResponse{Account: account}, nil
}

func (s *profileHandler) GetWAAccount(ctx context.Context, req *waappv1.GetWAAccountRequest) (*waappv1.GetWAAccountResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.GetWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.GetWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetWAAccountResponse{Account: account}, nil
}

func (s *profileHandler) ListWAAccounts(ctx context.Context, req *waappv1.ListWAAccountsRequest) (*waappv1.ListWAAccountsResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.ListWAAccountsResponse{Error: shared.ToProtoError(err)}, nil
	}
	accounts, nextCursor, err := s.listWAAccounts(ctx, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListWAAccountsResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.ListWAAccountsResponse{Accounts: accounts, NextCursor: nextCursor}, nil
}

func (s *profileHandler) DeleteWAAccount(ctx context.Context, req *waappv1.DeleteWAAccountRequest) (*waappv1.DeleteWAAccountResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	found, err := s.deleteWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(err)}, nil
	}
	if !found {
		return &waappv1.DeleteWAAccountResponse{Error: shared.ToProtoError(shared.NewError(waappv1.WaErrorCode_WA_ERROR_CODE_WA_ACCOUNT_NOT_FOUND, "WA account not found", false))}, nil
	}
	return &waappv1.DeleteWAAccountResponse{Success: true}, nil
}

func (s *profileHandler) DeletePendingRegistrationWAAccounts(ctx context.Context, req *waappv1.DeletePendingRegistrationWAAccountsRequest) (*waappv1.DeletePendingRegistrationWAAccountsResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.DeletePendingRegistrationWAAccountsResponse{Error: shared.ToProtoError(err)}, nil
	}
	deleted, err := s.deletePendingRegistrationWAAccounts(ctx)
	if err != nil {
		return &waappv1.DeletePendingRegistrationWAAccountsResponse{DeletedCount: int32(deleted), Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.DeletePendingRegistrationWAAccountsResponse{DeletedCount: int32(deleted)}, nil
}

func (s *profileHandler) PrepareClientProfile(ctx context.Context, req *waappv1.PrepareClientProfileRequest) (*waappv1.PrepareClientProfileResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	account, err := s.getWAAccount(ctx, accountID)
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	protocol, err := s.store.GetProtocolProfile(ctx, req.GetProtocolProfileId())
	if err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	profile := &waappv1.ClientProfile{ClientProfileId: s.ids.NewID("wacp_"), WaAccountId: waAccountID(account), ProtocolProfileId: req.GetProtocolProfileId(), Status: waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_PREPARING, RegistrationKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, MessagingKeyState: waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_PENDING, Audit: &waappv1.AuditStamp{CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now)}}
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	runErr := s.runner.PrepareClientProfile(ctx, wacore.EngineProfileInput{WAAccountID: waAccountID(account), ClientProfileID: profile.GetClientProfileId(), ProtocolProfileID: req.GetProtocolProfileId(), AppVersion: protocolAppVersion(protocol), Phone: account.GetPhone()})
	profile.Audit.UpdatedAt = timestamppb.New(s.clock.Now())
	if runErr != nil {
		profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_REJECTED
		profile.RegistrationKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_INVALID
		profile.MessagingKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_INVALID
		profile.LastError = shared.ToProtoError(runErr)
	} else {
		profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_READY
		profile.RegistrationKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
		profile.MessagingKeyState = waappv1.KeyMaterialStatus_KEY_MATERIAL_STATUS_READY
	}
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.PrepareClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.PrepareClientProfileResponse{ClientProfile: profile, Error: profile.GetLastError()}, nil
}

func (s *profileHandler) GetClientProfile(ctx context.Context, req *waappv1.GetClientProfileRequest) (*waappv1.GetClientProfileResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.GetClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	profile, err := s.store.GetClientProfile(ctx, req.GetClientProfileId())
	if err != nil {
		return &waappv1.GetClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.GetClientProfileResponse{ClientProfile: s.attachClientProfileRuntime(ctx, profile)}, nil
}

func (s *profileHandler) ListClientProfiles(ctx context.Context, req *waappv1.ListClientProfilesRequest) (*waappv1.ListClientProfilesResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.ListClientProfilesResponse{Error: shared.ToProtoError(err)}, nil
	}
	accountID, err := requireWAAccountID(req.GetWaAccountId())
	if err != nil {
		return &waappv1.ListClientProfilesResponse{Error: shared.ToProtoError(err)}, nil
	}
	if _, err := s.getWAAccount(ctx, accountID); err != nil {
		return &waappv1.ListClientProfilesResponse{Error: shared.ToProtoError(err)}, nil
	}
	profiles, nextCursor, err := s.store.ListClientProfiles(ctx, accountID, req.GetCursor(), int(req.GetLimit()))
	if err != nil {
		return &waappv1.ListClientProfilesResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.ListClientProfilesResponse{ClientProfiles: s.attachClientProfilesRuntime(ctx, profiles), NextCursor: nextCursor}, nil
}

func (s *profileHandler) RetireClientProfile(ctx context.Context, req *waappv1.RetireClientProfileRequest) (*waappv1.RetireClientProfileResponse, error) {
	if err := shared.ValidateContext(req.GetContext()); err != nil {
		return &waappv1.RetireClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	profile, err := s.store.GetClientProfile(ctx, req.GetClientProfileId())
	if err != nil {
		return &waappv1.RetireClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	profile.Status = waappv1.ClientProfileStatus_CLIENT_PROFILE_STATUS_RETIRED
	profile.Audit.UpdatedAt = timestamppb.New(s.clock.Now())
	if err := s.store.SaveClientProfile(ctx, profile); err != nil {
		return &waappv1.RetireClientProfileResponse{Error: shared.ToProtoError(err)}, nil
	}
	return &waappv1.RetireClientProfileResponse{ClientProfile: profile}, nil
}

func normalizePhone(phone *waappv1.PhoneTarget) *waappv1.PhoneTarget {
	if phone == nil {
		return &waappv1.PhoneTarget{}
	}
	cc := strings.TrimPrefix(strings.TrimSpace(phone.GetCountryCallingCode()), "+")
	national := strings.TrimSpace(phone.GetNationalNumber())
	e164 := strings.TrimSpace(phone.GetE164Number())
	if e164 == "" && cc != "" && national != "" {
		e164 = "+" + cc + national
	}
	if e164 != "" && !strings.HasPrefix(e164, "+") {
		e164 = "+" + e164
	}
	return &waappv1.PhoneTarget{E164Number: e164, CountryCallingCode: cc, NationalNumber: national, CountryIso2: strings.ToUpper(strings.TrimSpace(phone.GetCountryIso2()))}
}

func protocolAppVersion(profile *waappv1.ProtocolProfile) string {
	if profile == nil {
		return defaultWAAppVersion
	}
	return nativeAppVersion(profile.GetAppVersion())
}

func (s *serverCore) clientProfileAppVersion(ctx context.Context, profile *waappv1.ClientProfile) string {
	if s == nil || profile == nil {
		return defaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, profile.GetProtocolProfileId())
	if err != nil {
		return defaultWAAppVersion
	}
	if protocol.GetProtocolProfileId() == "waproto_native" && nativeAppVersion(protocol.GetAppVersion()) != defaultWAAppVersion {
		protocol.AppVersion = defaultWAAppVersion
		_ = s.store.SaveProtocolProfile(ctx, protocol)
	}
	return protocolAppVersion(protocol)
}

func (s *serverCore) protocolIDAppVersion(ctx context.Context, protocolProfileID string) string {
	if s == nil || strings.TrimSpace(protocolProfileID) == "" {
		return defaultWAAppVersion
	}
	protocol, err := s.store.GetProtocolProfile(ctx, protocolProfileID)
	if err != nil {
		return defaultWAAppVersion
	}
	if protocol.GetProtocolProfileId() == "waproto_native" && nativeAppVersion(protocol.GetAppVersion()) != defaultWAAppVersion {
		protocol.AppVersion = defaultWAAppVersion
		_ = s.store.SaveProtocolProfile(ctx, protocol)
	}
	return protocolAppVersion(protocol)
}

func (s *serverCore) loginStateAppVersion(ctx context.Context, loginState *waappv1.LoginState) string {
	if s == nil || loginState == nil {
		return defaultWAAppVersion
	}
	profile, err := s.store.GetClientProfile(ctx, loginState.GetClientProfileId())
	if err != nil {
		return defaultWAAppVersion
	}
	return s.clientProfileAppVersion(ctx, profile)
}
