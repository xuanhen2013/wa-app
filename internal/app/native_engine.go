package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	defaultWAAppVersion   = "2.26.21.73"
	defaultWAExistURL     = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/exist&"
	defaultWACodeURL      = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/code&"
	defaultWARegisterURL  = "https://y9yrsygcg6.execute-api.us-east-1.amazonaws.com/s/s?_=/v2/register&"
	defaultNativeHTTPHost = "v.whatsapp.net"
)

type NativeEngine struct {
	stateStore     Store
	activeProxyURL string
	http           *nativeHTTPClient
	clock          Clock
	ids            IDGenerator
}

func NewNativeEngine(stateStore Store, clock Clock, ids IDGenerator) (*NativeEngine, error) {
	if stateStore == nil {
		return nil, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, "native state store is required", false)
	}
	if clock == nil {
		clock = SystemClock{}
	}
	if ids == nil {
		ids = RandomIDGenerator{}
	}
	hc, err := newNativeHTTPClient("")
	if err != nil {
		return nil, err
	}
	return &NativeEngine{stateStore: stateStore, http: hc, clock: clock, ids: ids}, nil
}

func (e *NativeEngine) WithProxyURL(proxyURL string) (*NativeEngine, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	hc, err := newNativeHTTPClient(proxyURL)
	if err != nil {
		return nil, err
	}
	return &NativeEngine{stateStore: e.stateStore, activeProxyURL: proxyURL, http: hc, clock: e.clock, ids: e.ids}, nil
}

func (e *NativeEngine) PrepareClientProfile(ctx context.Context, input EngineProfileInput) error {
	_ = ctx
	state, err := newNativeState(input.Phone, defaultWAAppVersion)
	if err != nil {
		return err
	}
	return e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
}

func (e *NativeEngine) ProbeAccount(ctx context.Context, input EngineRegistrationInput) EngineProbeResult {
	state, err := e.newState(input.Phone)
	if err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}
	}
	return e.probeAccountWithState(ctx, input, state)
}

func (e *NativeEngine) probeAccountWithState(ctx context.Context, input EngineRegistrationInput, state nativeState) EngineProbeResult {
	params, rawKeys := e.existParams(input.Phone, state)
	plain := renderNativePlain(params, rawKeys)
	client, err := e.httpForProxy()
	if err != nil {
		return EngineProbeResult{Status: waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED, Err: err}
	}
	data, _, err := client.postWASafe(ctx, defaultWAExistURL, plain, state.UserAgent)
	result := parseExistProbeResult(data)
	if err != nil {
		if result.Status == waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_UNKNOWN {
			result.Status = waappv1.AccountProbeStatus_ACCOUNT_PROBE_STATUS_REJECTED
		}
		result.AccountFlow = accountProbeFlowProbeFailed
		if result.Err == nil {
			result.Err = classifyHTTPError(data, err)
		}
	}
	return result
}

func (e *NativeEngine) RequestVerificationCode(ctx context.Context, input EngineRegistrationInput) EngineCodeResult {
	state, err := e.loadState(ctx, input.WorkspaceID, input.ClientProfileID)
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}
	}
	result, updated := e.requestVerificationCodeWithState(ctx, input, state)
	_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, updated)
	return result
}

func (e *NativeEngine) requestVerificationCodeWithState(ctx context.Context, input EngineRegistrationInput, state nativeState) (EngineCodeResult, nativeState) {
	params, rawKeys := e.codeParams(input.Phone, state)
	plain := renderNativePlain(params, rawKeys)
	client, err := e.httpForProxy()
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: err}, state
	}
	data, enc, err := client.postWASafe(ctx, defaultWACodeURL, plain, state.UserAgent)
	state.LastCodeParams = params
	state.LastCodeResult = sanitizeResponse(data)
	if enc != "" {
		state.LastCodeResult["enc_sha256"] = encHash(enc)
	}
	if err != nil {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: classifyHTTPError(data, err)}, state
	}
	status := waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_WAITING
	s := responseStatus(data)
	if s == "sent" || s == "ok" {
		status = waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_SENT
	} else if s != "" && s != "too_recent" {
		return EngineCodeResult{Status: waappv1.VerificationRequestStatus_VERIFICATION_REQUEST_STATUS_REJECTED, Err: waProtocolError(data, "verification request was rejected")}, state
	}
	return EngineCodeResult{Status: status, ExpectedCodeLength: int32(jsonNumber(data["length"])), ExpiresAt: e.clock.Now().Add(10 * time.Minute)}, state
}

func (e *NativeEngine) SubmitVerificationCode(ctx context.Context, input EngineSubmitInput) EngineRegisterResult {
	if strings.TrimSpace(input.Code) == "" {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, "verification code is required", false)}
	}
	state, err := e.loadState(ctx, input.WorkspaceID, input.ClientProfileID)
	if err != nil {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	params, rawKeys := e.registerParams(input.Phone, input.Code, state)
	plain := renderNativePlain(params, rawKeys)
	client, err := e.httpForProxy()
	if err != nil {
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: err}
	}
	data, enc, err := client.postWASafe(ctx, defaultWARegisterURL, plain, state.UserAgent)
	state.LastRegister = sanitizeResponse(data)
	if enc != "" {
		state.LastRegister["enc_sha256"] = encHash(enc)
	}
	if err != nil {
		_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: classifyHTTPError(data, err)}
	}
	if status := responseStatus(data); status != "ok" && status != "registered" {
		_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
		return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REJECTED, Err: waProtocolError(data, "registration was rejected")}
	}
	login := firstNonEmpty(jsonString(data["login"]), jsonString(data["jid"]), jsonString(data["registration_jid"]), state.CC+state.Phone)
	lid := firstNonEmpty(jsonString(data["lid"]), login)
	if login != "" {
		state.RegistrationJID = normalizeJID(login)
	}
	_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
	completedAt := e.clock.Now()
	return EngineRegisterResult{Status: waappv1.RegistrationStatus_REGISTRATION_STATUS_REGISTERED, RegisteredID: "waid_" + stableID(login), ServiceAccountID: lid, ServiceLoginID: login, CompletedAt: completedAt}
}

func (e *NativeEngine) CheckLoginState(ctx context.Context, input EngineLoginCheckInput) EngineLoginCheckResult {
	state, err := e.loadState(ctx, input.WorkspaceID, input.ClientProfileID)
	if err != nil {
		return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID, Err: err}
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNSPECIFIED, Err: err}
	}
	timeout := defaultChatdReadWindow
	if input.RemoteTimeout > 0 {
		timeout = input.RemoteTimeout
	}
	client := newChatdClient(chatdClientConfig{ProxyURL: proxyURL, Timeout: timeout})
	if err := client.checkLoginState(ctx, state, input, defaultWAAppVersion); err != nil {
		status := loginCheckStatusForError(err)
		return EngineLoginCheckResult{Status: status, Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "login state remote check failed", status == waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE)}
	}
	return EngineLoginCheckResult{Status: waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE}
}

func loginCheckStatusForError(err error) waappv1.LoginStateCheckStatus {
	if err == nil {
		return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_ACTIVE
	}
	lower := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "deadline", "proxy", "dial", "connect", "network", "tls", "no such host", "connection refused", "temporary"} {
		if strings.Contains(lower, marker) {
			return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_UNREACHABLE
		}
	}
	return waappv1.LoginStateCheckStatus_LOGIN_STATE_CHECK_STATUS_INVALID
}

func (e *NativeEngine) ReceiveMessageBatch(ctx context.Context, input EngineMessageInput) EngineMessageBatchResult {
	state, err := e.loadState(ctx, input.WorkspaceID, input.ClientProfileID)
	if err != nil {
		return EngineMessageBatchResult{Err: err}
	}
	state.ensureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineMessageBatchResult{Err: err}
	}
	client := newChatdClient(chatdClientConfig{ProxyURL: proxyURL})
	messages, payloads, err := client.receiveBatch(ctx, state, input, defaultWAAppVersion, e.ids, e.clock.Now())
	if err != nil {
		return EngineMessageBatchResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "native chatd receive failed", true)}
	}
	for _, payload := range payloads {
		ref := payloadRefForEnc(input.WAAccountID, payload.Payload)
		state.MessagePayloads[ref] = nativeMessagePayload{Sender: payload.Sender, EncType: payload.EncType, Path: payload.Path, Payload: b64u(payload.Payload)}
	}
	_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
	return EngineMessageBatchResult{Messages: messages}
}

func (e *NativeEngine) DecryptMessage(ctx context.Context, input EngineDecryptInput) EngineDecryptResult {
	_ = ctx
	if strings.HasPrefix(input.PayloadRef, "plaintext:") {
		plain := strings.TrimPrefix(input.PayloadRef, "plaintext:")
		decryptedID := e.ids.NewID("wadec_")
		text := &waappv1.SensitiveText{RedactedValue: redacted(plain), SecretRef: "plaintext:" + decryptedID}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: "inline:" + decryptedID, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now())}
		return EngineDecryptResult{DecryptedMessage: msg, Candidates: extractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids)}
	}
	if strings.HasPrefix(input.PayloadRef, "native-enc:") {
		state, err := e.loadState(ctx, input.WorkspaceID, input.ClientProfileID)
		if err != nil {
			return EngineDecryptResult{Err: err}
		}
		payload, ok := state.MessagePayloads[input.PayloadRef]
		if !ok {
			return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_MESSAGE_NOT_FOUND, "encrypted message payload ref not found", false)}
		}
		commit := input.SessionCommitPolicy == waappv1.SessionCommitPolicy_SESSION_COMMIT_POLICY_COMMIT_LEARNED_STATE
		output, err := decryptNativeSignalPayload(&state, payload, commit)
		if err != nil {
			return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_DECRYPTION_FAILED, "native Signal message decryption failed", true)}
		}
		if commit {
			_ = e.saveState(ctx, input.WorkspaceID, input.ClientProfileID, state)
		}
		decryptedID := e.ids.NewID("wadec_")
		plain := nativePlaintextText(output.plaintext)
		text := &waappv1.SensitiveText{RedactedValue: redacted(plain), SecretRef: "native-plain:" + decryptedID}
		if input.IncludePlaintextText {
			text.Value = plain
		}
		msg := &waappv1.DecryptedMessage{DecryptedMessageId: decryptedID, MessageId: input.MessageID, Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextRef: "native-plain:" + decryptedID, PlaintextText: text, DecryptedAt: timestamppb.New(e.clock.Now())}
		return EngineDecryptResult{DecryptedMessage: msg, Candidates: extractCandidates(input.MessageID, decryptedID, plain, input.IncludePlaintextText, e.clock.Now(), e.ids)}
	}
	return EngineDecryptResult{Err: NewError(waappv1.WaErrorCode_WA_ERROR_CODE_UNSUPPORTED_OPERATION, "payload ref scheme is not supported by native decryptor", false)}
}

func nativePlaintextText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	plain := string(raw)
	if readableText(plain) {
		return strings.TrimSpace(plain)
	}
	return strings.TrimSpace(strings.Join(printableSegments(raw), "\n"))
}

func readableText(value string) bool {
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return false
	}
	total := 0
	readable := 0
	for _, r := range value {
		total++
		if r == '\n' || r == '\r' || r == '\t' || (r >= 0x20 && r != 0x7f) {
			readable++
		}
	}
	return total > 0 && readable*100/total >= 90
}

func printableSegments(raw []byte) []string {
	segments := []string{}
	var current strings.Builder
	flush := func() {
		value := strings.TrimSpace(current.String())
		current.Reset()
		if len(value) >= 4 {
			segments = append(segments, value)
		}
	}
	for _, b := range raw {
		if b == '\n' || b == '\r' || b == '\t' || (b >= 0x20 && b <= 0x7e) {
			current.WriteByte(b)
			continue
		}
		flush()
	}
	flush()
	if len(segments) > 32 {
		return segments[:32]
	}
	return segments
}

func (e *NativeEngine) codeParams(phone *waappv1.PhoneTarget, state nativeState) (map[string]string, map[string]struct{}) {
	params := map[string]string{
		"cc":                phoneCC(phone),
		"in":                phoneNational(phone),
		"method":            "sms",
		"lg":                "en",
		"lc":                "US",
		"fdid":              state.Profile.FDID,
		"expid":             state.Profile.ExpID,
		"access_session_id": state.Profile.AccessSessionID,
		"id":                state.Profile.ID,
		"backup_token":      state.Profile.BackupToken,
		"authkey":           state.AuthKey,
		"e_ident":           state.KeyBundle.IdentityPublic,
		"e_keytype":         state.KeyBundle.KeyType,
		"e_regid":           state.KeyBundle.RegID,
		"e_skey_id":         state.KeyBundle.SignedKeyID,
		"e_skey_val":        state.KeyBundle.SignedKeyValue,
		"e_skey_sig":        state.KeyBundle.SignedKeySig,
	}
	if token := e.registrationToken(phone, state); token != "" {
		params["token"] = token
	}
	raw := map[string]struct{}{"id": {}, "backup_token": {}}
	for key, value := range state.Profile.AdditionalMapFields {
		if omitEmptyNativeOperatorField(key, value) {
			continue
		}
		params[key] = pctBytes([]byte(value))
		raw[key] = struct{}{}
	}
	return params, raw
}

func omitEmptyNativeOperatorField(key string, value string) bool {
	if strings.TrimSpace(value) != "" {
		return false
	}
	switch key {
	case "mcc", "mnc", "sim_mcc", "sim_mnc":
		return true
	default:
		return false
	}
}

func (e *NativeEngine) registerParams(phone *waappv1.PhoneTarget, code string, state nativeState) (map[string]string, map[string]struct{}) {
	params := map[string]string{
		"cc":                phoneCC(phone),
		"in":                phoneNational(phone),
		"method":            "sms",
		"lg":                firstNonEmpty(state.LastCodeParams["lg"], "en"),
		"lc":                firstNonEmpty(state.LastCodeParams["lc"], "US"),
		"fdid":              firstNonEmpty(state.LastCodeParams["fdid"], state.Profile.FDID),
		"expid":             firstNonEmpty(state.LastCodeParams["expid"], state.Profile.ExpID),
		"access_session_id": firstNonEmpty(state.LastCodeParams["access_session_id"], state.Profile.AccessSessionID),
		"id":                firstNonEmpty(state.LastCodeParams["id"], state.Profile.ID),
		"backup_token":      firstNonEmpty(state.LastCodeParams["backup_token"], state.Profile.BackupToken),
		"code":              code,
		"authkey":           firstNonEmpty(state.LastCodeParams["authkey"], state.AuthKey),
		"e_ident":           firstNonEmpty(state.LastCodeParams["e_ident"], state.KeyBundle.IdentityPublic),
		"e_keytype":         firstNonEmpty(state.LastCodeParams["e_keytype"], state.KeyBundle.KeyType),
		"e_regid":           firstNonEmpty(state.LastCodeParams["e_regid"], state.KeyBundle.RegID),
		"e_skey_id":         firstNonEmpty(state.LastCodeParams["e_skey_id"], state.KeyBundle.SignedKeyID),
		"e_skey_val":        firstNonEmpty(state.LastCodeParams["e_skey_val"], state.KeyBundle.SignedKeyValue),
		"e_skey_sig":        firstNonEmpty(state.LastCodeParams["e_skey_sig"], state.KeyBundle.SignedKeySig),
	}
	if token := e.registrationToken(phone, state); token != "" {
		params["token"] = token
	}
	raw := map[string]struct{}{"id": {}, "backup_token": {}}
	for key, value := range registerVerificationMap(state, params["method"]) {
		params[key] = pctBytes([]byte(value))
		raw[key] = struct{}{}
	}
	return params, raw
}

func (e *NativeEngine) loadState(ctx context.Context, workspaceID string, clientProfileID string) (nativeState, error) {
	state, err := e.stateStore.GetNativeState(ctx, workspaceID, clientProfileID)
	if err != nil {
		return nativeState{}, NewError(waappv1.WaErrorCode_WA_ERROR_CODE_PROFILE_NOT_FOUND, "native client profile state not found", false)
	}
	return state, nil
}

func (e *NativeEngine) newState(phone *waappv1.PhoneTarget) (nativeState, error) {
	return newNativeState(phone, defaultWAAppVersion)
}

func (e *NativeEngine) saveState(ctx context.Context, workspaceID string, clientProfileID string, state nativeState) error {
	return e.stateStore.SaveNativeState(ctx, workspaceID, clientProfileID, state)
}

func sanitizeResponse(data map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range data {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "auth") || strings.Contains(lower, "code") || strings.Contains(lower, "sig") {
			out[key] = "<redacted>"
			continue
		}
		out[key] = value
	}
	return out
}

func classifyHTTPError(data map[string]any, err error) error {
	status := responseStatus(data)
	switch status {
	case "no_routes":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_ROUTE_UNAVAILABLE, "verification route is unavailable", false)
	case "too_recent":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_RATE_LIMITED, "verification request is too recent", true)
	case "blocked", "rejected":
		return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, "request was rejected", false)
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, err.Error(), true)
}

func jsonString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func jsonNumber(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	default:
		return 0
	}
}

func extractCandidates(messageID string, decryptedID string, text string, includeValue bool, now time.Time, ids IDGenerator) []*waappv1.ExtractedCandidate {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	patterns := []struct {
		kind      waappv1.CandidateKind
		re        *regexp.Regexp
		normalize func(string) string
	}{
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_FLAG, re: regexp.MustCompile(`(?i)(flag|ctf)\{[^\s}]{1,120}\}`)},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{4,8}\b`), normalize: digitsOnly},
		{kind: waappv1.CandidateKind_CANDIDATE_KIND_OTP, re: regexp.MustCompile(`\b\d{3}[-\s]\d{3}\b`), normalize: digitsOnly},
	}
	out := []*waappv1.ExtractedCandidate{}
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		for _, match := range pattern.re.FindAllString(text, -1) {
			value := match
			if pattern.normalize != nil {
				value = pattern.normalize(match)
			}
			if value == "" {
				continue
			}
			key := pattern.kind.String() + ":" + value
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidateID := ids.NewID("wacand_")
			sensitive := &waappv1.SensitiveText{RedactedValue: redacted(value), SecretRef: "candidate:" + candidateID}
			if includeValue {
				sensitive.Value = value
			}
			out = append(out, &waappv1.ExtractedCandidate{CandidateId: candidateID, MessageId: messageID, DecryptedMessageId: decryptedID, Kind: pattern.kind, Text: sensitive, Confidence: 0.9, ExtractedAt: timestamppb.New(now)})
		}
	}
	return out
}
