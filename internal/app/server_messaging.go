package app

import (
	"context"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) OpenMessageSession(ctx context.Context, req *waappv1.OpenMessageSessionRequest) (*waappv1.OpenMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	workspaceID := req.GetContext().GetWorkspaceId()
	account, profile, err := s.waAccountAndProfile(ctx, workspaceID, req.GetWaAccountId(), req.GetClientProfileId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	waID := waAccountID(account)
	if req.GetRegisteredIdentityId() == "" {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "registered_identity_id is required", false))}, nil
	}
	loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, workspaceID, req.GetRegisteredIdentityId())
	if err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	if loginState.GetStatus() != waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE || loginState.GetWaAccountId() != waID || loginState.GetClientProfileId() != req.GetClientProfileId() {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "registered identity is not active for WA account profile", false))}, nil
	}
	now := s.clock.Now()
	session := &waappv1.MessageSession{MessageSessionId: s.ids.NewID("wasess_"), WaAccountId: waID, ClientProfileId: req.GetClientProfileId(), RegisteredIdentityId: req.GetRegisteredIdentityId(), ProtocolProfileId: firstNonEmpty(req.GetProtocolProfileId(), profile.GetProtocolProfileId()), Status: waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_OPEN, OpenedAt: timestamppb.New(now), LastSeenAt: timestamppb.New(now)}
	if err := s.store.SaveMessageSession(ctx, session, workspaceID); err != nil {
		return &waappv1.OpenMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	if err := s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute); err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = ToProtoError(err)
		_ = s.store.SaveMessageSession(ctx, session, workspaceID)
		return &waappv1.OpenMessageSessionResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	return &waappv1.OpenMessageSessionResponse{Session: session}, nil
}

func (s *Server) GetMessageSession(ctx context.Context, req *waappv1.GetMessageSessionRequest) (*waappv1.GetMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	session, err := s.store.GetMessageSession(ctx, req.GetContext().GetWorkspaceId(), req.GetMessageSessionId())
	if err != nil {
		return &waappv1.GetMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.GetMessageSessionResponse{Session: session}, nil
}

func (s *Server) ReceiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest) (*waappv1.ReceiveMessageBatchResponse, error) {
	return s.receiveMessageBatch(ctx, req, s.runner)
}

func (s *Server) receiveMessageBatch(ctx context.Context, req *waappv1.ReceiveMessageBatchRequest, runner ProtocolEngine) (*waappv1.ReceiveMessageBatchResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Error: ToProtoError(err)}, nil
	}
	workspaceID := req.GetContext().GetWorkspaceId()
	session, err := s.store.GetMessageSession(ctx, workspaceID, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Error: ToProtoError(err)}, nil
	}
	if session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED || session.GetStatus() == waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_CONFLICT, "message session is not open", false))}, nil
	}
	if runner == nil {
		runner = s.runner
	}
	result := runner.ReceiveMessageBatch(ctx, EngineMessageInput{WorkspaceID: workspaceID, WAAccountID: session.GetWaAccountId(), ClientProfileID: session.GetClientProfileId(), RegisteredIdentityID: session.GetRegisteredIdentityId(), ProtocolProfileID: session.GetProtocolProfileId(), MessageSessionID: session.GetMessageSessionId(), WaitTimeout: durationFromProto(req.GetWaitTimeout()), MaxMessages: int(req.GetMaxMessages())})
	if result.Err != nil {
		session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_FAILED
		session.LastError = ToProtoError(result.Err)
		_ = s.store.SaveMessageSession(ctx, session, workspaceID)
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(result.Err)}, nil
	}
	if err := s.store.SaveInboundMessages(ctx, workspaceID, result.Messages); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	session.LastSeenAt = timestamppb.New(now)
	_ = s.runtime.OpenSessionLease(ctx, session.GetMessageSessionId(), 5*time.Minute)
	if loginState, err := s.store.GetLoginStateByRegisteredIdentity(ctx, workspaceID, session.GetRegisteredIdentityId()); err == nil && loginState.GetWaAccountId() == session.GetWaAccountId() && loginState.GetClientProfileId() == session.GetClientProfileId() {
		if loginState.Audit == nil {
			loginState.Audit = &waappv1.AuditStamp{CreatedAt: timestamppb.New(now)}
		}
		loginState.Status = waappv1.LoginStateStatus_LOGIN_STATE_STATUS_ACTIVE
		loginState.LastVerifiedAt = timestamppb.New(now)
		loginState.LastError = nil
		loginState.Audit.UpdatedAt = timestamppb.New(now)
		_ = s.store.SaveLoginState(ctx, loginState, workspaceID, "native-db:"+session.GetClientProfileId())
	}
	if err := s.store.SaveMessageSession(ctx, session, workspaceID); err != nil {
		return &waappv1.ReceiveMessageBatchResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	return &waappv1.ReceiveMessageBatchResponse{Messages: result.Messages, Session: session}, nil
}

func (s *Server) GetLongConnectionStatus(ctx context.Context, req *waappv1.GetLongConnectionStatusRequest) (*waappv1.GetLongConnectionStatusResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.GetLongConnectionStatusResponse{Error: ToProtoError(err)}, nil
	}
	if req.GetWaAccountId() != "" {
		accountID, err := requireWAAccountID(req.GetWaAccountId())
		if err != nil {
			return &waappv1.GetLongConnectionStatusResponse{Error: ToProtoError(err)}, nil
		}
		req.WaAccountId = accountID
	}
	if s.longConnections == nil {
		return &waappv1.GetLongConnectionStatusResponse{}, nil
	}
	return &waappv1.GetLongConnectionStatusResponse{Connections: s.longConnections.Snapshots(req)}, nil
}

func (s *Server) AcknowledgeMessage(ctx context.Context, req *waappv1.AcknowledgeMessageRequest) (*waappv1.AcknowledgeMessageResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	workspaceID := req.GetContext().GetWorkspaceId()
	msg, err := s.store.GetInboundMessage(ctx, workspaceID, req.GetMessageId())
	if err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	if msg.GetMessageSessionId() != req.GetMessageSessionId() {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "message does not belong to session", false))}, nil
	}
	msg.AckStatus = waappv1.MessageAckStatus_MESSAGE_ACK_STATUS_ACKED
	if err := s.store.SaveInboundMessages(ctx, workspaceID, []*waappv1.InboundMessage{msg}); err != nil {
		return &waappv1.AcknowledgeMessageResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.AcknowledgeMessageResponse{Message: msg}, nil
}

func (s *Server) CloseMessageSession(ctx context.Context, req *waappv1.CloseMessageSessionRequest) (*waappv1.CloseMessageSessionResponse, error) {
	if err := validateContext(req.GetContext()); err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	workspaceID := req.GetContext().GetWorkspaceId()
	session, err := s.store.GetMessageSession(ctx, workspaceID, req.GetMessageSessionId())
	if err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	now := s.clock.Now()
	session.Status = waappv1.MessageSessionStatus_MESSAGE_SESSION_STATUS_CLOSED
	session.ClosedAt = timestamppb.New(now)
	if err := s.runtime.CloseSessionLease(ctx, session.GetMessageSessionId()); err != nil {
		return &waappv1.CloseMessageSessionResponse{Session: session, Error: ToProtoError(err)}, nil
	}
	if err := s.store.SaveMessageSession(ctx, session, workspaceID); err != nil {
		return &waappv1.CloseMessageSessionResponse{Error: ToProtoError(err)}, nil
	}
	return &waappv1.CloseMessageSessionResponse{Session: session}, nil
}
