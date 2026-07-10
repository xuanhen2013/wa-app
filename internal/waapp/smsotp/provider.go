package smsotp

import (
	"context"
	"errors"
)

type Offer struct {
	OfferID        string
	Provider       string
	CountryISO2    string
	Service        string
	Price          float64
	Currency       string
	AvailableCount int
	PriceTier      string
	Operator       string
}

type Activation struct {
	ActivationID       string
	PhoneE164          string
	CountryCallingCode string
	CountryISO2        string
	Operator           string
	Price              float64
	Currency           string
}

type ActivationStatus struct {
	Status string
	Code   string
}

// Country is a supplier country that has been resolved to an ISO2 code. The
// supplier's internal country identifier never leaves its provider adapter.
type Country struct {
	CountryISO2 string `json:"country_iso2"`
	Name        string `json:"name"`
}

type AcquireInput struct {
	CountryISO2 string
	Offer       Offer
}

type Provider interface {
	Name() string
	ListOffers(context.Context, string, string) ([]Offer, error)
	AcquireNumber(context.Context, AcquireInput) (Activation, error)
	MarkReady(context.Context, string) error
	PollCode(context.Context, string) (ActivationStatus, error)
	Complete(context.Context, string) error
	Cancel(context.Context, string) error
}

// CountryLister is optional because not every SMS supplier exposes a country
// catalogue. Bulk registration requires it before it offers country choices.
type CountryLister interface {
	ListCountries(context.Context) ([]Country, error)
}

var ErrNotConfigured = errors.New("sms provider is not configured")
