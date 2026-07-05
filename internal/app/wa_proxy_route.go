package app

import (
	"strings"

	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
)

func proxyCountryCodeFromPayload(payload map[string]any) string {
	phone := objectField(payload, "phone")
	proxy := objectField(payload, "proxy")
	value := shared.FirstNonEmpty(
		textField(payload, "proxy_country_code"),
		textField(proxy, "country_code"),
		textField(proxy, "proxy_country_code"),
		textField(payload, "country_iso2"),
		textField(payload, "country_region"),
		textField(payload, "region"),
		textField(phone, "country_iso2"),
	)
	if value != "" {
		return normalizeProxyCountryCode(value)
	}
	callingCode := shared.FirstNonEmpty(
		textField(payload, "country_calling_code"),
		textField(payload, "cc"),
		textField(payload, "country_code"),
		textField(phone, "country_calling_code"),
	)
	return normalizeProxyCountryCode(proxyCountryCodeFromCallingCode(callingCode))
}

func proxyCountryCodeFromCallingCode(value string) string {
	switch strings.TrimPrefix(strings.TrimSpace(value), "+") {
	case "1":
		return "US"
	case "48":
		return "PL"
	case "57":
		return "CO"
	case "63":
		return "PH"
	case "84":
		return "VN"
	case "86":
		return "CN"
	default:
		return ""
	}
}

func normalizeProxyCountryCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "+")
	switch value {
	case "", "1", "USA", "UNITEDSTATES", "UNITED_STATES":
		return "US"
	default:
		return value
	}
}
