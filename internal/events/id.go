package events

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// NewSessionID returns a shiptrace-namespaced session ID of the form
// "shp_<12-char-lowercase-base32>". The randomness source is crypto/rand.
//
// Why 12 chars of base32 (60 bits): well under birthday-collision risk for
// any realistic shiptrace install (10^9 sessions before 1% collision odds),
// and short enough to print in CLI feedback lines without wrapping.
func NewSessionID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure on a normal OS is panic-worthy; there is no
		// graceful degradation that preserves the uniqueness contract.
		panic("shiptrace: crypto/rand failed: " + err.Error())
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	return "shp_" + strings.ToLower(enc)[:12]
}
