package bulkregistration

import (
	"errors"
	"strings"
	"time"
)

const (
	TaskStatusDraft            = "DRAFT"
	TaskStatusRunning          = "RUNNING"
	TaskStatusCancelRequested  = "CANCEL_REQUESTED"
	TaskStatusCanceling        = "CANCELING"
	TaskStatusCompleted        = "COMPLETED"
	TaskStatusPartialCompleted = "PARTIAL_COMPLETED"
	TaskStatusFailed           = "FAILED"
	TaskStatusCanceled         = "CANCELED"
	TaskStatusPaused           = "PAUSED"

	ItemStatusQueued          = "QUEUED"
	ItemStatusAcquiringNumber = "ACQUIRING_NUMBER"
	ItemStatusNumberAcquired  = "NUMBER_ACQUIRED"
	ItemStatusWAProbing       = "WA_PROBING"
	ItemStatusRequestingOTP   = "WA_REQUESTING_OTP"
	ItemStatusWaitingSMS      = "WAITING_SMS"
	ItemStatusSMSReceived     = "SMS_RECEIVED"
	ItemStatusSubmittingOTP   = "SUBMITTING_OTP"
	ItemStatusRegistered      = "REGISTERED"
	ItemStatusCancelingNumber = "CANCELING_NUMBER"
	ItemStatusNumberCanceled  = "NUMBER_CANCELED"
	ItemStatusCancelPending   = "CANCEL_PENDING"
	ItemStatusFailed          = "FAILED"
	ItemStatusCanceled        = "CANCELED"
)

var ErrTaskNotFound = errors.New("bulk registration task not found")

type Offer struct {
	OfferID        string  `json:"offer_id"`
	Provider       string  `json:"provider"`
	CountryISO2    string  `json:"country_iso2"`
	Service        string  `json:"service"`
	Price          float64 `json:"price"`
	Currency       string  `json:"currency"`
	AvailableCount int     `json:"available_count"`
	PriceTier      string  `json:"price_tier"`
	Operator       string  `json:"operator"`
}

type OfferSelection struct {
	OfferID  string  `json:"offer_id"`
	Quantity int     `json:"quantity"`
	MaxPrice float64 `json:"max_price"`
}

type Task struct {
	TaskID            string           `json:"task_id"`
	Status            string           `json:"status"`
	CountryISO2       string           `json:"country_iso2"`
	TargetCount       int              `json:"target_count"`
	Concurrency       int              `json:"concurrency"`
	IntegrityMode     string           `json:"integrity_mode"`
	Selections        []OfferSelection `json:"selections"`
	SuccessCount      int              `json:"success_count"`
	FailedCount       int              `json:"failed_count"`
	CanceledCount     int              `json:"canceled_count"`
	WaitingCount      int              `json:"waiting_count"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
	StartedAt         *time.Time       `json:"started_at,omitempty"`
	FinishedAt        *time.Time       `json:"finished_at,omitempty"`
	CancelRequestedAt *time.Time       `json:"cancel_requested_at,omitempty"`
	LastError         string           `json:"last_error,omitempty"`
}

type Item struct {
	ItemID                string     `json:"item_id"`
	TaskID                string     `json:"task_id"`
	Status                string     `json:"status"`
	Provider              string     `json:"provider"`
	Operator              string     `json:"operator"`
	OfferID               string     `json:"offer_id"`
	Price                 float64    `json:"price"`
	Currency              string     `json:"currency"`
	PhoneMasked           string     `json:"phone_masked"`
	PhoneE164             string     `json:"-"`
	CountryCallingCode    string     `json:"-"`
	CountryISO2           string     `json:"-"`
	ActivationID          string     `json:"-"`
	WAAccountID           string     `json:"wa_account_id,omitempty"`
	VerificationRequestID string     `json:"verification_request_id,omitempty"`
	SMSStatus             string     `json:"sms_status"`
	WAProbeStatus         string     `json:"wa_probe_status"`
	WAVerificationStatus  string     `json:"wa_verification_status"`
	WARegistrationStatus  string     `json:"wa_registration_status"`
	AttemptCount          int        `json:"attempt_count"`
	CancelAttemptCount    int        `json:"cancel_attempt_count"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
	LastError             string     `json:"last_error,omitempty"`
}

type Event struct {
	EventID        string    `json:"event_id"`
	TaskID         string    `json:"task_id"`
	ItemID         string    `json:"item_id"`
	Provider       string    `json:"provider"`
	ActivationID   string    `json:"-"`
	EventType      string    `json:"event_type"`
	ProviderStatus string    `json:"provider_status"`
	WAStatus       string    `json:"wa_status"`
	Message        string    `json:"message"`
	CreatedAt      time.Time `json:"created_at"`
}

type TaskDetail struct {
	Task   *Task   `json:"task,omitempty"`
	Items  []Item  `json:"items"`
	Events []Event `json:"events"`
}

func IsActiveTaskStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case TaskStatusDraft, TaskStatusRunning, TaskStatusCancelRequested, TaskStatusCanceling, TaskStatusPaused:
		return true
	default:
		return false
	}
}

func IsTerminalItemStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case ItemStatusRegistered, ItemStatusFailed, ItemStatusCanceled, ItemStatusNumberCanceled:
		return true
	default:
		return false
	}
}

func IsCancelableItem(item Item) bool {
	return item.ActivationID != "" && item.Status != ItemStatusRegistered && !IsTerminalItemStatus(item.Status)
}

func MaskPhone(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 5 {
		return value
	}
	return value[:3] + strings.Repeat("*", len(value)-5) + value[len(value)-2:]
}
