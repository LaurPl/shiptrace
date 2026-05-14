package claudecode

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

// Env vars that override the conservative privacy defaults. Day 3 replaces
// these with config.yaml values; the env-var form stays available as an
// override for ad-hoc runs.
const (
	EnvLogPromptText    = "SHIPTRACE_LOG_PROMPT_TEXT"
	EnvLogToolInputText = "SHIPTRACE_LOG_TOOL_INPUT"
)

// LogPromptText returns true when the user has explicitly opted into
// capturing verbatim prompt text in the eventlog. Default false —
// length+hash only.
func LogPromptText() bool {
	v := os.Getenv(EnvLogPromptText)
	return v == "1" || v == "true"
}

// LogToolInputText is the parallel toggle for tool input payloads.
func LogToolInputText() bool {
	v := os.Getenv(EnvLogToolInputText)
	return v == "1" || v == "true"
}

// HashString returns the SHA-256 hex digest, prefixed "sha256:". Used to
// fingerprint prompts and tool inputs when verbatim logging is disabled.
func HashString(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// HashBytes is the []byte form for tool_input JSON payloads.
func HashBytes(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}
