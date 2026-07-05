package app

import (
	"github.com/byte-v-forge/wa-app/internal/waapp/shared"
	"github.com/byte-v-forge/wa-app/internal/waapp/wacore"
)

func nativeIntegrityModeFromPayload(payload map[string]any) wacore.IntegrityMode {
	mode := shared.FirstNonEmpty(
		textField(payload, "integrity_mode"),
		textField(payload, "integrityMode"),
		textField(payload, "gpia_mode"),
		textField(payload, "play_integrity_mode"),
		textField(objectField(payload, "registration"), "integrity_mode"),
	)
	return wacore.NormalizeIntegrityMode(mode)
}
