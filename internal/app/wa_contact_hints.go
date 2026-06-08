package app

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	waContactProtoMaxDepth       = 8
	waContactProtoMaxFields      = 8192
	waContactDecodedPayloadLimit = 4 << 20
)

type waContactHint struct {
	LIDJID      string `json:"lid_jid,omitempty"`
	PNJID       string `json:"pn_jid,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	WAName      string `json:"wa_name,omitempty"`
	Username    string `json:"username,omitempty"`
}

func nativeContactHints(raw []byte) []waContactHint {
	if len(raw) == 0 {
		return nil
	}
	hints := waKnownContactRecordHints(raw)
	collectWAContactHints(raw, nil, 0, &hints)
	return dedupeWAContactHints(hints)
}

func contactHintsFromNativePayloadMetadata(payload nativeMessagePayload) []waContactHint {
	hints := append([]waContactHint{}, payload.ContactHints...)
	appendPayloadHint := func(lidRef string, pnRef string) {
		hint := waContactHint{
			LIDJID:      normalizeWAJID(lidRef),
			PNJID:       normalizeWAJID(pnRef),
			DisplayName: waContactName(payload.NotifyName),
			Username:    waContactName(payload.ParticipantUsername),
		}
		if hint.valid() {
			hints = append(hints, hint)
		}
	}
	appendPayloadHint(payload.Sender, payload.SenderPN)
	appendPayloadHint(payload.Contact, payload.ContactPN)
	appendPayloadHint(payload.Sender, payload.ContactPN)
	appendPayloadHint(payload.Contact, payload.SenderPN)
	return dedupeWAContactHints(hints)
}

func contactHintsFromChatdNode(node chatdNode) []waContactHint {
	hints := []waContactHint{}
	var walk func(chatdNode)
	walk = func(current chatdNode) {
		hints = append(hints, contactHintsFromChatdAttrs(current.Attrs)...)
		for _, child := range chatdChildren(current) {
			walk(child)
		}
	}
	walk(node)
	return dedupeWAContactHints(hints)
}

func contactHintsFromChatdAttrs(attrs map[string]string) []waContactHint {
	if len(attrs) == 0 {
		return nil
	}
	displayName := waContactName(firstChatdAttr(attrs, "notify", "notify_name", "display_name"))
	username := waContactUsername(firstChatdAttr(attrs, "participant_username", "peer_recipient_username", "author_username", "username"))
	peerLIDKeys := []string{"peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid"}
	peerPNKeys := []string{"peer_recipient_pn", "peer_recipient_pn_jid", "recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid"}
	actorLIDKeys := []string{"author", "author_lid", "creator_lid"}
	actorPNKeys := []string{"author_pn", "author_pn_jid", "creator_pn", "creator_pn_jid", "pn_jid"}
	contactLIDKeys := []string{"contact_lid"}
	contactPNKeys := []string{"contact_pn", "contact_pn_jid", "pn_jid", "new_jid"}
	fallbackLIDKeys := []string{
		"sender_lid", "participant_lid", "peer_recipient_lid", "recipient_latest_lid", "recipient_lid", "peer_lid",
		"contact_lid", "author_lid", "creator_lid", "caller_lid", "invitee_lid", "lid", "jid", "participant", "author", "from",
	}
	fallbackPNKeys := []string{
		"sender_pn", "sender_pn_jid", "participant_pn", "participant_pn_jid", "peer_recipient_pn", "peer_recipient_pn_jid",
		"recipient_pn", "recipient_pn_jid", "peer_pn", "peer_pn_jid", "contact_pn", "contact_pn_jid", "author_pn", "author_pn_jid",
		"creator_pn", "creator_pn_jid", "caller_pn", "caller_pn_jid", "invitee_pn", "invitee_pn_jid", "from_pn", "from_pn_jid",
		"pn_jid", "new_jid", "jid", "participant", "author", "from",
	}
	hints := []waContactHint{}
	appendChatdAttrHints(&hints, attrs, displayName, username, []string{"sender_lid"}, []string{"sender_pn", "sender_pn_jid"})
	appendChatdAttrHints(&hints, attrs, displayName, username, []string{"participant_lid", "participant", "jid"}, []string{"participant_pn", "participant_pn_jid", "pn_jid", "jid"})
	appendChatdAttrHints(&hints, attrs, displayName, waContactUsername(firstChatdAttr(attrs, "peer_recipient_username", "username")), peerLIDKeys, peerPNKeys)
	appendChatdAttrHints(&hints, attrs, displayName, waContactUsername(firstChatdAttr(attrs, "author_username", "username")), actorLIDKeys, actorPNKeys)
	appendChatdAttrHints(&hints, attrs, displayName, username, []string{"from"}, []string{"from_pn", "from_pn_jid", "pn_jid", "new_jid"})
	appendChatdAttrHints(&hints, attrs, waContactName(firstNonEmpty(firstChatdAttr(attrs, "contact_push_name"), displayName)), waContactUsername(firstChatdAttr(attrs, "contact_username", "username")), contactLIDKeys, contactPNKeys)
	appendChatdAttrHints(&hints, attrs, displayName, username, []string{"caller_lid"}, []string{"caller_pn", "caller_pn_jid"})
	appendChatdAttrHints(&hints, attrs, displayName, username, []string{"invitee_lid"}, []string{"invitee_pn", "invitee_pn_jid"})
	if len(hints) == 0 {
		lids := chatdAttrJIDs(attrs, "@lid", fallbackLIDKeys...)
		pns := chatdAttrJIDs(attrs, "@s.whatsapp.net", fallbackPNKeys...)
		if len(lids) == 1 && len(pns) <= 1 {
			hint := waContactHint{LIDJID: lids[0], DisplayName: displayName, Username: username}
			if len(pns) == 1 {
				hint.PNJID = pns[0]
			}
			if hint.valid() {
				hints = append(hints, hint)
			}
		}
	}
	return dedupeWAContactHints(hints)
}

func firstChatdAttr(attrs map[string]string, keys ...string) string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, attrs[key])
	}
	return firstNonEmpty(values...)
}

func appendChatdAttrHints(hints *[]waContactHint, attrs map[string]string, displayName string, username string, lidKeys []string, pnKeys []string) {
	lids := chatdAttrJIDs(attrs, "@lid", lidKeys...)
	if len(lids) == 0 {
		return
	}
	pns := chatdAttrJIDs(attrs, "@s.whatsapp.net", pnKeys...)
	if len(pns) == 0 {
		if len(lids) != 1 {
			return
		}
		hint := waContactHint{LIDJID: lids[0], DisplayName: displayName, Username: username}
		if hint.valid() {
			*hints = append(*hints, hint)
		}
		return
	}
	for _, lid := range lids {
		for _, pn := range pns {
			hint := waContactHint{LIDJID: lid, PNJID: pn, DisplayName: displayName, Username: username}
			if hint.valid() {
				*hints = append(*hints, hint)
			}
		}
	}
}

func chatdAttrJIDs(attrs map[string]string, suffix string, keys ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, key := range keys {
		jid := normalizeWAJID(attrs[key])
		if !strings.HasSuffix(jid, suffix) {
			continue
		}
		if _, ok := seen[jid]; ok {
			continue
		}
		seen[jid] = struct{}{}
		out = append(out, jid)
	}
	return out
}

func collectWAContactHints(raw []byte, path []protowire.Number, depth int, hints *[]waContactHint) {
	if depth > waContactProtoMaxDepth || len(raw) == 0 {
		return
	}
	fields, ok := parseWAProtoFieldsWithLimit(raw, waContactProtoMaxFields)
	if !ok {
		return
	}
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		fieldPath := appendWAPath(path, field.number)
		normalized := normalizeWAMessagePath(fieldPath)
		switch {
		case sameWAPath(normalized, 12, 23, 1):
			*hints = append(*hints, waLIDMigrationMappingPayloadHints(field.value)...)
		case sameWAPath(normalized, 12, 6, 11):
			*hints = append(*hints, waHistorySyncContactHints(field.value)...)
		}
		*hints = append(*hints, waKnownContactRecordHints(field.value)...)
		collectWAContactHints(field.value, fieldPath, depth+1, hints)
	}
}

func waKnownContactRecordHints(raw []byte) []waContactHint {
	hints := []waContactHint{}
	hints = append(hints, waSyncdIndexedContactHints(raw)...)
	if hint := waAppStateContactActionHint(raw); hint.valid() {
		hints = append(hints, hint)
	}
	if hint := waLIDContactActionHint(raw); hint.valid() {
		hints = append(hints, hint)
	}
	return dedupeWAContactHints(hints)
}

func waSyncdIndexedContactHints(raw []byte) []waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return nil
	}
	var indexRaw []byte
	var valueRaw []byte
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			indexRaw = field.value
		case 2:
			valueRaw = field.value
		}
	}
	index := waSyncdIndexValues(indexRaw)
	if len(index) == 0 || len(valueRaw) == 0 {
		return nil
	}
	return waAppStateActionHintsForIndex(index, valueRaw)
}

func waSyncdIndexValues(raw []byte) []string {
	if len(raw) == 0 || len(raw) > 16<<10 || !utf8.Valid(raw) {
		return nil
	}
	text := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(text, "[") || !strings.HasSuffix(text, "]") {
		return nil
	}
	var values []string
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil
	}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func waAppStateActionHintsForIndex(index []string, raw []byte) []waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 256)
	if !ok {
		return nil
	}
	indexLID := waSyncdIndexJID(index, "@lid")
	indexPN := waSyncdIndexJID(index, "@s.whatsapp.net")
	hints := []waContactHint{}
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 3:
			if hint := waIndexedAppStateContactActionHint(field.value, indexLID, indexPN); hint.valid() {
				hints = append(hints, hint)
			}
		case 37:
			if hint := waIndexedPNForLIDChatActionHint(field.value, indexLID); hint.valid() {
				hints = append(hints, hint)
			}
		case 61:
			if hint := waIndexedLIDContactActionHint(field.value, indexLID, indexPN); hint.valid() {
				hints = append(hints, hint)
			}
		case 79:
			if hint := waIndexedOutContactActionHint(field.value, indexLID, indexPN); hint.valid() {
				hints = append(hints, hint)
			}
		}
	}
	return dedupeWAContactHints(hints)
}

func waSyncdIndexJID(index []string, suffix string) string {
	for i := 1; i < len(index); i++ {
		jid := normalizeWAJID(index[i])
		if strings.HasSuffix(jid, suffix) {
			return jid
		}
	}
	return ""
}

func waIndexedAppStateContactActionHint(raw []byte, indexLID string, indexPN string) waContactHint {
	hint := waAppStateContactActionHint(raw)
	hint.LIDJID = firstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = firstNonEmpty(hint.PNJID, indexPN)
	return hint.normalized()
}

func waIndexedPNForLIDChatActionHint(raw []byte, indexLID string) waContactHint {
	hint := waPNForLIDChatActionHint(raw)
	hint.LIDJID = firstNonEmpty(hint.LIDJID, indexLID)
	return hint.normalized()
}

func waIndexedLIDContactActionHint(raw []byte, indexLID string, indexPN string) waContactHint {
	hint := waLIDContactActionHint(raw)
	hint.LIDJID = firstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = firstNonEmpty(hint.PNJID, indexPN)
	return hint.normalized()
}

func waIndexedOutContactActionHint(raw []byte, indexLID string, indexPN string) waContactHint {
	hint := waOutContactActionHint(raw)
	hint.LIDJID = firstNonEmpty(hint.LIDJID, indexLID)
	hint.PNJID = firstNonEmpty(hint.PNJID, indexPN)
	return hint.normalized()
}

func waAppStateContactActionHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 32)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			hint.DisplayName = waContactNameString(field.value)
		case 2:
			hint.WAName = waContactNameString(field.value)
		case 3:
			hint.LIDJID = normalizeWAJID(waProtoPlainString(field.value))
		case 5:
			hint.PNJID = normalizeWAJID(waProtoPlainString(field.value))
		case 6:
			hint.Username = waContactNameString(field.value)
		}
	}
	return hint.normalized()
}

func waLIDContactActionHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			hint.DisplayName = waContactNameString(field.value)
		case 2:
			hint.WAName = waContactNameString(field.value)
		case 3:
			hint.Username = waContactNameString(field.value)
		}
	}
	return hint.normalized()
}

func waOutContactActionHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			hint.DisplayName = waContactNameString(field.value)
		case 2:
			hint.WAName = waContactNameString(field.value)
		}
	}
	return hint.normalized()
}

func waPNForLIDChatActionHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 8)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		if field.number == 1 {
			hint.PNJID = normalizeWAJID(waProtoPlainString(field.value))
		}
	}
	return hint.normalized()
}

func waLIDMigrationMappingPayloadHints(raw []byte) []waContactHint {
	payloads := [][]byte{raw}
	if decoded, ok := waGunzipPayload(raw); ok {
		payloads = append([][]byte{decoded}, payloads...)
	}
	hints := []waContactHint{}
	for _, payload := range payloads {
		fields, ok := parseWAProtoFieldsWithLimit(payload, waContactProtoMaxFields)
		if !ok {
			continue
		}
		for _, field := range fields {
			if field.kind != protowire.BytesType || field.number != 1 {
				continue
			}
			hints = append(hints, waNumericLIDMappingRecordHints(field.value)...)
		}
		if len(hints) > 0 {
			break
		}
	}
	return dedupeWAContactHints(hints)
}

func waNumericLIDMappingRecordHints(raw []byte) []waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return nil
	}
	var pn uint64
	lids := []uint64{}
	for _, field := range fields {
		if field.kind != protowire.VarintType {
			continue
		}
		switch field.number {
		case 1:
			pn = field.varint
		case 2, 3:
			if field.varint > 0 {
				lids = append(lids, field.varint)
			}
		}
	}
	if pn == 0 || len(lids) == 0 {
		return nil
	}
	pnJID := numericWAJID(pn, "s.whatsapp.net")
	if pnJID == "" {
		return nil
	}
	hints := []waContactHint{}
	for _, lid := range lids {
		if lidJID := numericWAJID(lid, "lid"); lidJID != "" {
			hints = append(hints, waContactHint{LIDJID: lidJID, PNJID: pnJID})
		}
	}
	return dedupeWAContactHints(hints)
}

func waHistorySyncContactHints(raw []byte) []waContactHint {
	payloads := [][]byte{raw}
	if decoded, ok := waGunzipPayload(raw); ok {
		payloads = append([][]byte{decoded}, payloads...)
	}
	hints := []waContactHint{}
	for _, payload := range payloads {
		fields, ok := parseWAProtoFieldsWithLimit(payload, waContactProtoMaxFields)
		if !ok {
			continue
		}
		for _, field := range fields {
			if field.kind != protowire.BytesType {
				continue
			}
			switch field.number {
			case 15:
				if hint := waStringLIDMappingRecordHint(field.value); hint.valid() {
					hints = append(hints, hint)
				}
			case 20:
				if hint := waInlineContactRecordHint(field.value); hint.valid() {
					hints = append(hints, hint)
				}
			}
		}
		if len(hints) > 0 {
			break
		}
	}
	return dedupeWAContactHints(hints)
}

func waStringLIDMappingRecordHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			hint.PNJID = normalizeWAJID(waProtoPlainString(field.value))
		case 2:
			hint.LIDJID = normalizeWAJID(waProtoPlainString(field.value))
		}
	}
	return hint.normalized()
}

func waInlineContactRecordHint(raw []byte) waContactHint {
	fields, ok := parseWAProtoFieldsWithLimit(raw, 16)
	if !ok {
		return waContactHint{}
	}
	var hint waContactHint
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		switch field.number {
		case 1:
			hint.PNJID = normalizeWAJID(waProtoPlainString(field.value))
		case 2:
			hint.LIDJID = normalizeWAJID(waProtoPlainString(field.value))
		case 3:
			hint.DisplayName = waContactNameString(field.value)
		case 4:
			hint.WAName = waContactNameString(field.value)
		case 5:
			hint.Username = waContactNameString(field.value)
		}
	}
	return hint.normalized()
}

func (h waContactHint) valid() bool {
	h = h.normalized()
	return h.LIDJID != "" && (h.PNJID != "" || h.DisplayName != "" || h.WAName != "" || h.Username != "")
}

func (h waContactHint) normalized() waContactHint {
	h.LIDJID = normalizeWAJID(h.LIDJID)
	h.PNJID = normalizeWAJID(h.PNJID)
	h.DisplayName = waContactName(h.DisplayName)
	h.WAName = waContactName(h.WAName)
	h.Username = waContactName(h.Username)
	if h.LIDJID != "" && !strings.HasSuffix(h.LIDJID, "@lid") {
		h.LIDJID = ""
	}
	if h.PNJID != "" && !strings.HasSuffix(h.PNJID, "@s.whatsapp.net") {
		h.PNJID = ""
	}
	return h
}

func dedupeWAContactHints(hints []waContactHint) []waContactHint {
	if len(hints) == 0 {
		return nil
	}
	merged := map[string]waContactHint{}
	order := []string{}
	for _, hint := range hints {
		hint = hint.normalized()
		if !hint.valid() {
			continue
		}
		key := hint.LIDJID + "\x00" + hint.PNJID
		current, exists := merged[key]
		if !exists {
			order = append(order, key)
		}
		current.LIDJID = hint.LIDJID
		current.PNJID = hint.PNJID
		current.DisplayName = firstNonEmpty(current.DisplayName, hint.DisplayName)
		current.WAName = firstNonEmpty(current.WAName, hint.WAName)
		current.Username = firstNonEmpty(current.Username, hint.Username)
		merged[key] = current
	}
	out := make([]waContactHint, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}

func numericWAJID(value uint64, domain string) string {
	if value == 0 {
		return ""
	}
	local := strconv.FormatUint(value, 10)
	if len(local) < 5 || len(local) > 24 {
		return ""
	}
	return local + "@" + domain
}

func waGunzipPayload(raw []byte) ([]byte, bool) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return nil, false
	}
	reader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(io.LimitReader(reader, waContactDecodedPayloadLimit+1))
	if err != nil || len(data) == 0 || len(data) > waContactDecodedPayloadLimit {
		return nil, false
	}
	return data, true
}

func waProtoPlainString(raw []byte) string {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.ContainsRune(text, 0) {
		return ""
	}
	return text
}

func waContactNameString(raw []byte) string {
	return waContactName(waProtoPlainString(raw))
}

func waContactName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return ""
	}
	return trimWARunes(value, 120)
}
