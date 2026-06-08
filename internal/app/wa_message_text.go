package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	waMessageProtoMaxDepth  = 6
	waMessageProtoMaxFields = 128
	waDisplayTextMaxRunes   = 4096
)

type waProtoField struct {
	number protowire.Number
	kind   protowire.Type
	value  []byte
	varint uint64
}

type waMessageTextCandidate struct {
	value string
	score int
}

func nativeMessageDisplayText(raw []byte) (string, bool) {
	candidates := []waMessageTextCandidate{}
	collectWAMessageText(raw, nil, 0, &candidates)
	if len(candidates) == 0 {
		collectOffsetWAMessageText(raw, &candidates)
	}
	if len(candidates) == 0 {
		return "", false
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		if candidates[i].score == candidates[j].score {
			return utf8.RuneCountInString(candidates[i].value) > utf8.RuneCountInString(candidates[j].value)
		}
		return candidates[i].score > candidates[j].score
	})
	return candidates[0].value, true
}

func collectWAMessageText(raw []byte, path []protowire.Number, depth int, candidates *[]waMessageTextCandidate) {
	if depth > waMessageProtoMaxDepth || len(raw) == 0 {
		return
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return
	}
	for _, field := range fields {
		if field.kind != protowire.BytesType {
			continue
		}
		fieldPath := appendWAPath(path, field.number)
		if value, score, ok := waKnownTextField(fieldPath, field.value); ok {
			*candidates = append(*candidates, waMessageTextCandidate{value: value, score: score})
		}
		if value, score, ok := waCompositeTextField(fieldPath, field.value); ok {
			*candidates = append(*candidates, waMessageTextCandidate{value: value, score: score})
		}
		if value, score, ok := waMessagePlaceholder(fieldPath); ok {
			*candidates = append(*candidates, waMessageTextCandidate{value: value, score: score})
		}
		collectWAMessageText(field.value, fieldPath, depth+1, candidates)
	}
}

func collectOffsetWAMessageText(raw []byte, candidates *[]waMessageTextCandidate) {
	limit := len(raw)
	if limit > 128 {
		limit = 128
	}
	for offset := 1; offset < limit; offset++ {
		offsetCandidates := []waMessageTextCandidate{}
		collectWAMessageText(raw[offset:], nil, 0, &offsetCandidates)
		for _, candidate := range offsetCandidates {
			candidate.score -= 120 + offset
			if candidate.score >= 650 {
				*candidates = append(*candidates, candidate)
			}
		}
	}
}

func parseWAProtoFields(raw []byte) ([]waProtoField, bool) {
	return parseWAProtoFieldsWithLimit(raw, waMessageProtoMaxFields)
}

func parseWAProtoFieldsWithLimit(raw []byte, maxFields int) ([]waProtoField, bool) {
	if maxFields <= 0 {
		return nil, false
	}
	fields := []waProtoField{}
	for len(raw) > 0 {
		if len(fields) >= maxFields {
			return nil, false
		}
		number, kind, tagSize := protowire.ConsumeTag(raw)
		if tagSize < 0 || !number.IsValid() {
			return nil, false
		}
		valueBytes := raw[tagSize:]
		switch kind {
		case protowire.BytesType:
			value, size := protowire.ConsumeBytes(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, waProtoField{number: number, kind: kind, value: value})
			raw = valueBytes[size:]
		case protowire.VarintType:
			value, size := protowire.ConsumeVarint(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, waProtoField{number: number, kind: kind, varint: value})
			raw = valueBytes[size:]
		case protowire.Fixed32Type:
			_, size := protowire.ConsumeFixed32(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, waProtoField{number: number, kind: kind})
			raw = valueBytes[size:]
		case protowire.Fixed64Type:
			_, size := protowire.ConsumeFixed64(valueBytes)
			if size < 0 {
				return nil, false
			}
			fields = append(fields, waProtoField{number: number, kind: kind})
			raw = valueBytes[size:]
		default:
			return nil, false
		}
	}
	return fields, true
}

func waKnownTextField(path []protowire.Number, raw []byte) (string, int, bool) {
	text := waCleanDisplayText(raw)
	if text == "" {
		return "", 0, false
	}
	if jsonText := waJSONDisplayText(text); jsonText != "" {
		text = jsonText
	}
	paramText := waTemplateParamDisplayText(raw)
	normalized := normalizeWAMessagePath(path)
	switch {
	case sameWAPath(normalized, 1):
		return text, 1000, true
	case suffixWAPath(normalized, 6, 1):
		return text, 995, true
	case suffixWAPath(normalized, 6, 5), suffixWAPath(normalized, 6, 6):
		return text, 860, true
	case suffixWAPath(normalized, 3, 3):
		return withWAPrefix("图片", text), 940, true
	case suffixWAPath(normalized, 9, 7):
		return withWAPrefix("视频", text), 930, true
	case suffixWAPath(normalized, 66, 7):
		return withWAPrefix("圆形视频", text), 930, true
	case suffixWAPath(normalized, 7, 20):
		return withWAPrefix("文件", text), 925, true
	case suffixWAPath(normalized, 7, 3):
		return withWAPrefix("文件", text), 760, true
	case suffixWAPath(normalized, 7, 8):
		return withWAPrefix("文件", text), 720, true
	case suffixWAPath(normalized, 46, 2):
		return withWAPrefix("回应", text), 900, true
	case suffixWAPath(normalized, 38, 6), suffixWAPath(normalized, 38, 7):
		return withWAPrefix("订单", text), 860, true
	case suffixWAPath(normalized, 4, 1):
		return withWAPrefix("联系人", text), 850, true
	case suffixWAPath(normalized, 5, 3), suffixWAPath(normalized, 5, 4), suffixWAPath(normalized, 5, 11):
		return withWAPrefix("位置", text), 835, true
	case suffixWAPath(normalized, 18, 6):
		return withWAPrefix("实时位置", text), 830, true
	case suffixWAPath(normalized, 29, 2):
		return withWAPrefix("模板回复", text), 875, true
	case suffixWAPath(normalized, 14, 3), suffixWAPath(normalized, 14, 6, 1):
		if paramText != "" {
			return withWAPrefix("模板", paramText), 845, true
		}
	case suffixWAPath(normalized, 25, 4, 6), suffixWAPath(normalized, 25, 4, 2), suffixWAPath(normalized, 25, 4, 7), suffixWAPath(normalized, 25, 2, 6), suffixWAPath(normalized, 25, 2, 2), suffixWAPath(normalized, 25, 2, 7), suffixWAPath(normalized, 25, 1, 6), suffixWAPath(normalized, 25, 1, 7):
		return withWAPrefix("模板", text), 865, true
	case suffixWAPath(normalized, 36, 1), suffixWAPath(normalized, 36, 2):
		return withWAPrefix("列表", text), 850, true
	case suffixWAPath(normalized, 39, 1), suffixWAPath(normalized, 39, 5), suffixWAPath(normalized, 39, 3, 2):
		return withWAPrefix("列表回复", text), 855, true
	case suffixWAPath(normalized, 42, 1), suffixWAPath(normalized, 42, 6):
		return withWAPrefix("按钮", text), 850, true
	case suffixWAPath(normalized, 42, 9, 1, 1), suffixWAPath(normalized, 42, 9, 2, 1), suffixWAPath(normalized, 42, 9, 2, 2), suffixWAPath(normalized, 42, 9, 3, 1):
		return withWAPrefix("按钮", text), 845, true
	case suffixWAPath(normalized, 43, 2):
		return withWAPrefix("按钮回复", text), 880, true
	case suffixWAPath(normalized, 45, 2, 1), suffixWAPath(normalized, 45, 1, 1), suffixWAPath(normalized, 45, 1, 2), suffixWAPath(normalized, 45, 3, 1):
		return withWAPrefix("互动", text), 855, true
	case suffixWAPath(normalized, 45, 6, 1, 1), suffixWAPath(normalized, 45, 6, 1, 2), suffixWAPath(normalized, 45, 6, 2), suffixWAPath(normalized, 45, 8, 4):
		return withWAPrefix("互动", text), 845, true
	case suffixWAPath(normalized, 48, 1):
		return withWAPrefix("互动回复", text), 860, true
	case suffixWAPath(normalized, 48, 1, 1), suffixWAPath(normalized, 48, 2, 1), suffixWAPath(normalized, 48, 2, 2):
		return withWAPrefix("互动回复", text), 865, true
	case suffixWAPath(normalized, 49, 2), suffixWAPath(normalized, 60, 2), suffixWAPath(normalized, 64, 2), suffixWAPath(normalized, 93, 2), suffixWAPath(normalized, 111, 2), suffixWAPath(normalized, 119, 2):
		return withWAPrefix("投票", text), 860, true
	case suffixWAPath(normalized, 31, 2):
		return text, 760, true
	default:
		return "", 0, false
	}
	return "", 0, false
}

func waCompositeTextField(path []protowire.Number, raw []byte) (string, int, bool) {
	normalized := normalizeWAMessagePath(path)
	switch {
	case sameWAPath(normalized, 4):
		if text := waContactDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 5):
		if text := waLocationDisplayText("位置", raw); text != "" {
			return text, 885, true
		}
	case sameWAPath(normalized, 13):
		if text := waContactsArrayDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 6):
		if text := waExtendedTextDisplayText(raw); text != "" {
			return text, 1010, true
		}
	case sameWAPath(normalized, 18):
		if text := waLocationDisplayText("实时位置", raw); text != "" {
			return text, 875, true
		}
	case sameWAPath(normalized, 14):
		if text := waHighlyStructuredDisplayText(raw); text != "" {
			return text, 910, true
		}
	case sameWAPath(normalized, 12):
		if text := waProtocolMessageDisplayText(raw); text != "" {
			return text, 520, true
		}
	case sameWAPath(normalized, 25):
		if text := waTemplateDisplayText(raw); text != "" {
			return text, 915, true
		}
	case sameWAPath(normalized, 30):
		if text := waProductDisplayText(raw); text != "" {
			return text, 930, true
		}
	case sameWAPath(normalized, 36):
		if text := waListDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 38):
		if text := waOrderDisplayText(raw); text != "" {
			return text, 885, true
		}
	case sameWAPath(normalized, 39):
		if text := waListResponseDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 42):
		if text := waButtonsDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 45):
		if text := waInteractiveDisplayText(raw); text != "" {
			return text, 890, true
		}
	case sameWAPath(normalized, 48):
		if text := waInteractiveResponseDisplayText(raw); text != "" {
			return text, 880, true
		}
	case sameWAPath(normalized, 49), sameWAPath(normalized, 60), sameWAPath(normalized, 64), sameWAPath(normalized, 93), sameWAPath(normalized, 111), sameWAPath(normalized, 119):
		if text := waPollDisplayText(raw); text != "" {
			return text, 900, true
		}
	}
	return "", 0, false
}

func waProductDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 1, 3),
		waHumanStringAtPath(raw, 5),
		waHumanStringAtPath(raw, 1, 4),
		waProductPrice(raw),
		waHumanStringAtPath(raw, 6),
		waHumanStringAtPath(raw, 4, 2),
		waHumanStringAtPath(raw, 4, 3),
	)
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("商品", strings.Join(parts, "\n"))
}

func waExtendedTextDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 1),
		waHumanStringAtPath(raw, 6),
		waHumanStringAtPath(raw, 5),
		waStringAtPath(raw, 2),
	)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n")
}

func waHighlyStructuredDisplayText(raw []byte) string {
	params := waHighlyStructuredParams(raw)
	templateName := waHumanStringAtPath(raw, 2)
	if len(params) == 0 && templateName == "" {
		return ""
	}
	parts := params
	if len(parts) == 0 {
		parts = append(parts, templateName)
	}
	return withWAPrefix("模板", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waProtocolMessageDisplayText(raw []byte) string {
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return ""
	}
	for _, field := range fields {
		switch field.number {
		case 6:
			return "[系统] 历史同步"
		case 23:
			return "[系统] 联系人同步"
		}
	}
	return "[系统消息]"
}

func waTemplateDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 4, 6),
		waHumanStringAtPath(raw, 4, 2),
		waHumanStringAtPath(raw, 4, 7),
		waHumanStringAtPath(raw, 2, 6),
		waHumanStringAtPath(raw, 2, 2),
		waHumanStringAtPath(raw, 2, 7),
		waHumanStringAtPath(raw, 1, 6),
		waHumanStringAtPath(raw, 1, 7),
	)
	for _, path := range [][]protowire.Number{{1}, {2}, {4}} {
		if message := waBytesAtPath(raw, path...); len(message) > 0 {
			parts = append(parts, waHydratedTemplateParts(message)...)
		}
	}
	for _, path := range [][]protowire.Number{{1, 2}, {2, 2}, {4, 2}} {
		if message := waBytesAtPath(raw, path...); len(message) > 0 {
			parts = append(parts, strings.TrimPrefix(waHighlyStructuredDisplayText(message), "[模板] "))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("模板", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waHydratedTemplateParts(raw []byte) []string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 6), waHumanStringAtPath(raw, 2), waHumanStringAtPath(raw, 7))
	for _, button := range waBytesValuesAtPath(raw, 8) {
		if text := waTemplateButtonDisplayText(button); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	return uniqueNonEmptyStrings(parts...)
}

func waTemplateButtonDisplayText(raw []byte) string {
	return firstNonEmpty(
		waHumanStringAtPath(raw, 1, 1),
		waHumanStringAtPath(raw, 2, 1),
		waHumanStringAtPath(raw, 2, 2),
		waHumanStringAtPath(raw, 3, 1),
		waHumanStringAtPath(raw, 3, 2),
	)
}

func waContactDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waVCardPhone(waStringAtPath(raw, 16)))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("联系人", strings.Join(parts, "\n"))
}

func waContactsArrayDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1))
	for _, contact := range waBytesValuesAtPath(raw, 2) {
		if text := strings.TrimPrefix(waContactDisplayText(contact), "[联系人] "); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("联系人", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waLocationDisplayText(label string, raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 3),
		waHumanStringAtPath(raw, 4),
		waHumanStringAtPath(raw, 11),
		waHumanStringAtPath(raw, 5),
		waHumanStringAtPath(raw, 6),
	)
	if len(parts) == 0 {
		return "[" + label + "]"
	}
	return withWAPrefix(label, strings.Join(parts, "\n"))
}

func waListDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 2), waHumanStringAtPath(raw, 3), waHumanStringAtPath(raw, 7))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("列表", strings.Join(parts, "\n"))
}

func waListResponseDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 5), waHumanStringAtPath(raw, 3, 2), waHumanStringAtPath(raw, 3, 3), waHumanStringAtPath(raw, 3, 1))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("列表回复", strings.Join(parts, "\n"))
}

func waButtonsDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1), waHumanStringAtPath(raw, 6), waHumanStringAtPath(raw, 7))
	for _, button := range waBytesValuesAtPath(raw, 9) {
		if text := waButtonDisplayText(button); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("按钮", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waButtonDisplayText(raw []byte) string {
	return firstNonEmpty(
		waHumanStringAtPath(raw, 1, 1),
		waHumanStringAtPath(raw, 2, 1),
		waHumanStringAtPath(raw, 2, 2),
		waHumanStringAtPath(raw, 3, 1),
		waHumanStringAtPath(raw, 3, 2),
	)
}

func waInteractiveDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 2, 1),
		waHumanStringAtPath(raw, 1, 1),
		waHumanStringAtPath(raw, 1, 2),
		waHumanStringAtPath(raw, 3, 1),
		waHumanStringAtPath(raw, 8, 4),
		waHumanStringAtPath(raw, 8, 2),
	)
	parts = append(parts, waNativeFlowParts(raw)...)
	for _, card := range waBytesValuesAtPath(raw, 7, 1) {
		if text := strings.TrimPrefix(waInteractiveDisplayText(card), "[互动] "); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("互动", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waInteractiveResponseDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 1, 1), waHumanStringAtPath(raw, 2, 1), waHumanStringAtPath(raw, 2, 2))
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("互动回复", strings.Join(parts, "\n"))
}

func waNativeFlowParts(raw []byte) []string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 6, 2))
	for _, button := range waBytesValuesAtPath(raw, 6, 1) {
		parts = append(parts, uniqueNonEmptyStrings(waHumanStringAtPath(button, 1), waHumanStringAtPath(button, 2))...)
	}
	return uniqueNonEmptyStrings(parts...)
}

func waPollDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(waHumanStringAtPath(raw, 2))
	for _, option := range waBytesValuesAtPath(raw, 3) {
		if text := waHumanStringAtPath(option, 1); text != "" {
			parts = append(parts, "• "+text)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("投票", strings.Join(uniqueNonEmptyStrings(parts...), "\n"))
}

func waOrderDisplayText(raw []byte) string {
	parts := uniqueNonEmptyStrings(
		waHumanStringAtPath(raw, 7),
		waHumanStringAtPath(raw, 6),
		waOrderTotal(raw),
	)
	if len(parts) == 0 {
		return ""
	}
	return withWAPrefix("订单", strings.Join(parts, "\n"))
}

func waProductPrice(raw []byte) string {
	currency := waHumanStringAtPath(raw, 1, 5)
	amount, ok := waVarintAtPath(raw, 1, 12)
	if !ok || amount == 0 {
		amount, ok = waVarintAtPath(raw, 1, 6)
	}
	if !ok || amount == 0 {
		return ""
	}
	return formatWAAmount(currency, amount)
}

func waOrderTotal(raw []byte) string {
	currency := waHumanStringAtPath(raw, 11)
	amount, ok := waVarintAtPath(raw, 10)
	if !ok || amount == 0 {
		return ""
	}
	return formatWAAmount(currency, amount)
}

func formatWAAmount(currency string, amount1000 uint64) string {
	major := amount1000 / 1000
	minor := amount1000 % 1000
	if currency == "" {
		if minor == 0 {
			return strconv.FormatUint(major, 10)
		}
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%d.%03d", major, minor), "0"), ".")
	}
	if minor == 0 {
		return currency + " " + strconv.FormatUint(major, 10)
	}
	return currency + " " + strings.TrimRight(strings.TrimRight(fmt.Sprintf("%d.%03d", major, minor), "0"), ".")
}

func waHumanStringAtPath(raw []byte, path ...protowire.Number) string {
	return waHumanDisplayText(waStringAtPath(raw, path...))
}

func waStringAtPath(raw []byte, path ...protowire.Number) string {
	if len(path) == 0 {
		return waCleanDisplayText(raw)
	}
	value := waBytesAtPath(raw, path...)
	if len(value) == 0 {
		return ""
	}
	return waStringAtPath(value)
}

func waBytesAtPath(raw []byte, path ...protowire.Number) []byte {
	if len(path) == 0 {
		return raw
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return nil
	}
	for _, field := range fields {
		if field.kind == protowire.BytesType && field.number == path[0] {
			return waBytesAtPath(field.value, path[1:]...)
		}
	}
	return nil
}

func waBytesValuesAtPath(raw []byte, path ...protowire.Number) [][]byte {
	if len(path) == 0 {
		return nil
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return nil
	}
	values := [][]byte{}
	for _, field := range fields {
		if field.kind != protowire.BytesType || field.number != path[0] {
			continue
		}
		if len(path) == 1 {
			values = append(values, field.value)
			continue
		}
		values = append(values, waBytesValuesAtPath(field.value, path[1:]...)...)
	}
	return values
}

func waStringValuesAtPath(raw []byte, path ...protowire.Number) []string {
	values := []string{}
	for _, rawValue := range waBytesValuesAtPath(raw, path...) {
		if text := waTemplateParamDisplayText(rawValue); text != "" {
			values = append(values, text)
		}
	}
	return values
}

func waHighlyStructuredParams(raw []byte) []string {
	params := waStringValuesAtPath(raw, 3)
	for _, item := range waBytesValuesAtPath(raw, 6) {
		if text := waTemplateParamDisplayText(waBytesAtPath(item, 1)); text != "" {
			params = append(params, text)
		}
	}
	return uniqueNonEmptyStrings(params...)
}

func waTemplateParamDisplayText(raw []byte) string {
	text := waCleanDisplayText(raw)
	if text == "" {
		return ""
	}
	if value := waJSONDisplayText(text); value != "" {
		return value
	}
	return waHumanDisplayText(text)
}

func waJSONDisplayText(text string) string {
	value := strings.TrimSpace(text)
	if !strings.HasPrefix(value, "{") || !strings.HasSuffix(value, "}") {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return ""
	}
	parts := []string{}
	for _, key := range []string{"display_text", "display_title", "title", "text", "body", "description", "footer_text", "button_text", "cta_button_text", "url_text", "url", "name", "formatted_amount", "formatted_amount_with_currency"} {
		if part := waJSONTextValue(payload[key]); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		parts = append(parts, waJSONNestedTextValues(payload, 0)...)
	}
	return strings.Join(uniqueNonEmptyStrings(parts...), "\n")
}

func waJSONTextValue(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return waHumanDisplayText(text)
}

func waJSONNestedTextValues(value any, depth int) []string {
	if depth > 4 {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		parts := []string{}
		for _, key := range []string{"display_text", "display_title", "title", "text", "body", "description", "footer_text", "button_text", "cta_button_text", "url_text", "url", "name"} {
			if part := waJSONTextValue(typed[key]); part != "" {
				parts = append(parts, part)
			}
		}
		if len(parts) > 0 {
			return uniqueNonEmptyStrings(parts...)
		}
		for _, child := range typed {
			parts = append(parts, waJSONNestedTextValues(child, depth+1)...)
		}
		return uniqueNonEmptyStrings(parts...)
	case []any:
		parts := []string{}
		for _, child := range typed {
			parts = append(parts, waJSONNestedTextValues(child, depth+1)...)
		}
		return uniqueNonEmptyStrings(parts...)
	default:
		return nil
	}
}

func waVarintAtPath(raw []byte, path ...protowire.Number) (uint64, bool) {
	if len(path) == 0 {
		return 0, false
	}
	fields, ok := parseWAProtoFields(raw)
	if !ok {
		return 0, false
	}
	for _, field := range fields {
		if field.number != path[0] {
			continue
		}
		if len(path) == 1 && field.kind == protowire.VarintType {
			return field.varint, true
		}
		if len(path) > 1 && field.kind == protowire.BytesType {
			return waVarintAtPath(field.value, path[1:]...)
		}
	}
	return 0, false
}

func waMessagePlaceholder(path []protowire.Number) (string, int, bool) {
	normalized := normalizeWAMessagePath(path)
	if len(normalized) != 1 {
		return "", 0, false
	}
	switch normalized[0] {
	case 3:
		return "[图片]", 520, true
	case 4:
		return "[联系人]", 510, true
	case 5:
		return "[位置]", 510, true
	case 12:
		return "[系统消息]", 500, true
	case 7:
		return "[文件]", 520, true
	case 8:
		return "[语音]", 520, true
	case 9:
		return "[视频]", 520, true
	case 13:
		return "[联系人]", 510, true
	case 14, 25:
		return "[模板]", 500, true
	case 18:
		return "[实时位置]", 500, true
	case 26:
		return "[贴纸]", 500, true
	case 28:
		return "[群邀请]", 500, true
	case 30:
		return "[商品]", 500, true
	case 36, 39:
		return "[列表]", 500, true
	case 38:
		return "[订单]", 500, true
	case 42, 43:
		return "[按钮]", 500, true
	case 45, 48:
		return "[互动]", 500, true
	case 46:
		return "[回应]", 500, true
	case 49, 60, 64, 93, 111, 119:
		return "[投票]", 500, true
	case 66:
		return "[圆形视频]", 500, true
	default:
		return "", 0, false
	}
}

func waCleanDisplayText(raw []byte) string {
	if len(raw) == 0 || !utf8.Valid(raw) {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.ContainsRune(text, 0) || !readableText(text) {
		return ""
	}
	return trimWARunes(text, waDisplayTextMaxRunes)
}

func waHumanDisplayText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if jsonText := waJSONDisplayText(text); jsonText != "" {
		return jsonText
	}
	if isLikelyMachineToken(text) || isLikelyMachineText(text) {
		return ""
	}
	return trimWARunes(text, waDisplayTextMaxRunes)
}

func isLikelyMachineText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.Contains(lower, "://") {
		return true
	}
	if strings.Contains(lower, "mmg.whatsapp.net/") || strings.Contains(lower, "whatsapp.net/") && strings.Count(lower, " ") == 0 {
		return true
	}
	if utf8.RuneCountInString(text) > 240 && strings.Count(text, " ") <= 1 {
		return true
	}
	return false
}

func waVCardPhone(vcard string) string {
	for _, line := range strings.Split(vcard, "\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if !strings.HasPrefix(upper, "TEL") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeWAMessagePath(path []protowire.Number) []protowire.Number {
	out := append([]protowire.Number(nil), path...)
	for len(out) >= 2 && isWAWrapperInnerField(out[0], out[1]) {
		out = out[2:]
	}
	return out
}

func isWAWrapperInnerField(field protowire.Number, inner protowire.Number) bool {
	switch field {
	case 31:
		return inner == 2
	case 37, 40, 53, 55, 58, 59, 62, 67, 83, 87, 91, 92, 96, 103, 104, 106, 116, 117, 120:
		return inner == 1
	default:
		return false
	}
}

func appendWAPath(path []protowire.Number, field protowire.Number) []protowire.Number {
	out := make([]protowire.Number, 0, len(path)+1)
	out = append(out, path...)
	out = append(out, field)
	return out
}

func sameWAPath(path []protowire.Number, fields ...protowire.Number) bool {
	if len(path) != len(fields) {
		return false
	}
	for i := range fields {
		if path[i] != fields[i] {
			return false
		}
	}
	return true
}

func suffixWAPath(path []protowire.Number, fields ...protowire.Number) bool {
	if len(path) < len(fields) {
		return false
	}
	start := len(path) - len(fields)
	for i := range fields {
		if path[start+i] != fields[i] {
			return false
		}
	}
	return true
}

func withWAPrefix(label string, text string) string {
	if text == "" {
		return "[" + label + "]"
	}
	return "[" + label + "] " + text
}

func trimWARunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	out := make([]rune, 0, limit)
	for _, ch := range text {
		if len(out) >= limit {
			break
		}
		out = append(out, ch)
	}
	return string(out) + "..."
}

func nativePrintableDisplayText(raw []byte) string {
	segments := printableSegments(raw)
	if len(segments) == 0 {
		return ""
	}
	best := ""
	bestScore := -1 << 30
	for _, segment := range segments {
		text := strings.TrimSpace(segment)
		if text == "" {
			continue
		}
		score := waPrintableSegmentScore(text)
		if score > bestScore {
			best = text
			bestScore = score
		}
	}
	if best != "" && (len(segments) == 1 || bestScore >= 80) {
		return trimWARunes(best, waDisplayTextMaxRunes)
	}
	if best != "" && bestScore >= 0 {
		return trimWARunes(best, waDisplayTextMaxRunes)
	}
	return ""
}

func waPrintableSegmentScore(text string) int {
	runes := utf8.RuneCountInString(text)
	score := runes
	if nativeSensitiveDigitsPattern.MatchString(text) {
		score += 600
	}
	if strings.Contains(strings.ToLower(text), "flag{") || strings.Contains(strings.ToLower(text), "ctf{") {
		score += 600
	}
	if strings.ContainsAny(text, " .,:;!?，。！？：；") {
		score += 80
	}
	if isLikelyMachineToken(text) {
		score -= 600
	}
	return score
}

func isLikelyMachineToken(text string) bool {
	if utf8.RuneCountInString(text) < 16 {
		return false
	}
	letters := 0
	digits := 0
	other := 0
	for _, ch := range text {
		switch {
		case ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z':
			letters++
		case ch >= '0' && ch <= '9':
			digits++
		case ch == '+' || ch == '/' || ch == '=' || ch == '-' || ch == '_':
			other++
		}
	}
	return (letters+digits+other)*100/utf8.RuneCountInString(text) > 95 && strings.Count(text, " ") == 0
}

func uniqueNonEmptyStrings(values ...string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
