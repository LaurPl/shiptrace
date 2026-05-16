package shellquote

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuoteRoundTripsThroughSh(t *testing.T) {
	// For each input we run `sh -c 'printf %s <quoted>'` and confirm sh echoes
	// the exact bytes back. If our quoting leaks any shell metacharacter the
	// echo will differ.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	cases := []string{
		"",
		"plain",
		"/usr/local/bin/shiptrace",
		"/path with space/x",
		`/path/with"double"quote`,
		"/path/with'single'quote",
		"/tmp/$(rm -rf ~)",
		"/tmp/`whoami`",
		`/back\slash`,
		"multi\nline",
		"$HOME",
		"*",
		"glob[abc]?",
		// Unicode just to be sure ReplaceAll does not corrupt bytes.
		"naïve/path/ümläut",
	}
	for _, in := range cases {
		quoted := Quote(in)
		// Use printf %s so we don't have to worry about echo's portability.
		cmd := exec.Command("sh", "-c", "printf %s "+quoted)
		out, err := cmd.Output()
		if err != nil {
			t.Errorf("sh -c failed for %q (quoted=%s): %v", in, quoted, err)
			continue
		}
		if string(out) != in {
			t.Errorf("round-trip mismatch:\n  in   = %q\n  out  = %q\n  q    = %s", in, string(out), quoted)
		}
	}
}

func TestQuoteAlwaysProducesASingleToken(t *testing.T) {
	// `sh -c "set -- <quoted>; echo $#"` should print 1 for any input.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	for _, in := range []string{"a b c", "$(echo evil)", "; rm -rf /", "a'b'c"} {
		quoted := Quote(in)
		cmd := exec.Command("sh", "-c", "set -- "+quoted+`; printf %s "$#"`)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("sh -c failed: %v (quoted=%s)", err, quoted)
		}
		if strings.TrimSpace(string(out)) != "1" {
			t.Errorf("%q quoted to %s expanded to %s tokens (want 1)", in, quoted, string(out))
		}
	}
}
