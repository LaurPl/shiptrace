package events

import (
	"regexp"
	"testing"
)

var sessionIDPattern = regexp.MustCompile(`^shp_[a-z2-7]{12}$`)

func TestNewSessionIDFormat(t *testing.T) {
	for i := 0; i < 1000; i++ {
		id := NewSessionID()
		if !sessionIDPattern.MatchString(id) {
			t.Fatalf("id %q does not match expected pattern", id)
		}
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewSessionID()
		if _, dup := seen[id]; dup {
			t.Fatalf("collision after %d iterations: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}
