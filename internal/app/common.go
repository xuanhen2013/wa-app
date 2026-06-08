package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type IDGenerator interface{ NewID(prefix string) string }

type RandomIDGenerator struct{}

func (RandomIDGenerator) NewID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return prefix + hex.EncodeToString(b[:])
}

type AppError struct {
	Code      waappv1.WaErrorCode
	Message   string
	Retryable bool
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func NewError(code waappv1.WaErrorCode, message string, retryable bool) *AppError {
	return &AppError{Code: code, Message: message, Retryable: retryable}
}

func ToProtoError(err error) *waappv1.WaError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return &waappv1.WaError{Code: appErr.Code, Message: appErr.Message, Retryable: appErr.Retryable}
	}
	return &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_INTERNAL, Message: "wa-app operation failed", Retryable: false}
}

func errorFromProto(err *waappv1.WaError) *AppError {
	if err == nil || err.GetCode() == waappv1.WaErrorCode_WA_ERROR_CODE_UNSPECIFIED {
		return nil
	}
	return NewError(err.GetCode(), err.GetMessage(), err.GetRetryable())
}

func validateContext(ctx *waappv1.RequestContext) error {
	_ = ctx
	return nil
}

func timestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t.UTC())
}

func timestampOrNow(t time.Time, now time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return timestamppb.New(now.UTC())
	}
	return timestamppb.New(t.UTC())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func redacted(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= 4 {
		return "****"
	}
	return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
}

func phoneCC(phone *waappv1.PhoneTarget) string {
	if phone == nil {
		return ""
	}
	if cc := digitsOnly(phone.GetCountryCallingCode()); cc != "" {
		return cc
	}
	e164 := digitsOnly(phone.GetE164Number())
	national := digitsOnly(phone.GetNationalNumber())
	if e164 != "" && national != "" && strings.HasSuffix(e164, national) {
		return strings.TrimSuffix(e164, national)
	}
	if strings.EqualFold(phone.GetCountryIso2(), "US") && len(e164) == 11 && strings.HasPrefix(e164, "1") {
		return "1"
	}
	return ""
}

func phoneNational(phone *waappv1.PhoneTarget) string {
	if phone == nil {
		return ""
	}
	if national := digitsOnly(phone.GetNationalNumber()); national != "" {
		return national
	}
	e164 := digitsOnly(phone.GetE164Number())
	cc := phoneCC(phone)
	if cc != "" && strings.HasPrefix(e164, cc) {
		return strings.TrimPrefix(e164, cc)
	}
	return e164
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:24]
}

func durationFromProto(value *durationpb.Duration) time.Duration {
	if value == nil {
		return 0
	}
	return value.AsDuration()
}
