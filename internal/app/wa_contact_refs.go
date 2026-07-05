package app

import (
	"strings"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func contactActionRefs(contactRef string, contact *waappv1.WAContact) []string {
	refs := contactRefVariants(contactRef)
	if contact != nil {
		refs = append(refs, contact.GetContactId())
		refs = append(refs, contactMessageRefs(contact)...)
	}
	return uniqueNonEmptyStrings(refs...)
}

func contactMessageRefs(contact *waappv1.WAContact) []string {
	if contact == nil {
		return nil
	}
	refs := contactRefVariants(contact.GetJid())
	refs = append(refs, contactRefVariants(contact.GetNumber())...)
	return uniqueNonEmptyStrings(refs...)
}

func contactRefVariants(contactRef string) []string {
	contactRef = strings.TrimSpace(contactRef)
	numberRef := strings.TrimPrefix(contactRef, "+")
	if local, domain, ok := strings.Cut(numberRef, "@"); ok && domain == "s.whatsapp.net" {
		numberRef = local
	} else if strings.Contains(numberRef, "@") {
		numberRef = contactRef
	}
	return uniqueNonEmptyStrings(contactRef, numberRef, normalizeWAJID(numberRef))
}
