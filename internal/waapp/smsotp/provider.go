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
	Price              float64
	Currency           string
}

type ActivationStatus struct {
	Status string
	Code   string
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

var ErrNotConfigured = errors.New("sms provider is not configured")
