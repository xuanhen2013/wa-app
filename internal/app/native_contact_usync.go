package app

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

const (
	defaultContactUsyncTimeout = 32 * time.Second
	maxContactUsyncBatchSize   = 50
)

func (e *NativeEngine) ResolveContacts(ctx context.Context, input EngineContactResolveInput) EngineContactResolveResult {
	jids := normalizeContactUsyncJIDs(input.JIDs)
	if len(jids) == 0 {
		return EngineContactResolveResult{}
	}
	state, err := e.loadState(ctx, input.ClientProfileID)
	if err != nil {
		return EngineContactResolveResult{Queried: len(jids), Err: err}
	}
	state.ensureMaps()
	if state.ChatStatic.Private == "" || state.ChatStatic.Public == "" {
		state.ChatStatic = ensureChatStatic(state.ChatStatic)
		if err := e.saveState(ctx, input.ClientProfileID, state); err != nil {
			return EngineContactResolveResult{Queried: len(jids), Err: err}
		}
	}
	allContacts := contactsFromNativeStateMessagePayloads(input.WAAccountID, state, jids, e.clock.Now())
	jids = unresolvedContactResolveJIDs(jids, allContacts)
	if len(jids) == 0 {
		allContacts = dedupeWAContacts(allContacts)
		return EngineContactResolveResult{Contacts: allContacts, Queried: inputQueriedCount(input.JIDs), Resolved: contactUsyncIdentityCount(allContacts)}
	}
	proxyURL, err := e.proxyURL()
	if err != nil {
		return EngineContactResolveResult{Contacts: allContacts, Queried: inputQueriedCount(input.JIDs), Resolved: contactUsyncIdentityCount(allContacts), Err: err}
	}
	timeout := input.RemoteTimeout
	if timeout <= 0 {
		timeout = defaultContactUsyncTimeout
	}
	client := newChatdClient(chatdConfigForState(proxyURL, state, timeout))
	for _, batch := range chunkStrings(jids, maxContactUsyncBatchSize) {
		var batchContacts []*waappv1.WAContact
		var lastErr error
		hadSuccess := false
		for _, variant := range contactUsyncVariants() {
			request := buildContactUsyncIQ(e.ids.NewID("waiq_"), e.ids.NewID("sync_sid_query_"), batch, variant)
			response, update, err := client.sendIQ(ctx, state, input.RegisteredIdentityID, defaultWAAppVersion, request, "contact usync iq timed out")
			if applyChatdConnectionState(&state, update) {
				_ = e.saveState(ctx, input.ClientProfileID, state)
			}
			if err != nil {
				lastErr = err
				continue
			}
			hadSuccess = true
			logContactUsyncShape(variant.Name, response)
			contacts := contactsFromContactUsyncIQ(input.WAAccountID, response, e.clock.Now(), batch)
			batchContacts = append(batchContacts, contacts...)
			if contactUsyncIdentityCount(contacts) >= len(batch) {
				break
			}
		}
		if !hadSuccess && lastErr != nil {
			if contactUsyncOptionalRemoteFailure(lastErr) {
				return EngineContactResolveResult{Contacts: allContacts, Queried: inputQueriedCount(input.JIDs), Resolved: contactUsyncIdentityCount(allContacts)}
			}
			return EngineContactResolveResult{Contacts: allContacts, Queried: inputQueriedCount(input.JIDs), Resolved: contactUsyncIdentityCount(allContacts), Err: contactUsyncError(lastErr)}
		}
		allContacts = append(allContacts, batchContacts...)
	}
	allContacts = dedupeWAContacts(allContacts)
	return EngineContactResolveResult{Contacts: allContacts, Queried: inputQueriedCount(input.JIDs), Resolved: contactUsyncIdentityCount(allContacts)}
}

func contactsFromNativeStateMessagePayloads(accountID string, state nativeState, jids []string, now time.Time) []*waappv1.WAContact {
	if accountID == "" || len(jids) == 0 || (len(state.MessagePayloads) == 0 && len(state.ContactHints) == 0) {
		return nil
	}
	requested := map[string]struct{}{}
	for _, jid := range jids {
		jid = normalizeWAJID(jid)
		if strings.HasSuffix(jid, "@lid") {
			requested[jid] = struct{}{}
		}
	}
	if len(requested) == 0 {
		return nil
	}
	hints := []waContactHint{}
	for _, hint := range state.ContactHints {
		if _, ok := requested[hint.normalized().LIDJID]; ok {
			hints = append(hints, hint)
		}
	}
	for _, payload := range state.MessagePayloads {
		for _, hint := range contactHintsFromNativePayloadMetadata(payload) {
			if _, ok := requested[hint.normalized().LIDJID]; ok {
				hints = append(hints, hint)
			}
		}
	}
	return contactsFromContactHints(accountID, nil, hints, now)
}

func unresolvedContactResolveJIDs(jids []string, contacts []*waappv1.WAContact) []string {
	resolved := map[string]struct{}{}
	for _, contact := range contacts {
		if contactUsyncHasIdentity(contact) {
			resolved[contact.GetJid()] = struct{}{}
		}
	}
	out := []string{}
	for _, jid := range jids {
		if _, ok := resolved[jid]; ok {
			continue
		}
		out = append(out, jid)
	}
	return out
}

func inputQueriedCount(values []string) int {
	return len(normalizeContactUsyncJIDs(values))
}

type contactUsyncVariant struct {
	Name             string
	Context          string
	UserContainer    string
	Query            []chatdNode
	UsyncExtraAttrs  map[string]string
	IncludeEmptyList bool
}

func contactUsyncVariants() []contactUsyncVariant {
	return []contactUsyncVariant{
		{
			Name:          "interactive_username_contact",
			Context:       "interactive",
			UserContainer: "list",
			Query:         []chatdNode{{Tag: "username"}, {Tag: "contact"}},
		},
		{
			Name:          "interactive_contact_lid_business",
			Context:       "interactive",
			UserContainer: "list",
			Query: []chatdNode{
				{Tag: "username"},
				{Tag: "contact"},
				{Tag: "lid"},
				buildContactUsyncBusinessQuery(),
			},
		},
		{
			Name:          "interactive_contact_addressed_lid",
			Context:       "interactive",
			UserContainer: "list",
			Query: []chatdNode{
				{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "lid"},
				{Tag: "username"},
				buildContactUsyncBusinessQuery(),
			},
		},
		{
			Name:          "message_lid_migration",
			Context:       "message",
			UserContainer: "list",
			Query: []chatdNode{
				{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "lid"},
				{Tag: "username"},
				buildContactUsyncBusinessQuery(),
			},
		},
		{
			Name:          "notification_lid_migration",
			Context:       "notification",
			UserContainer: "list",
			Query: []chatdNode{
				{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "lid"},
				{Tag: "username"},
				buildContactUsyncBusinessQuery(),
			},
		},
		{
			Name:          "interactive_sidelist_lid",
			Context:       "interactive",
			UserContainer: "side_list",
			Query: []chatdNode{
				{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "sidelist", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "lid"},
				{Tag: "username"},
			},
		},
		{
			Name:          "interactive_sidelist_plain",
			Context:       "interactive",
			UserContainer: "side_list",
			Query: []chatdNode{
				{Tag: "contact", Attrs: map[string]string{"addressing_mode": "lid"}},
				{Tag: "sidelist"},
				{Tag: "lid"},
				{Tag: "username"},
			},
		},
	}
}

func buildContactUsyncBusinessQuery() chatdNode {
	return chatdNode{
		Tag: "business",
		Content: []chatdNode{
			{Tag: "verified_name"},
			{Tag: "profile", Attrs: map[string]string{"v": "1"}},
		},
	}
}

func buildContactUsyncIQ(id string, sid string, jids []string, variant contactUsyncVariant) chatdNode {
	users := make([]chatdNode, 0, len(jids))
	for _, jid := range jids {
		users = append(users, chatdNode{Tag: "user", Attrs: map[string]string{"jid": jid}})
	}
	attrs := map[string]string{
		"sid":     sid,
		"index":   "0",
		"last":    "true",
		"mode":    "query",
		"context": firstNonEmpty(variant.Context, "interactive"),
	}
	for key, value := range variant.UsyncExtraAttrs {
		attrs[key] = value
	}
	userContainer := firstNonEmpty(variant.UserContainer, "list")
	content := []chatdNode{
		{Tag: "query", Content: variant.Query},
		{Tag: userContainer, Content: users},
	}
	if userContainer == "side_list" && variant.IncludeEmptyList {
		content = append(content, chatdNode{Tag: "list", Content: []chatdNode{}})
	}
	return chatdNode{
		Tag:   "iq",
		Attrs: map[string]string{"xmlns": "usync", "id": id, "type": "get"},
		Content: []chatdNode{{
			Tag:     "usync",
			Attrs:   attrs,
			Content: content,
		}},
	}
}

func contactsFromContactUsyncIQ(accountID string, response chatdNode, now time.Time, requestedJIDs []string) []*waappv1.WAContact {
	if accountID == "" {
		return nil
	}
	usync, ok := findChatdNode(response, "usync")
	if !ok {
		return nil
	}
	contacts := []*waappv1.WAContact{}
	for _, listTag := range []string{"list", "side_list"} {
		if listNode, ok := chatdChild(usync, listTag); ok {
			contacts = append(contacts, contactsFromContactUsyncList(accountID, listNode, now, requestedJIDs)...)
		}
	}
	return dedupeWAContacts(contacts)
}

func contactsFromContactUsyncList(accountID string, listNode chatdNode, now time.Time, requestedJIDs []string) []*waappv1.WAContact {
	contacts := []*waappv1.WAContact{}
	for index, userNode := range chatdChildren(listNode) {
		if userNode.Tag != "user" {
			continue
		}
		fallbackLID := ""
		if index < len(requestedJIDs) {
			fallbackLID = requestedJIDs[index]
		}
		if contact := contactFromContactUsyncUser(accountID, userNode, now, fallbackLID); contact != nil {
			contacts = append(contacts, contact)
		}
	}
	return contacts
}

func contactFromContactUsyncUser(accountID string, userNode chatdNode, now time.Time, fallbackLID string) *waappv1.WAContact {
	jid := normalizeWAJID(userNode.Attrs["jid"])
	contactNode, _ := chatdChild(userNode, "contact")
	pnJID := normalizeWAJID(firstNonEmpty(
		userNode.Attrs["pn_jid"],
		userNode.Attrs["new_jid"],
		contactNode.Attrs["pn_jid"],
		contactNode.Attrs["jid"],
		firstPNJIDInNode(userNode),
	))
	lidJID := ""
	switch {
	case strings.HasSuffix(jid, "@lid"):
		lidJID = jid
	case strings.HasSuffix(jid, "@s.whatsapp.net"):
		pnJID = firstNonEmpty(pnJID, jid)
	}
	if lidNode, ok := chatdChild(userNode, "lid"); ok {
		lidJID = firstNonEmpty(lidJID, normalizeWAJID(firstNonEmpty(lidNode.Attrs["val"], lidNode.Attrs["jid"], chatdNodeText(lidNode))))
	}
	if businessNode, ok := chatdChild(userNode, "business"); ok {
		pnJID = firstNonEmpty(pnJID, normalizeWAJID(businessNode.Attrs["pn_jid"]), firstPNJIDInNode(businessNode))
	}
	lidJID = firstNonEmpty(lidJID, firstLIDJIDInNode(userNode), normalizeContactUsyncFallbackLID(fallbackLID))
	if lidJID == "" {
		return nil
	}
	contact := contactFromRef(accountID, lidJID, now, now)
	if contact == nil {
		return nil
	}
	contact.Number = firstNonEmpty(contactNumberForJID(pnJID), contactUsyncPhoneNumber(userNode))
	displayName, waName, verifiedName, business := contactUsyncNames(userNode)
	contact.DisplayName = firstNonEmpty(displayName, verifiedName, waName, fallbackWAContactDisplayName(contact.GetKind(), contact.GetJid(), contact.GetNumber()))
	contact.WaName = waName
	contact.VerifiedName = verifiedName
	if business || verifiedName != "" {
		contact.Kind = waappv1.WAContactKind_WA_CONTACT_KIND_BUSINESS
	}
	return contact
}

func normalizeContactUsyncFallbackLID(jid string) string {
	jid = normalizeWAJID(jid)
	if strings.HasSuffix(jid, "@lid") {
		return jid
	}
	return ""
}

func contactUsyncNames(userNode chatdNode) (string, string, string, bool) {
	contactNode, _ := chatdChild(userNode, "contact")
	usernameNode, hasUsername := chatdChild(userNode, "username")
	businessNode, hasBusiness := chatdChild(userNode, "business")
	displayName := waContactName(firstNonEmpty(
		userNode.Attrs["display_name"],
		userNode.Attrs["notify"],
		contactNode.Attrs["display_name"],
		contactNode.Attrs["name"],
		contactNode.Attrs["notify"],
		contactTextName(contactNode),
	))
	username := ""
	if hasUsername {
		username = waContactName(firstNonEmpty(firstUsernameInNode(usernameNode), chatdNodeText(usernameNode), usernameNode.Attrs["username"], usernameNode.Attrs["value"], usernameNode.Attrs["id"]))
	}
	username = waContactName(firstNonEmpty(username, firstUsernameInNode(userNode), userNode.Attrs["username"], contactNode.Attrs["username"]))
	verifiedName := ""
	if hasBusiness {
		if verifiedNode, ok := chatdChild(businessNode, "verified_name"); ok {
			verifiedName = waContactName(firstNonEmpty(chatdNodeText(verifiedNode), verifiedNode.Attrs["display_name"], verifiedNode.Attrs["name"]))
		}
		if profileNode, ok := chatdChild(businessNode, "profile"); ok {
			displayName = firstNonEmpty(displayName, waContactName(firstNonEmpty(profileNode.Attrs["display_name"], profileNode.Attrs["name"], chatdNodeText(profileNode))))
		}
	}
	return displayName, username, verifiedName, hasBusiness
}

func firstUsernameInNode(node chatdNode) string {
	for _, key := range []string{"username", "value", "id"} {
		if value := waContactUsername(node.Attrs[key]); value != "" {
			return value
		}
	}
	if node.Tag == "username" || node.Tag == "username_info" {
		if value := waContactUsername(chatdNodeText(node)); value != "" {
			return value
		}
	}
	for _, child := range chatdChildren(node) {
		if value := firstUsernameInNode(child); value != "" {
			return value
		}
	}
	return ""
}

func waContactUsername(value string) string {
	value = waContactName(value)
	if value == "" || strings.Contains(value, "@") || strings.Contains(value, "://") {
		return ""
	}
	return value
}

func contactTextName(node chatdNode) string {
	text := waContactName(chatdNodeText(node))
	switch strings.ToLower(text) {
	case "", "in", "out", "invalid":
		return ""
	default:
		return text
	}
}

func firstPNJIDInNode(node chatdNode) string {
	return firstJIDInNodeBySuffix(node, "@s.whatsapp.net")
}

func firstLIDJIDInNode(node chatdNode) string {
	return firstJIDInNodeBySuffix(node, "@lid")
}

func firstJIDInNodeBySuffix(node chatdNode, suffix string) string {
	for _, value := range node.Attrs {
		jid := normalizeWAJID(value)
		if strings.HasSuffix(jid, suffix) {
			return jid
		}
	}
	if text := normalizeWAJID(chatdNodeText(node)); strings.HasSuffix(text, suffix) {
		return text
	}
	for _, child := range chatdChildren(node) {
		if jid := firstJIDInNodeBySuffix(child, suffix); jid != "" {
			return jid
		}
	}
	return ""
}

func contactUsyncPhoneNumber(node chatdNode) string {
	for _, key := range []string{"number", "phone", "pn", "wa_id"} {
		if number := digitsOnly(node.Attrs[key]); number != "" {
			return number
		}
	}
	for _, child := range chatdChildren(node) {
		if number := contactUsyncPhoneNumber(child); number != "" {
			return number
		}
	}
	return ""
}

func normalizeContactUsyncJIDs(values []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		jid := normalizeWAJID(value)
		if !strings.HasSuffix(jid, "@lid") {
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

func findChatdNode(node chatdNode, tag string) (chatdNode, bool) {
	if node.Tag == tag {
		return node, true
	}
	for _, child := range chatdChildren(node) {
		if found, ok := findChatdNode(child, tag); ok {
			return found, true
		}
	}
	return chatdNode{}, false
}

func dedupeWAContacts(contacts []*waappv1.WAContact) []*waappv1.WAContact {
	if len(contacts) == 0 {
		return nil
	}
	merged := map[string]*waappv1.WAContact{}
	order := []string{}
	for _, contact := range contacts {
		if contact == nil || contact.GetJid() == "" {
			continue
		}
		key := contact.GetJid()
		current := merged[key]
		if current == nil {
			merged[key] = contact
			order = append(order, key)
			continue
		}
		current.Number = firstNonEmpty(current.GetNumber(), contact.GetNumber())
		current.DisplayName = betterWAContactDisplayName(current, contact.GetDisplayName())
		current.WaName = firstNonEmpty(current.GetWaName(), contact.GetWaName())
		current.VerifiedName = firstNonEmpty(current.GetVerifiedName(), contact.GetVerifiedName())
		if current.GetKind() == waappv1.WAContactKind_WA_CONTACT_KIND_USER && contact.GetKind() != waappv1.WAContactKind_WA_CONTACT_KIND_UNSPECIFIED {
			current.Kind = contact.GetKind()
		}
		current.IsWhatsappUser = current.GetIsWhatsappUser() || contact.GetIsWhatsappUser()
		current.IsReachable = current.GetIsReachable() || contact.GetIsReachable()
	}
	out := make([]*waappv1.WAContact, 0, len(order))
	for _, key := range order {
		out = append(out, merged[key])
	}
	return out
}

func contactUsyncIdentityCount(contacts []*waappv1.WAContact) int {
	count := 0
	for _, contact := range contacts {
		if contactUsyncHasIdentity(contact) {
			count++
		}
	}
	return count
}

func contactUsyncHasIdentity(contact *waappv1.WAContact) bool {
	if contact == nil {
		return false
	}
	if contact.GetNumber() != "" || contact.GetWaName() != "" || contact.GetVerifiedName() != "" {
		return true
	}
	name := strings.TrimSpace(contact.GetDisplayName())
	return name != "" && name != "未知联系人" && !strings.HasPrefix(name, "联系人 ") && !strings.HasPrefix(name, "LID ")
}

func betterWAContactDisplayName(contact *waappv1.WAContact, candidate string) string {
	candidate = waContactName(candidate)
	if candidate == "" {
		return contact.GetDisplayName()
	}
	current := contact.GetDisplayName()
	if current == "" || current == "未知联系人" || strings.HasPrefix(current, "联系人 ") || strings.HasPrefix(current, "LID ") {
		return candidate
	}
	return current
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 || len(values) <= size {
		return [][]string{values}
	}
	chunks := [][]string{}
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

func contactUsyncError(err error) error {
	message := "native contact usync failed"
	if snippet := chatdSafeFailureMessage(err); snippet != "" {
		message += ": " + snippet
	}
	return NewError(waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, message, accountSettingsRetryableError(err))
}

func contactUsyncOptionalRemoteFailure(err error) bool {
	return err != nil && accountSettingsRetryableError(err)
}

func logContactUsyncShape(variant string, response chatdNode) {
	shape := newContactUsyncShape()
	shape.IQType = response.Attrs["type"]
	if usync, ok := findChatdNode(response, "usync"); ok {
		shape.UsyncAttrKeys = sortedMapKeys(usync.Attrs)
		shape.QueryProtocolTags = childTagCounts(chatdChildOrZero(usync, "query"))
		shape.ListUsers = countContactUsyncUsers(usync, "list", shape)
		shape.SideListUsers = countContactUsyncUsers(usync, "side_list", shape)
	}
	log.Printf(
		"WA contact usync shape variant=%s iq_type=%s list_users=%d side_list_users=%d usync_attr_keys=%s query_protocols=%s user_attr_keys=%s user_jid_domains=%s child_tags=%s child_attr_keys=%s pn_hints=%d lid_hints=%d name_hints=%d",
		variant,
		shape.IQType,
		shape.ListUsers,
		shape.SideListUsers,
		strings.Join(shape.UsyncAttrKeys, ","),
		formatCountMap(shape.QueryProtocolTags),
		formatCountMap(shape.UserAttrKeys),
		formatCountMap(shape.UserJIDDomains),
		formatCountMap(shape.ChildTags),
		formatCountMap(shape.ChildAttrKeys),
		shape.PNHints,
		shape.LIDHints,
		shape.NameHints,
	)
}

type contactUsyncShape struct {
	IQType            string
	UsyncAttrKeys     []string
	QueryProtocolTags map[string]int
	UserAttrKeys      map[string]int
	UserJIDDomains    map[string]int
	ChildTags         map[string]int
	ChildAttrKeys     map[string]int
	ListUsers         int
	SideListUsers     int
	PNHints           int
	LIDHints          int
	NameHints         int
}

func newContactUsyncShape() *contactUsyncShape {
	return &contactUsyncShape{
		QueryProtocolTags: map[string]int{},
		UserAttrKeys:      map[string]int{},
		UserJIDDomains:    map[string]int{},
		ChildTags:         map[string]int{},
		ChildAttrKeys:     map[string]int{},
	}
}

func countContactUsyncUsers(usync chatdNode, listTag string, shape *contactUsyncShape) int {
	listNode, ok := chatdChild(usync, listTag)
	if !ok {
		return 0
	}
	count := 0
	for _, userNode := range chatdChildren(listNode) {
		if userNode.Tag != "user" {
			continue
		}
		count++
		for key, value := range userNode.Attrs {
			shape.UserAttrKeys[key]++
			if key == "jid" || strings.HasSuffix(key, "_jid") || key == "new_jid" {
				shape.UserJIDDomains[jidDomainClass(value)]++
			}
		}
		for _, child := range chatdChildren(userNode) {
			collectContactUsyncChildShape(shape, child, child.Tag)
		}
		if firstPNJIDInNode(userNode) != "" {
			shape.PNHints++
		}
		if firstLIDJIDInNode(userNode) != "" {
			shape.LIDHints++
		}
		displayName, waName, verifiedName, _ := contactUsyncNames(userNode)
		if firstNonEmpty(displayName, waName, verifiedName) != "" {
			shape.NameHints++
		}
	}
	return count
}

func collectContactUsyncChildShape(shape *contactUsyncShape, node chatdNode, path string) {
	if path == "" {
		path = node.Tag
	}
	shape.ChildTags[path]++
	for key := range node.Attrs {
		shape.ChildAttrKeys[path+"."+key]++
	}
	for _, child := range chatdChildren(node) {
		childPath := child.Tag
		if path != "" {
			childPath = path + "/" + child.Tag
		}
		collectContactUsyncChildShape(shape, child, childPath)
	}
}

func chatdChildOrZero(node chatdNode, tag string) chatdNode {
	child, _ := chatdChild(node, tag)
	return child
}

func childTagCounts(node chatdNode) map[string]int {
	out := map[string]int{}
	for _, child := range chatdChildren(node) {
		out[child.Tag]++
	}
	return out
}

func jidDomainClass(value string) string {
	value = normalizeWAJID(value)
	switch {
	case strings.HasSuffix(value, "@lid"):
		return "@lid"
	case strings.HasSuffix(value, "@s.whatsapp.net"):
		return "@s.whatsapp.net"
	case strings.Contains(value, "@"):
		return "@other"
	case strings.TrimSpace(value) == "":
		return "empty"
	default:
		return "non_jid"
	}
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func formatCountMap(values map[string]int) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+":"+strconv.Itoa(values[key]))
	}
	return strings.Join(parts, ",")
}
