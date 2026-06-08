package app

import (
	"context"
	"regexp"
	"strings"
	"time"

	waappv1 "github.com/byte-v-forge/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var nativeUserAgentPattern = regexp.MustCompile(`^WhatsApp/([^ ]+) Android/([^ ]+) Device/([^- ]+)-(.+)$`)

func (s *Server) attachClientProfileRuntime(ctx context.Context, profile *waappv1.ClientProfile) *waappv1.ClientProfile {
	if profile == nil {
		return nil
	}
	state, err := s.store.GetNativeState(ctx, profile.GetClientProfileId())
	if err != nil {
		return profile
	}
	profile.DeviceFingerprint = deviceFingerprintFromState(state)
	return profile
}

func (s *Server) attachClientProfilesRuntime(ctx context.Context, profiles []*waappv1.ClientProfile) []*waappv1.ClientProfile {
	for _, profile := range profiles {
		s.attachClientProfileRuntime(ctx, profile)
	}
	return profiles
}

func deviceFingerprintFromState(state nativeState) *waappv1.DeviceFingerprint {
	profile := state.Profile
	userAgent := firstNonEmpty(profile.UserAgent, state.UserAgent)
	appVersion, androidVersion, vendor, model := parseNativeUserAgent(userAgent)
	fields := profile.AdditionalMapFields
	createdAt := profile.CreatedAtUnix
	if createdAt == 0 {
		createdAt = state.CreatedAtUnix
	}
	return &waappv1.DeviceFingerprint{
		FingerprintId:     "wafp_" + stableID(strings.Join([]string{profile.FDID, userAgent, profile.PhoneSHA256}, ":")),
		UserAgent:         userAgent,
		AppVersion:        appVersion,
		DeviceVendor:      vendor,
		DeviceModel:       model,
		AndroidVersion:    androidVersion,
		Fdid:              profile.FDID,
		PhoneSha256Prefix: prefixRunes(profile.PhoneSHA256, 12),
		DeviceRamGib:      fields["device_ram"],
		Mcc:               fields["mcc"],
		Mnc:               fields["mnc"],
		SimMcc:            fields["sim_mcc"],
		SimMnc:            fields["sim_mnc"],
		NetworkRadioType:  fields["network_radio_type"],
		CreatedAt:         unixTimestamp(createdAt),
	}
}

func parseNativeUserAgent(userAgent string) (string, string, string, string) {
	match := nativeUserAgentPattern.FindStringSubmatch(strings.TrimSpace(userAgent))
	if len(match) != 5 {
		return "", "", "", ""
	}
	return match[1], match[2], match[3], match[4]
}

func unixTimestamp(value int64) *timestamppb.Timestamp {
	if value <= 0 {
		return nil
	}
	return timestamppb.New(time.Unix(value, 0).UTC())
}

func prefixRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}
