package app

import (
	"context"
	"time"
)

type RuntimeState interface {
	Close() error
	ClaimRequest(context.Context, string, time.Duration) (bool, error)
	SaveTransientState(context.Context, string, []byte, time.Duration) error
	GetTransientState(context.Context, string) ([]byte, error)
	DeleteTransientState(context.Context, string) error
	ClaimLease(context.Context, string, string, time.Duration) (bool, error)
	RenewLease(context.Context, string, string, time.Duration) (bool, error)
	ReleaseLease(context.Context, string, string) error
	OpenSessionLease(context.Context, string, time.Duration) error
	CloseSessionLease(context.Context, string) error
}

const (
	accountProbeFlowUnknown           = "unknown"
	accountProbeFlowProbeFailed       = "probe_failed"
	accountProbeFlowRegistered        = "registered"
	accountProbeFlowNotRegistered     = "not_registered"
	accountProbeFlowBlocked           = "blocked"
	accountProbeFlowInvalidNumber     = "invalid_number"
	accountProbeFlowRateLimited       = "rate_limited"
	accountProbeFlowConsentRequired   = "consent_required"
	accountProbeFlowChallengeRequired = "challenge_required"
)
