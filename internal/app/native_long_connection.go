package app

import (
	"context"
	"sync"
	"time"
)

const (
	longConnectionChatdAttemptTimeout = 20 * time.Second
	longConnectionChatdOpenTimeout    = 45 * time.Second
)

type longConnectionNativeEngine struct {
	*NativeEngine

	mu      sync.Mutex
	session *chatdSession
}

func newLongConnectionNativeEngine(engine *NativeEngine) *longConnectionNativeEngine {
	return &longConnectionNativeEngine{NativeEngine: engine}
}

func (e *longConnectionNativeEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closeLocked()
}

func (e *longConnectionNativeEngine) ReceiveMessageBatch(ctx context.Context, input EngineMessageInput) EngineMessageBatchResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	session, err := e.ensureSessionWithTimeoutLocked(ctx, input)
	if err != nil {
		e.closeLocked()
		return EngineMessageBatchResult{Err: chatdReceiveError(err)}
	}
	now := e.clock.Now()
	messages, payloads, update, err := session.receiveBatch(input, now)
	if err != nil {
		e.closeLocked()
		session, retryErr := e.ensureSessionWithTimeoutLocked(ctx, input)
		if retryErr != nil {
			return EngineMessageBatchResult{Err: chatdReceiveError(retryErr)}
		}
		now = e.clock.Now()
		messages, payloads, update, err = session.receiveBatch(input, now)
		if err != nil {
			e.closeLocked()
			return EngineMessageBatchResult{Err: chatdReceiveError(err)}
		}
	}
	if len(payloads) > 0 || len(update.ContactHints) > 0 || update.RoutingInfo != "" || update.Endpoint.Host != "" || update.ServerStaticPublic != "" {
		state, err := e.loadState(ctx, input.ClientProfileID)
		if err != nil {
			e.closeLocked()
			return EngineMessageBatchResult{Err: err}
		}
		if applyChatdReceiveState(&state, input, payloads, update) {
			if err := e.saveState(ctx, input.ClientProfileID, state); err != nil {
				e.closeLocked()
				return EngineMessageBatchResult{Err: err}
			}
		}
	}
	return EngineMessageBatchResult{Messages: messages, Contacts: contactsFromContactHints(input.WAAccountID, nil, update.ContactHints, now)}
}

func (e *longConnectionNativeEngine) ensureSessionWithTimeoutLocked(ctx context.Context, input EngineMessageInput) (*chatdSession, error) {
	if e.session != nil {
		return e.session, nil
	}
	openCtx, cancel := context.WithTimeout(ctx, longConnectionChatdOpenTimeout)
	defer cancel()
	return e.ensureSessionLocked(openCtx, input)
}

func (e *longConnectionNativeEngine) ensureSessionLocked(ctx context.Context, input EngineMessageInput) (*chatdSession, error) {
	if e.session != nil {
		return e.session, nil
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return nil, err
	}
	state.ensureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		if err := e.saveState(ctx, input.ClientProfileID, state); err != nil {
			return nil, err
		}
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return nil, err
	}
	cfg := chatdConfigForState(proxyURL, state, longConnectionChatdAttemptTimeout)
	cfg.Endpoints = longConnectionChatdEndpoints(state)
	client := newChatdClient(cfg)
	session, err := client.openSession(ctx, state, input.RegisteredIdentityID, defaultLoginPayload, defaultWAAppVersion)
	if err != nil {
		return nil, err
	}
	e.session = session
	return session, nil
}

func longConnectionChatdEndpoints(state nativeState) []chatdEndpoint {
	endpoints := []chatdEndpoint{}
	seen := map[string]struct{}{}
	add := func(host string, port int) {
		endpoint := chatdEndpoint{Host: host, Port: port}.normalized(defaultChatdHost, defaultChatdPort)
		if endpoint.Host == "" || endpoint.Port != defaultChatdPort {
			return
		}
		key := endpoint.address()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	if state.ChatConnection.LastHost != "" {
		add(state.ChatConnection.LastHost, state.ChatConnection.LastPort)
	}
	add(defaultChatdHost, defaultChatdPort)
	add(chatdFallbackHost, defaultChatdPort)
	return endpoints
}

func (e *longConnectionNativeEngine) closeLocked() error {
	if e.session == nil {
		return nil
	}
	err := e.session.Close()
	e.session = nil
	return err
}

var _ ProtocolEngine = (*longConnectionNativeEngine)(nil)
var _ interface{ Close() error } = (*longConnectionNativeEngine)(nil)
