package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	wav1 "github.com/byte-v-forge/common-lib/gen/go/byte/v/forge/contracts/wa/v1"
	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	longConnectionWaitTimeout  = 25 * time.Second
	longConnectionMaxBackoff   = 30 * time.Second
	longConnectionDecryptLimit = 100
)

type LongConnectionManager struct {
	server *Server

	mu      sync.Mutex
	rootCtx context.Context
	cancel  context.CancelFunc
	entries map[string]*longConnectionEntry
}

type longConnectionEntry struct {
	cancel   context.CancelFunc
	snapshot *waappv1.LongConnectionState
}

func NewLongConnectionManager(server *Server) *LongConnectionManager {
	return &LongConnectionManager{server: server, entries: map[string]*longConnectionEntry{}}
}

func (m *LongConnectionManager) Run(ctx context.Context) error {
	if m == nil || m.server == nil {
		return nil
	}
	rootCtx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.rootCtx = rootCtx
	m.cancel = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.stopAll()
	}()
	if err := m.restore(rootCtx); err != nil {
		return err
	}
	<-rootCtx.Done()
	return nil
}

func (m *LongConnectionManager) Ensure(ctx context.Context, workspaceID string, loginState *waappv1.LoginState) {
	if m == nil || loginState == nil || loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
		return
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return
	}
	m.mu.Lock()
	rootCtx := m.rootCtx
	if rootCtx == nil {
		m.mu.Unlock()
		return
	}
	key := longConnectionKey(workspaceID, loginState)
	if existing, ok := m.entries[key]; ok && existing.cancel != nil {
		m.mu.Unlock()
		return
	}
	entryCtx, cancel := context.WithCancel(rootCtx)
	snapshot := &waappv1.LongConnectionState{
		WorkspaceId:          workspaceID,
		LoginStateId:         loginState.GetLoginStateId(),
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
		Status:               waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STARTING,
		HeartbeatSupported:   true,
		StartedAt:            timestamppb.New(m.server.clock.Now()),
	}
	m.entries[key] = &longConnectionEntry{cancel: cancel, snapshot: snapshot}
	m.mu.Unlock()
	go m.runEntry(entryCtx, workspaceID, proto.Clone(loginState).(*waappv1.LoginState), key)
	_ = ctx
}

func (m *LongConnectionManager) Snapshots(req *waappv1.GetLongConnectionStatusRequest) []*waappv1.LongConnectionState {
	if m == nil || req == nil {
		return nil
	}
	workspaceID := req.GetContext().GetWorkspaceId()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*waappv1.LongConnectionState{}
	for _, entry := range m.entries {
		if entry == nil || entry.snapshot == nil {
			continue
		}
		s := entry.snapshot
		if workspaceID != "" && s.GetWorkspaceId() != workspaceID {
			continue
		}
		if req.GetLoginStateId() != "" && s.GetLoginStateId() != req.GetLoginStateId() {
			continue
		}
		if req.GetRegisteredIdentityId() != "" && s.GetRegisteredIdentityId() != req.GetRegisteredIdentityId() {
			continue
		}
		if req.GetWaAccountId() != "" && s.GetWaAccountId() != req.GetWaAccountId() {
			continue
		}
		if req.GetClientProfileId() != "" && s.GetClientProfileId() != req.GetClientProfileId() {
			continue
		}
		out = append(out, proto.Clone(s).(*waappv1.LongConnectionState))
	}
	return out
}

func (m *LongConnectionManager) restore(ctx context.Context) error {
	records, err := m.server.store.ListActiveLoginStates(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return nil
		}
		runner, err := m.server.longConnectionRunner(ctx)
		if err != nil {
			m.recordRestoreFailure(record.WorkspaceID, record.LoginState, err)
			m.Ensure(ctx, record.WorkspaceID, record.LoginState)
			continue
		}
		resp, err := m.server.checkLoginState(ctx, &waappv1.CheckLoginStateRequest{
			Context:              &waappv1.RequestContext{WorkspaceId: record.WorkspaceID, RequestId: m.server.ids.NewID("wa-restore_"), CorrelationId: record.LoginState.GetLoginStateId()},
			LoginStateId:         record.LoginState.GetLoginStateId(),
			WaAccountId:          record.LoginState.GetWaAccountId(),
			ClientProfileId:      record.LoginState.GetClientProfileId(),
			RegisteredIdentityId: record.LoginState.GetRegisteredIdentityId(),
			RemoteTimeout:        durationpb.New(10 * time.Second),
		}, runner)
		if err != nil {
			m.recordRestoreFailure(record.WorkspaceID, record.LoginState, err)
			continue
		}
		if resp.GetCheck().GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE && resp.GetLoginState().GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			m.Ensure(ctx, record.WorkspaceID, resp.GetLoginState())
			continue
		}
		if resp.GetCheck().GetStatus() == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE && resp.GetLoginState().GetStatus() == waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE {
			m.Ensure(ctx, record.WorkspaceID, resp.GetLoginState())
			continue
		}
		if resp.GetError() != nil {
			m.recordRestoreFailure(record.WorkspaceID, record.LoginState, errorFromProto(resp.GetError()))
		}
	}
	return nil
}

func (m *LongConnectionManager) runEntry(ctx context.Context, workspaceID string, loginState *waappv1.LoginState, key string) {
	backoff := 2 * time.Second
	reconnects := int32(0)
	defer m.markStopped(key)
	for ctx.Err() == nil {
		m.update(key, func(snapshot *waappv1.LongConnectionState) {
			if reconnects > 0 {
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
			} else {
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STARTING
			}
			snapshot.ReconnectCount = reconnects
		})
		session, err := m.openSession(ctx, workspaceID, loginState)
		if err != nil {
			m.recordLoopError(key, reconnects, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			reconnects++
			continue
		}
		backoff = 2 * time.Second
		m.update(key, func(snapshot *waappv1.LongConnectionState) {
			snapshot.MessageSessionId = session.GetMessageSessionId()
			snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED
			snapshot.LastConnectedAt = timestamppb.New(m.server.clock.Now())
			snapshot.LastError = nil
		})
		runner, err := m.server.longConnectionRunner(ctx)
		if err != nil {
			m.recordLoopError(key, reconnects, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			reconnects++
			continue
		}
		m.decryptPendingMessages(ctx, workspaceID, session, runner)
		for ctx.Err() == nil {
			resp, err := m.server.receiveMessageBatch(ctx, &waappv1.ReceiveMessageBatchRequest{Context: &waappv1.RequestContext{WorkspaceId: workspaceID, RequestId: m.server.ids.NewID("wa-rx_"), CorrelationId: loginState.GetLoginStateId()}, MessageSessionId: session.GetMessageSessionId(), MaxMessages: 10, WaitTimeout: durationpb.New(longConnectionWaitTimeout)}, runner)
			if err != nil {
				m.recordLoopError(key, reconnects, err)
				break
			}
			if resp.GetError() != nil {
				m.recordLoopError(key, reconnects, errorFromProto(resp.GetError()))
				break
			}
			now := m.server.clock.Now()
			messages := resp.GetMessages()
			m.update(key, func(snapshot *waappv1.LongConnectionState) {
				snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_HEARTBEAT_WAITING
				snapshot.LastHeartbeatAt = timestamppb.New(now)
				snapshot.LastError = nil
				if len(messages) > 0 {
					snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_CONNECTED
					snapshot.LastMessageAt = timestamppb.New(now)
				}
			})
			m.decryptReceivedMessages(ctx, workspaceID, session, messages, runner)
		}
		if ctx.Err() != nil {
			return
		}
		reconnects++
		_, _ = m.server.CloseMessageSession(context.WithoutCancel(ctx), &waappv1.CloseMessageSessionRequest{Context: &waappv1.RequestContext{WorkspaceId: workspaceID}, MessageSessionId: session.GetMessageSessionId(), Reason: "long connection reconnect"})
		if !sleepContext(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

func (m *LongConnectionManager) openSession(ctx context.Context, workspaceID string, loginState *waappv1.LoginState) (*waappv1.MessageSession, error) {
	resp, err := m.server.OpenMessageSession(ctx, &waappv1.OpenMessageSessionRequest{
		Context:              &waappv1.RequestContext{WorkspaceId: workspaceID, RequestId: m.server.ids.NewID("wa-open_"), CorrelationId: loginState.GetLoginStateId()},
		WaAccountId:          loginState.GetWaAccountId(),
		ClientProfileId:      loginState.GetClientProfileId(),
		RegisteredIdentityId: loginState.GetRegisteredIdentityId(),
	})
	if err != nil {
		return nil, err
	}
	if resp.GetError() != nil {
		return nil, errorFromProto(resp.GetError())
	}
	return resp.GetSession(), nil
}

func (m *LongConnectionManager) decryptPendingMessages(ctx context.Context, workspaceID string, session *waappv1.MessageSession, runner ProtocolEngine) {
	messages, err := m.server.store.ListPendingEncryptedInboundMessages(ctx, workspaceID, session.GetWaAccountId(), session.GetClientProfileId(), longConnectionDecryptLimit)
	if err != nil {
		log.Printf("WA long connection pending decrypt load failed: %v", sanitizeEventPublishError(err))
		return
	}
	if len(messages) == 0 {
		return
	}
	log.Printf("WA long connection retry pending decrypt: count=%d", len(messages))
	m.decryptReceivedMessages(ctx, workspaceID, session, messages, runner)
}

func (m *LongConnectionManager) decryptReceivedMessages(ctx context.Context, workspaceID string, session *waappv1.MessageSession, messages []*waappv1.InboundMessage, runner ProtocolEngine) {
	for _, msg := range messages {
		if msg.GetEncryptionState() == waappv1.MessageEncryptionState_MESSAGE_ENCRYPTION_STATE_PLAINTEXT && !strings.HasPrefix(msg.GetPayloadRef(), "plaintext:") {
			continue
		}
		resp, err := m.server.decryptMessage(ctx, &waappv1.DecryptMessageRequest{Context: &waappv1.RequestContext{WorkspaceId: workspaceID, RequestId: m.server.ids.NewID("wa-dec_"), CorrelationId: session.GetRegisteredIdentityId()}, MessageId: msg.GetMessageId(), SessionCommitPolicy: waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE, IncludeSensitivePlaintext: true}, runner, wav1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("WA long connection decrypt failed: message_id=%s error=%v", msg.GetMessageId(), sanitizeEventPublishError(err))
		}
		if resp != nil && resp.GetError() != nil {
			log.Printf("WA long connection decrypt failed: message_id=%s code=%s retryable=%t", msg.GetMessageId(), resp.GetError().GetCode().String(), resp.GetError().GetRetryable())
		}
	}
}

func (m *LongConnectionManager) recordRestoreFailure(workspaceID string, loginState *waappv1.LoginState, err error) {
	if loginState == nil {
		return
	}
	key := longConnectionKey(workspaceID, loginState)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key] = &longConnectionEntry{snapshot: &waappv1.LongConnectionState{WorkspaceId: workspaceID, LoginStateId: loginState.GetLoginStateId(), WaAccountId: loginState.GetWaAccountId(), ClientProfileId: loginState.GetClientProfileId(), RegisteredIdentityId: loginState.GetRegisteredIdentityId(), Status: waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_FAILED, HeartbeatSupported: true, LastError: ToProtoError(err)}}
}

func (m *LongConnectionManager) recordLoopError(key string, reconnects int32, err error) {
	m.update(key, func(snapshot *waappv1.LongConnectionState) {
		snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_RECONNECTING
		snapshot.ReconnectCount = reconnects
		snapshot.LastError = ToProtoError(err)
	})
}

func (m *LongConnectionManager) update(key string, mutate func(*waappv1.LongConnectionState)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.snapshot == nil {
		return
	}
	mutate(entry.snapshot)
}

func (m *LongConnectionManager) markStopped(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry := m.entries[key]
	if entry == nil || entry.snapshot == nil {
		return
	}
	entry.cancel = nil
	entry.snapshot.Status = waappv1.LongConnectionStatus_LONG_CONNECTION_STATUS_STOPPED
}

func (m *LongConnectionManager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		if entry != nil && entry.cancel != nil {
			entry.cancel()
		}
	}
}

func (s *Server) ensureLongConnection(ctx context.Context, workspaceID string, loginState *waappv1.LoginState) {
	if s != nil && s.longConnections != nil {
		s.longConnections.Ensure(ctx, workspaceID, loginState)
	}
}

func (s *Server) longConnectionRunner(ctx context.Context) (ProtocolEngine, error) {
	engine, ok := s.runner.(*NativeEngine)
	if !ok || strings.TrimSpace(engine.activeProxyURL) != "" {
		return s.runner, nil
	}
	if s.proxyRuntime == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "PROXY_RUNTIME_API_BASE_URL is required", false)
	}
	proxyURL, err := s.proxyRuntime.FixedProxyURL(ctx)
	if err != nil {
		return nil, err
	}
	proxyEngine, err := engine.WithProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	return proxyEngine, nil
}

func longConnectionKey(workspaceID string, loginState *waappv1.LoginState) string {
	return workspaceID + ":" + firstNonEmpty(loginState.GetRegisteredIdentityId(), loginState.GetLoginStateId())
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return 2 * time.Second
	}
	current *= 2
	if current > longConnectionMaxBackoff {
		return longConnectionMaxBackoff
	}
	return current
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
