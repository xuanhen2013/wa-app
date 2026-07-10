package smsotp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	heroSMSName              = "hero-sms"
	heroSMSOffersEndpoint    = "https://hero-sms.com/api/v1/activations/offers"
	heroSMSLifecycleEndpoint = "https://hero-sms.com/stubs/handler_api.php"
	heroSMSWhatsAppService   = "whatsapp"
)

type HeroSMSProvider struct {
	apiKey string
	client *http.Client

	mu       sync.Mutex
	mappings map[string]heroSMSMapping
}

type heroSMSMapping struct {
	ServiceCode string
	CountryID   string
}

func NewHeroSMSProvider(apiKey string) *HeroSMSProvider {
	return NewHeroSMSProviderWithClient(apiKey, &http.Client{Timeout: 20 * time.Second})
}

func NewHeroSMSProviderWithClient(apiKey string, client *http.Client) *HeroSMSProvider {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &HeroSMSProvider{apiKey: strings.TrimSpace(apiKey), client: client, mappings: map[string]heroSMSMapping{}}
}

func (p *HeroSMSProvider) Name() string { return heroSMSName }

func (p *HeroSMSProvider) ListOffers(ctx context.Context, countryISO2 string, service string) ([]Offer, error) {
	if !p.configured() {
		return nil, ErrNotConfigured
	}
	countryISO2 = normalizeCountryISO2(countryISO2)
	if service = normalizeService(service); service != heroSMSWhatsAppService {
		return nil, fmt.Errorf("HeroSMS supports WhatsApp offers only")
	}
	mapping, err := p.mapping(ctx, countryISO2)
	if err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(heroSMSOffersEndpoint)
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("services", mapping.ServiceCode)
	query.Set("countries", mapping.CountryID)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "ApiKey "+p.apiKey)
	value, statusCode, err := p.doJSON(req)
	if err != nil {
		return nil, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil, heroSMSResponseError(value, statusCode)
	}
	return heroSMSOffers(value, countryISO2, mapping), nil
}

func (p *HeroSMSProvider) AcquireNumber(ctx context.Context, input AcquireInput) (Activation, error) {
	if !p.configured() {
		return Activation{}, ErrNotConfigured
	}
	mapping, err := p.mapping(ctx, input.CountryISO2)
	if err != nil {
		return Activation{}, err
	}
	query := url.Values{}
	query.Set("action", "getNumber")
	query.Set("service", mapping.ServiceCode)
	query.Set("country", mapping.CountryID)
	query.Set("maxPrice", strconv.FormatFloat(input.Offer.Price, 'f', -1, 64))
	query.Set("fixedPrice", "true")
	value, statusCode, err := p.lifecycle(ctx, query)
	if err != nil {
		return Activation{}, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return Activation{}, heroSMSResponseError(value, statusCode)
	}
	activationID, phone, ok := heroSMSNumberActivation(heroSMSStatusText(value))
	if !ok {
		return Activation{}, heroSMSStatusError(heroSMSStatusText(value))
	}
	if activationID == "" || phone == "" {
		return Activation{}, fmt.Errorf("HeroSMS returned an incomplete activation")
	}
	return Activation{ActivationID: activationID, PhoneE164: phone, CountryISO2: normalizeCountryISO2(input.CountryISO2), Price: input.Offer.Price, Currency: input.Offer.Currency}, nil
}

func (p *HeroSMSProvider) MarkReady(ctx context.Context, activationID string) error {
	return p.setStatus(ctx, activationID, "1")
}

func (p *HeroSMSProvider) PollCode(ctx context.Context, activationID string) (ActivationStatus, error) {
	query := url.Values{}
	query.Set("action", "getStatus")
	query.Set("id", activationID)
	value, statusCode, err := p.lifecycle(ctx, query)
	if err != nil {
		return ActivationStatus{}, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return ActivationStatus{}, heroSMSResponseError(value, statusCode)
	}
	status := heroSMSStatusText(value)
	if status == "" {
		return ActivationStatus{}, fmt.Errorf("HeroSMS returned an unknown activation status")
	}
	parts := strings.SplitN(status, ":", 2)
	result := ActivationStatus{Status: parts[0]}
	if len(parts) == 2 && strings.EqualFold(parts[0], "STATUS_OK") {
		result.Code = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func (p *HeroSMSProvider) Complete(ctx context.Context, activationID string) error {
	if err := p.lifecycleNoContent(ctx, "finishActivation", activationID); err == nil {
		return nil
	}
	return p.setStatus(ctx, activationID, "6")
}

func (p *HeroSMSProvider) Cancel(ctx context.Context, activationID string) error {
	if err := p.lifecycleNoContent(ctx, "cancelActivation", activationID); err == nil {
		return nil
	}
	return p.setStatus(ctx, activationID, "8")
}

func (p *HeroSMSProvider) setStatus(ctx context.Context, activationID string, status string) error {
	query := url.Values{}
	query.Set("action", "setStatus")
	query.Set("id", activationID)
	query.Set("status", status)
	value, statusCode, err := p.lifecycle(ctx, query)
	if err != nil {
		return err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return heroSMSResponseError(value, statusCode)
	}
	response := heroSMSStatusText(value)
	if strings.HasPrefix(response, "ACCESS_") {
		return nil
	}
	return heroSMSStatusError(response)
}

func (p *HeroSMSProvider) lifecycleNoContent(ctx context.Context, action string, activationID string) error {
	query := url.Values{}
	query.Set("action", action)
	query.Set("id", activationID)
	value, statusCode, err := p.lifecycle(ctx, query)
	if err != nil {
		return err
	}
	if statusCode == http.StatusNoContent || (statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices && heroSMSStatusText(value) == "") {
		return nil
	}
	return heroSMSResponseError(value, statusCode)
}

func (p *HeroSMSProvider) lifecycle(ctx context.Context, query url.Values) (any, int, error) {
	if !p.configured() {
		return nil, 0, ErrNotConfigured
	}
	endpoint, err := url.Parse(heroSMSLifecycleEndpoint)
	if err != nil {
		return nil, 0, err
	}
	query.Set("api_key", p.apiKey)
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	return p.doJSON(req)
}

func (p *HeroSMSProvider) mapping(ctx context.Context, countryISO2 string) (heroSMSMapping, error) {
	countryISO2 = normalizeCountryISO2(countryISO2)
	p.mu.Lock()
	cached, ok := p.mappings[countryISO2]
	p.mu.Unlock()
	if ok {
		return cached, nil
	}
	services, serviceStatus, err := p.lifecycle(ctx, url.Values{"action": {"getServicesList"}})
	if err != nil {
		return heroSMSMapping{}, err
	}
	if serviceStatus < http.StatusOK || serviceStatus >= http.StatusMultipleChoices {
		return heroSMSMapping{}, heroSMSResponseError(services, serviceStatus)
	}
	countries, countryStatus, err := p.lifecycle(ctx, url.Values{"action": {"getCountries"}})
	if err != nil {
		return heroSMSMapping{}, err
	}
	if countryStatus < http.StatusOK || countryStatus >= http.StatusMultipleChoices {
		return heroSMSMapping{}, heroSMSResponseError(countries, countryStatus)
	}
	mapping := heroSMSMapping{ServiceCode: heroSMSFindServiceCode(services), CountryID: heroSMSFindCountryID(countries, countryISO2)}
	if mapping.ServiceCode == "" || mapping.CountryID == "" {
		return heroSMSMapping{}, fmt.Errorf("HeroSMS does not expose the requested WhatsApp country mapping")
	}
	p.mu.Lock()
	p.mappings[countryISO2] = mapping
	p.mu.Unlock()
	return mapping, nil
}

func (p *HeroSMSProvider) configured() bool {
	return p != nil && p.client != nil && strings.TrimSpace(p.apiKey) != ""
}

func (p *HeroSMSProvider) doJSON(req *http.Request) (any, int, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	value := any(strings.TrimSpace(string(data)))
	if len(data) > 0 {
		_ = json.Unmarshal(data, &value)
	}
	return value, resp.StatusCode, nil
}

func heroSMSOffers(value any, countryISO2 string, mapping heroSMSMapping) []Offer {
	root := heroSMSObject(value)
	data := heroSMSObject(root["data"])
	service := heroSMSObject(data[mapping.ServiceCode])
	country := heroSMSObject(service[mapping.CountryID])
	priceMap := heroSMSObject(country["map"])
	offers := make([]Offer, 0, len(priceMap))
	for tier, countValue := range priceMap {
		price, err := strconv.ParseFloat(strings.TrimSpace(tier), 64)
		if err != nil || price <= 0 {
			continue
		}
		count := int(heroSMSNumber(countValue))
		if count < 0 {
			count = 0
		}
		offers = append(offers, Offer{OfferID: strings.Join([]string{heroSMSName, countryISO2, mapping.ServiceCode, "price", tier}, ":"), Provider: heroSMSName, CountryISO2: countryISO2, Service: heroSMSWhatsAppService, Price: price, Currency: "USD", AvailableCount: count, PriceTier: tier, Operator: "any"})
	}
	return offers
}

func heroSMSStatusText(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return heroSMSString(typed, "status", "message", "title")
	default:
		return ""
	}
}

func heroSMSNumberActivation(status string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(status), ":", 3)
	if len(parts) != 3 || !strings.EqualFold(parts[0], "ACCESS_NUMBER") {
		return "", "", false
	}
	activationID := strings.TrimSpace(parts[1])
	phone := normalizeE164(parts[2])
	if activationID == "" || phone == "" {
		return "", "", false
	}
	return activationID, phone, true
}

func heroSMSResponseError(value any, statusCode int) error {
	message := heroSMSStatusText(value)
	if message == "" {
		message = "HeroSMS request failed"
	}
	return fmt.Errorf("%s (HTTP %d)", compactProviderMessage(message), statusCode)
}

func heroSMSStatusError(status string) error {
	status = compactProviderMessage(status)
	if status == "" {
		status = "HeroSMS returned an unknown status"
	}
	return fmt.Errorf("HeroSMS: %s", status)
}

func compactProviderMessage(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 180 {
		return value[:180]
	}
	return value
}

func heroSMSObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func heroSMSString(value any, keys ...string) string {
	object := heroSMSObject(value)
	for _, key := range keys {
		switch current := object[key].(type) {
		case string:
			if current = strings.TrimSpace(current); current != "" {
				return current
			}
		case float64:
			return strconv.FormatInt(int64(current), 10)
		case json.Number:
			return current.String()
		}
	}
	return ""
}

func heroSMSFloat(value any, keys ...string) float64 {
	object := heroSMSObject(value)
	for _, key := range keys {
		if result := heroSMSNumber(object[key]); result > 0 {
			return result
		}
	}
	return 0
}

func heroSMSNumber(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		result, _ := typed.Float64()
		return result
	case string:
		result, _ := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return result
	default:
		return 0
	}
}

func heroSMSFindServiceCode(value any) string {
	return heroSMSServiceCode(value)
}

func heroSMSServiceCode(value any) string {
	object := heroSMSObject(value)
	if code := heroSMSString(object, "code", "serviceCode", "service_code"); code != "" && heroSMSContainsWhatsApp(object) {
		return code
	}
	for key, details := range object {
		if strings.Contains(strings.ToLower(key), "whatsapp") {
			return key
		}
		if len(key) >= 2 && len(key) <= 4 && heroSMSContainsWhatsApp(details) {
			return key
		}
		if code := heroSMSServiceCode(details); code != "" {
			return code
		}
	}
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if code := heroSMSServiceCode(item); code != "" {
				return code
			}
		}
	}
	return ""
}

func heroSMSContainsWhatsApp(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(strings.ToLower(typed), "whatsapp")
	case map[string]any:
		for _, current := range typed {
			if heroSMSContainsWhatsApp(current) {
				return true
			}
		}
	case []any:
		for _, current := range typed {
			if heroSMSContainsWhatsApp(current) {
				return true
			}
		}
	}
	return false
}

func heroSMSFindCountryID(value any, countryISO2 string) string {
	return heroSMSCountryID(value, normalizeCountryISO2(countryISO2))
}

func heroSMSCountryID(value any, countryISO2 string) string {
	switch typed := value.(type) {
	case map[string]any:
		if heroSMSCountryMatches(typed, countryISO2) {
			return heroSMSString(typed, "id", "countryId", "country_id")
		}
		for key, current := range typed {
			if text, ok := current.(string); ok && strings.EqualFold(strings.TrimSpace(text), countryName(countryISO2)) {
				return key
			}
			if heroSMSCountryMatches(heroSMSObject(current), countryISO2) {
				return key
			}
			if strings.EqualFold(countryName(countryISO2), strings.TrimSpace(key)) {
				return heroSMSString(current, "id", "countryId", "country_id")
			}
			if result := heroSMSCountryID(current, countryISO2); result != "" {
				return result
			}
		}
	case []any:
		for _, current := range typed {
			if result := heroSMSCountryID(current, countryISO2); result != "" {
				return result
			}
		}
	}
	return ""
}

func heroSMSCountryMatches(value map[string]any, countryISO2 string) bool {
	for _, current := range value {
		if text, ok := current.(string); ok && strings.EqualFold(strings.TrimSpace(text), countryISO2) {
			return true
		}
		if text, ok := current.(string); ok && strings.EqualFold(strings.TrimSpace(text), countryName(countryISO2)) {
			return true
		}
	}
	return false
}

func normalizeCountryISO2(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeService(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "wa" || value == "whatsapp" {
		return heroSMSWhatsAppService
	}
	return value
}

func countryName(countryISO2 string) string {
	switch normalizeCountryISO2(countryISO2) {
	case "PH":
		return "Philippines"
	case "US":
		return "United States"
	case "GB":
		return "United Kingdom"
	default:
		return ""
	}
}

func normalizeE164(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "+") {
		value = "+" + value
	}
	for index, runeValue := range value {
		if index == 0 && runeValue == '+' {
			continue
		}
		if runeValue < '0' || runeValue > '9' {
			return ""
		}
	}
	return value
}
