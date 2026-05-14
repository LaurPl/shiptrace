package display

import (
	"testing"
	"time"
)

func TestRelative(t *testing.T) {
	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"under a second", 200 * time.Millisecond, "just now"},
		{"future clamps to just now", -5 * time.Second, "just now"},
		{"seconds", 15 * time.Second, "15s ago"},
		{"minutes", 7 * time.Minute, "7m ago"},
		{"hours", 3 * time.Hour, "3h ago"},
		{"days", 5 * 24 * time.Hour, "5d ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Relative(now.Add(-c.ago), now)
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{2 * time.Minute, "2m"},
		{2*time.Minute + 15*time.Second, "2m 15s"},
		{3 * time.Hour, "3h"},
		{3*time.Hour + 25*time.Minute, "3h 25m"},
	}
	for _, c := range cases {
		got := Duration(c.d)
		if got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
