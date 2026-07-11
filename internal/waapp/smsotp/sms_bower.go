package smsotp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/byte-v-forge/wa-app/internal/waapp/countrycatalog"
)

const (
	smsBowerName              = "smsbower"
	smsBowerLifecycleEndpoint = "https://smsbower.page/stubs/handler_api.php"
	smsBowerWhatsAppService   = "whatsapp"
	smsBowerCountriesCacheTTL = 5 * time.Minute
)

type SMSBowerProvider struct {
	apiKey string
	client *http.Client

	mu          sync.Mutex
	mappings    map[string]smsBowerMapping
	countries   []smsBowerCountry
	countriesAt time.Time
}

type smsBowerMapping struct {
	ServiceCode string
	CountryID   string
}

type smsBowerCountry struct {
	ID          string
	CountryISO2 string
	Name        string
}

func NewSMSBowerProvider(apiKey string) *SMSBowerProvider {
	return NewSMSBowerProviderWithClient(apiKey, &http.Client{Timeout: 20 * time.Second})
}

func NewSMSBowerProviderWithClient(apiKey string, client *http.Client) *SMSBowerProvider {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &SMSBowerProvider{apiKey: strings.TrimSpace(apiKey), client: client, mappings: map[string]smsBowerMapping{}}
}

func (*SMSBowerProvider) Name() string { return smsBowerName }

func (p *SMSBowerProvider) ListCountries(ctx context.Context) ([]Country, error) {
	countries, err := p.countriesForRegistration(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Country, 0, len(countries))
	for _, country := range countries {
		result = append(result, Country{CountryISO2: country.CountryISO2, Name: country.Name})
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].Name == result[right].Name {
			return result[left].CountryISO2 < result[right].CountryISO2
		}
		return result[left].Name < result[right].Name
	})
	return result, nil
}

func (p *SMSBowerProvider) ListOffers(ctx context.Context, countryISO2 string, service string) ([]Offer, error) {
	if !p.configured() {
		return nil, ErrNotConfigured
	}
	countryISO2 = normalizeCountryISO2(countryISO2)
	if service = normalizeService(service); service != smsBowerWhatsAppService {
		return nil, fmt.Errorf("SMSBower supports WhatsApp offers only")
	}
	mapping, err := p.mapping(ctx, countryISO2)
	if err != nil {
		return nil, err
	}
	value, statusCode, err := p.lifecycle(ctx, url.Values{"action": {"getPricesV3"}, "service": {mapping.ServiceCode}, "country": {mapping.CountryID}})
	if err != nil {
		return nil, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil, smsBowerResponseError(value, statusCode)
	}
	return smsBowerOffers(value, countryISO2, mapping), nil
}

func (p *SMSBowerProvider) AcquireNumber(ctx context.Context, input AcquireInput) (Activation, error) {
	if !p.configured() {
		return Activation{}, ErrNotConfigured
	}
	mapping, err := p.mapping(ctx, input.CountryISO2)
	if err != nil {
		return Activation{}, err
	}
	providerID, price, err := smsBowerOfferParts(input.Offer, normalizeCountryISO2(input.CountryISO2), mapping.ServiceCode)
	if err != nil {
		return Activation{}, err
	}
	query := url.Values{}
	query.Set("action", "getNumberV2")
	query.Set("service", mapping.ServiceCode)
	query.Set("country", mapping.CountryID)
	query.Set("providerIds", providerID)
	query.Set("maxPrice", strconv.FormatFloat(price, 'f', -1, 64))
	query.Set("minPrice", strconv.FormatFloat(price, 'f', -1, 64))
	value, statusCode, err := p.lifecycle(ctx, query)
	if err != nil {
		return Activation{}, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return Activation{}, smsBowerResponseError(value, statusCode)
	}
	activationID, phone, operator, actualPrice, ok := smsBowerActivation(value)
	if !ok {
		return Activation{}, smsBowerStatusError(smsBowerStatusText(value))
	}
	return Activation{ActivationID: activationID, PhoneE164: phone, CountryISO2: normalizeCountryISO2(input.CountryISO2), Operator: operator, Price: actualPrice, Currency: input.Offer.Currency}, nil
}

func (p *SMSBowerProvider) MarkReady(ctx context.Context, activationID string) error {
	return p.setStatus(ctx, activationID, "1", "ACCESS_READY")
}

func (p *SMSBowerProvider) PollCode(ctx context.Context, activationID string) (ActivationStatus, error) {
	value, statusCode, err := p.lifecycle(ctx, url.Values{"action": {"getStatus"}, "id": {activationID}})
	if err != nil {
		return ActivationStatus{}, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return ActivationStatus{}, smsBowerResponseError(value, statusCode)
	}
	status := smsBowerStatusText(value)
	if status == "" {
		return ActivationStatus{}, fmt.Errorf("SMSBower returned an unknown activation status")
	}
	parts := strings.SplitN(status, ":", 2)
	result := ActivationStatus{Status: parts[0]}
	if len(parts) == 2 && strings.EqualFold(parts[0], "STATUS_OK") {
		result.Code = strings.TrimSpace(parts[1])
	}
	return result, nil
}

func (p *SMSBowerProvider) Complete(ctx context.Context, activationID string) error {
	return p.setStatus(ctx, activationID, "6", "ACCESS_ACTIVATION")
}

func (p *SMSBowerProvider) Cancel(ctx context.Context, activationID string) error {
	return p.setStatus(ctx, activationID, "8", "ACCESS_CANCEL")
}

func (p *SMSBowerProvider) setStatus(ctx context.Context, activationID string, status string, expected string) error {
	value, statusCode, err := p.lifecycle(ctx, url.Values{"action": {"setStatus"}, "id": {activationID}, "status": {status}})
	if err != nil {
		return err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return smsBowerResponseError(value, statusCode)
	}
	response := smsBowerStatusText(value)
	if response == expected {
		return nil
	}
	return smsBowerStatusError(response)
}

func (p *SMSBowerProvider) mapping(ctx context.Context, countryISO2 string) (smsBowerMapping, error) {
	countryISO2 = normalizeCountryISO2(countryISO2)
	p.mu.Lock()
	cached, ok := p.mappings[countryISO2]
	p.mu.Unlock()
	if ok {
		return cached, nil
	}
	services, statusCode, err := p.lifecycle(ctx, url.Values{"action": {"getServicesList"}})
	if err != nil {
		return smsBowerMapping{}, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return smsBowerMapping{}, smsBowerResponseError(services, statusCode)
	}
	mapping := smsBowerMapping{ServiceCode: smsBowerFindWhatsAppServiceCode(services)}
	countries, err := p.countriesForRegistration(ctx)
	if err != nil {
		return smsBowerMapping{}, err
	}
	for _, country := range countries {
		if country.CountryISO2 == countryISO2 {
			mapping.CountryID = country.ID
			break
		}
	}
	if mapping.ServiceCode == "" || mapping.CountryID == "" {
		return smsBowerMapping{}, fmt.Errorf("SMSBower does not expose the requested WhatsApp country mapping")
	}
	p.mu.Lock()
	p.mappings[countryISO2] = mapping
	p.mu.Unlock()
	return mapping, nil
}

func (p *SMSBowerProvider) countriesForRegistration(ctx context.Context) ([]smsBowerCountry, error) {
	if !p.configured() {
		return nil, ErrNotConfigured
	}
	now := time.Now().UTC()
	p.mu.Lock()
	if len(p.countries) > 0 && now.Sub(p.countriesAt) < smsBowerCountriesCacheTTL {
		result := append([]smsBowerCountry(nil), p.countries...)
		p.mu.Unlock()
		return result, nil
	}
	p.mu.Unlock()
	value, statusCode, err := p.lifecycle(ctx, url.Values{"action": {"getCountries"}})
	if err != nil {
		return nil, err
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		return nil, smsBowerResponseError(value, statusCode)
	}
	countries := smsBowerCountries(value)
	if len(countries) == 0 {
		return nil, fmt.Errorf("SMSBower does not expose any supported registration countries")
	}
	p.mu.Lock()
	p.countries = append([]smsBowerCountry(nil), countries...)
	p.countriesAt = now
	p.mu.Unlock()
	return countries, nil
}

func (p *SMSBowerProvider) lifecycle(ctx context.Context, query url.Values) (any, int, error) {
	if !p.configured() {
		return nil, 0, ErrNotConfigured
	}
	endpoint, err := url.Parse(smsBowerLifecycleEndpoint)
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

func (p *SMSBowerProvider) doJSON(req *http.Request) (any, int, error) {
	return (&HeroSMSProvider{client: p.client}).doJSON(req)
}

func (p *SMSBowerProvider) configured() bool {
	return p != nil && p.client != nil && strings.TrimSpace(p.apiKey) != ""
}

func smsBowerOffers(value any, countryISO2 string, mapping smsBowerMapping) []Offer {
	service := heroSMSObject(heroSMSObject(value)[mapping.CountryID])[mapping.ServiceCode]
	providers := heroSMSObject(service)
	result := make([]Offer, 0, len(providers))
	for fallbackProviderID, raw := range providers {
		entry := heroSMSObject(raw)
		price := heroSMSNumber(entry["price"])
		count := int(heroSMSNumber(entry["count"]))
		providerID := strings.TrimSpace(heroSMSString(entry, "provider_id"))
		if providerID == "" {
			providerID = strings.TrimSpace(fallbackProviderID)
		}
		if providerID == "" || price <= 0 || count <= 0 {
			continue
		}
		priceTier := strconv.FormatFloat(price, 'f', -1, 64)
		result = append(result, Offer{OfferID: strings.Join([]string{smsBowerName, countryISO2, mapping.ServiceCode, "provider", providerID, "price", priceTier}, ":"), Provider: smsBowerName, CountryISO2: countryISO2, Service: smsBowerWhatsAppService, Price: price, Currency: "USD", AvailableCount: count, PriceTier: providerID + ":" + priceTier, Operator: "channel:" + providerID})
	}
	sort.Slice(result, func(left int, right int) bool {
		if result[left].Price == result[right].Price {
			return result[left].PriceTier < result[right].PriceTier
		}
		return result[left].Price < result[right].Price
	})
	return result
}

func smsBowerOfferParts(offer Offer, countryISO2 string, serviceCode string) (string, float64, error) {
	parts := strings.Split(offer.OfferID, ":")
	if len(parts) != 7 || parts[0] != smsBowerName || parts[1] != countryISO2 || parts[2] != serviceCode || parts[3] != "provider" || parts[5] != "price" {
		return "", 0, fmt.Errorf("SMSBower offer is invalid")
	}
	price, err := strconv.ParseFloat(parts[6], 64)
	if err != nil || price <= 0 || strings.TrimSpace(parts[4]) == "" {
		return "", 0, fmt.Errorf("SMSBower offer is invalid")
	}
	return parts[4], price, nil
}

func smsBowerActivation(value any) (string, string, string, float64, bool) {
	activation := heroSMSObject(value)
	activationID := heroSMSString(activation, "activationId", "activation_id", "id")
	phone := normalizeE164(heroSMSString(activation, "phoneNumber", "phone_number", "phone"))
	operator := strings.TrimSpace(heroSMSString(activation, "activationOperator", "activation_operator", "operator"))
	price := heroSMSFloat(activation, "activationCost", "activation_cost", "cost", "price")
	if activationID == "" || phone == "" || price <= 0 {
		return "", "", "", 0, false
	}
	if operator == "" {
		operator = "unknown"
	}
	return activationID, phone, operator, price, true
}

func smsBowerCountries(value any) []smsBowerCountry {
	result := make([]smsBowerCountry, 0)
	seen := map[string]struct{}{}
	var collect func(any, string)
	collect = func(current any, fallbackID string) {
		switch typed := current.(type) {
		case map[string]any:
			id := heroSMSString(typed, "id", "countryId", "country_id")
			if id == "" {
				id = fallbackID
			}
			englishName := heroSMSString(typed, "eng", "english", "name_en", "name")
			chineseName := heroSMSString(typed, "chn", "chinese", "name_zh", "name_cn")
			if id != "" && englishName != "" {
				if countryISO2 := countrycatalog.ISO2FromCountryNames(englishName); countryISO2 != "" {
					key := countryISO2 + ":" + id
					if _, found := seen[key]; !found {
						seen[key] = struct{}{}
						result = append(result, smsBowerCountry{ID: id, CountryISO2: countryISO2, Name: sharedCountryName(chineseName, englishName)})
					}
				}
			}
			for key, child := range typed {
				collect(child, key)
			}
		case []any:
			for _, child := range typed {
				collect(child, fallbackID)
			}
		}
	}
	collect(value, "")
	return result
}

func smsBowerFindWhatsAppServiceCode(value any) string {
	object := heroSMSObject(value)
	services, ok := object["services"].([]any)
	if !ok {
		services = []any{value}
	}
	for _, entry := range services {
		service := heroSMSObject(entry)
		code := heroSMSString(service, "code", "serviceCode", "service_code")
		name := strings.ToLower(heroSMSString(service, "name", "title"))
		if code != "" && strings.Contains(name, "whatsapp") {
			return code
		}
	}
	return ""
}

func smsBowerStatusText(value any) string { return heroSMSStatusText(value) }

func smsBowerResponseError(value any, statusCode int) error {
	message := smsBowerStatusText(value)
	if message == "" {
		message = "SMSBower request failed"
	}
	return fmt.Errorf("%s (HTTP %d)", compactProviderMessage(message), statusCode)
}

func smsBowerStatusError(status string) error {
	status = compactProviderMessage(status)
	if status == "" {
		status = "SMSBower returned an unknown status"
	}
	return fmt.Errorf("SMSBower: %s", status)
}
