package smsotp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
)

func TestHeroSMSProviderUsesV1OffersAndCompatibleLifecycle(t *testing.T) {
	requests := []*http.Request{}
	provider := NewHeroSMSProviderWithClient("test-key", &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Clone(request.Context()))
		body := ""
		statusCode := http.StatusOK
		switch request.URL.Query().Get("action") {
		case "getServicesList":
			body = `{"status":"success","services":[{"code":"wa","name":"WhatsApp"}]}`
		case "getCountries":
			body = `[{"id":4,"eng":"Philippines"}]`
		case "getNumber":
			body = "ACCESS_NUMBER:635468024:639171234567"
		case "setStatus":
			body = "ACCESS_READY"
		case "getStatus":
			body = "STATUS_OK:123456"
		case "finishActivation":
			statusCode = http.StatusNoContent
		case "cancelActivation":
			statusCode = http.StatusNoContent
		default:
			body = `{"data":{"wa":{"4":{"map":{"0.1500":22598}}}}}`
		}
		return &http.Response{StatusCode: statusCode, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), Request: request}, nil
	})})
	offers, err := provider.ListOffers(context.Background(), "PH", "whatsapp")
	if err != nil {
		t.Fatalf("list offers: %v", err)
	}
	if len(offers) != 1 || offers[0].Price != 0.15 || offers[0].AvailableCount != 22598 {
		t.Fatalf("unexpected offers: %#v", offers)
	}
	activation, err := provider.AcquireNumber(context.Background(), AcquireInput{CountryISO2: "PH", Offer: offers[0]})
	if err != nil {
		t.Fatalf("acquire number: %v", err)
	}
	if activation.PhoneE164 != "+639171234567" || activation.ActivationID != "635468024" {
		t.Fatalf("unexpected activation: %#v", activation)
	}
	if err := provider.MarkReady(context.Background(), activation.ActivationID); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	status, err := provider.PollCode(context.Background(), activation.ActivationID)
	if err != nil {
		t.Fatalf("poll code: %v", err)
	}
	if status.Status != "STATUS_OK" || status.Code != "123456" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if err := provider.Complete(context.Background(), activation.ActivationID); err != nil {
		t.Fatalf("complete activation: %v", err)
	}
	if len(requests) < 7 {
		t.Fatalf("request count=%d, want lifecycle and offers calls", len(requests))
	}
	var offersRequest *http.Request
	var numberRequest *http.Request
	for _, request := range requests {
		if request.URL.Path == "/api/v1/activations/offers" {
			offersRequest = request
		}
		if request.URL.Query().Get("action") == "getNumber" {
			numberRequest = request
		}
	}
	if offersRequest == nil || offersRequest.Header.Get("Authorization") != "ApiKey test-key" || offersRequest.URL.Query().Get("services") != "wa" || offersRequest.URL.Query().Get("countries") != "4" {
		t.Fatalf("unexpected offers request: %#v", offersRequest)
	}
	if numberRequest == nil || numberRequest.URL.Query().Get("fixedPrice") != "true" {
		t.Fatalf("unexpected getNumber request: %#v", numberRequest)
	}
}

func TestHeroSMSProviderListsVisibleCountriesResolvedToSupportedISO2(t *testing.T) {
	provider := NewHeroSMSProviderWithClient("test-key", &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		body := `{"4":{"id":4,"eng":"Philippines","chn":"菲律宾","visible":1},"187":{"id":187,"eng":"USA","chn":"美国","visible":1},"74":{"id":74,"eng":"China","chn":"中国","visible":1},"99":{"id":99,"eng":"Philippines","chn":"菲律宾","visible":0}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), Request: request}, nil
	})})
	countries, err := provider.ListCountries(context.Background())
	if err != nil {
		t.Fatalf("list countries: %v", err)
	}
	byISO2 := map[string]string{}
	for _, country := range countries {
		byISO2[country.CountryISO2] = country.Name
	}
	if byISO2["PH"] != "菲律宾" || byISO2["US"] != "美国" {
		t.Fatalf("unexpected resolved countries: %#v", countries)
	}
	if _, found := byISO2["CN"]; found {
		t.Fatalf("country outside the 1024proxy catalogue must not be returned: %#v", countries)
	}
	if len(byISO2) != 2 {
		t.Fatalf("visible country count=%d, want 2: %#v", len(byISO2), countries)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
