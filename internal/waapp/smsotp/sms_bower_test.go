package smsotp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSMSBowerProviderUsesV3OffersAndCompatibleLifecycle(t *testing.T) {
	requests := []*http.Request{}
	provider := NewSMSBowerProviderWithClient("test-key", &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Clone(request.Context()))
		body := ""
		switch request.URL.Query().Get("action") {
		case "getServicesList":
			body = `{"status":"success","services":[{"code":"wa","name":"WhatsApp"}]}`
		case "getCountries":
			body = `{"4":{"id":4,"eng":"Philippines","chn":"菲律宾"}}`
		case "getPricesV3":
			body = `{"4":{"wa":{"3237":{"count":470,"price":0.17,"provider_id":3237}}}}`
		case "getNumberV2":
			body = `{"activationId":"635468024","phoneNumber":"639171234567","activationCost":0.17,"activationOperator":"smart"}`
		case "getStatus":
			body = "STATUS_OK:123456"
		case "setStatus":
			switch request.URL.Query().Get("status") {
			case "1":
				body = "ACCESS_READY"
			case "6":
				body = "ACCESS_ACTIVATION"
			case "8":
				body = "ACCESS_CANCEL"
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(body)), Request: request}, nil
	})})
	countries, err := provider.ListCountries(context.Background())
	if err != nil || len(countries) != 1 || countries[0].CountryISO2 != "PH" {
		t.Fatalf("unexpected SMSBower countries: %#v err=%v", countries, err)
	}
	offers, err := provider.ListOffers(context.Background(), "PH", "whatsapp")
	if err != nil || len(offers) != 1 {
		t.Fatalf("unexpected SMSBower offers: %#v err=%v", offers, err)
	}
	offer := offers[0]
	if offer.Provider != smsBowerName || offer.Price != 0.17 || offer.AvailableCount != 470 || offer.Operator != "channel:3237" {
		t.Fatalf("unexpected SMSBower offer: %#v", offer)
	}
	activation, err := provider.AcquireNumber(context.Background(), AcquireInput{CountryISO2: "PH", Offer: offer})
	if err != nil {
		t.Fatalf("acquire number: %v", err)
	}
	if activation.ActivationID != "635468024" || activation.PhoneE164 != "+639171234567" || activation.Operator != "smart" || activation.Price != 0.17 {
		t.Fatalf("unexpected SMSBower activation: %#v", activation)
	}
	if err := provider.MarkReady(context.Background(), activation.ActivationID); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	status, err := provider.PollCode(context.Background(), activation.ActivationID)
	if err != nil || status.Status != "STATUS_OK" || status.Code != "123456" {
		t.Fatalf("unexpected SMSBower status: %#v err=%v", status, err)
	}
	if err := provider.Complete(context.Background(), activation.ActivationID); err != nil {
		t.Fatalf("complete activation: %v", err)
	}
	if err := provider.Cancel(context.Background(), activation.ActivationID); err != nil {
		t.Fatalf("cancel activation: %v", err)
	}
	var numberRequest *http.Request
	for _, request := range requests {
		if request.URL.Query().Get("action") == "getNumberV2" {
			numberRequest = request
			break
		}
	}
	if numberRequest == nil || numberRequest.URL.Query().Get("providerIds") != "3237" || numberRequest.URL.Query().Get("maxPrice") != "0.17" || numberRequest.URL.Query().Get("minPrice") != "0.17" {
		t.Fatalf("unexpected SMSBower number request: %#v", numberRequest)
	}
}

func TestSMSBowerProviderReturnsActivationAboveSelectedPriceForCancellation(t *testing.T) {
	provider := NewSMSBowerProviderWithClient("test-key", &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Query().Get("action") {
		case "getServicesList":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"services":[{"code":"wa","name":"WhatsApp"}]}`)), Request: request}, nil
		case "getCountries":
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"4":{"id":4,"eng":"Philippines"}}`)), Request: request}, nil
		default:
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString(`{"activationId":"activation","phoneNumber":"639171234567","activationCost":0.18}`)), Request: request}, nil
		}
	})})
	activation, err := provider.AcquireNumber(context.Background(), AcquireInput{CountryISO2: "PH", Offer: Offer{OfferID: "smsbower:PH:wa:provider:3237:price:0.17", Provider: smsBowerName, Price: 0.17, Currency: "USD"}})
	if err != nil || activation.ActivationID != "activation" || activation.Price != 0.18 {
		t.Fatalf("expected activation to be returned for cancellation, activation=%#v err=%v", activation, err)
	}
}

func TestSMSBowerProviderRedactsTransportRequestDetails(t *testing.T) {
	provider := NewSMSBowerProviderWithClient("test-key", &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Get https://smsbower.page/stubs/handler_api.php?action=getCountries&api_key=test-key: EOF")
	})})
	_, err := provider.ListCountries(context.Background())
	if err == nil || err.Error() != "SMS provider request failed due to a network error" {
		t.Fatalf("unexpected transport error: %v", err)
	}
	if strings.Contains(err.Error(), "test-key") || strings.Contains(err.Error(), "api_key") || strings.Contains(err.Error(), "smsbower.page") {
		t.Fatalf("transport error exposed request details: %v", err)
	}
}
