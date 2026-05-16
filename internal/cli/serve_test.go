package cli

import "testing"

func TestIsLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:7777", true},
		{"localhost:7777", true},
		{"[::1]:7777", true},
		{":7777", true}, // bare port; cobra default leaves the host empty in some forms
		{"0.0.0.0:7777", false},
		{"192.168.1.10:7777", false},
		{"public.example.com:7777", false},
		{"10.0.0.1:80", false},
	}
	for _, tc := range cases {
		got := isLoopbackAddr(tc.addr)
		if got != tc.want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestServeRefusesPublicBindWithoutOptIn(t *testing.T) {
	cmd := NewRootCommand(nil, nil)
	cmd.SetArgs([]string{"serve", "--addr", "0.0.0.0:7777"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when binding 0.0.0.0 without --listen-public")
	}
	if !contains(err.Error(), "refusing to bind") || !contains(err.Error(), "--listen-public") {
		t.Errorf("unexpected error: %v", err)
	}
}

// contains is a tiny helper that mirrors strings.Contains; using it inline
// keeps this test self-contained without dragging in extra imports.
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
