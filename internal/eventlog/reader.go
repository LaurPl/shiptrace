package eventlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/LaurPl/shiptrace/internal/events"
)

// MaxLineBytes is the per-line cap for the JSONL scanner. Events are
// typically <2 KiB; we set a generous 4 MiB ceiling so a malformed file with
// a missing newline can't OOM the ingester before we can repair it. A line
// hitting the cap returns a clear error pointing at the offset.
const MaxLineBytes = 4 << 20

// ScanFile reads path starting at startOffset and invokes fn for every
// well-formed JSON line. fn receives the parsed event and the byte offset of
// the first byte AFTER that line; persisting that offset is how the ingester
// resumes after a crash without re-applying events.
//
// A malformed line returns an error and leaves the offset at the start of the
// bad line so the caller can decide how to recover.
func ScanFile(path string, startOffset int64, fn func(e events.Event, nextOffset int64) error) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return startOffset, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	defer f.Close()

	if startOffset > 0 {
		if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
			return startOffset, fmt.Errorf("eventlog: seek %s @%d: %w", path, startOffset, err)
		}
	}

	r := bufio.NewReader(f)
	offset := startOffset

	for {
		line, err := r.ReadBytes('\n')
		if len(line) > MaxLineBytes {
			// A pathologically long line (likely a corrupted file with no
			// newline for many MB) would otherwise grow bufio's buffer
			// without bound. Refuse with the offset of the bad line so the
			// caller can repair it manually.
			return offset, fmt.Errorf("eventlog: %s @%d: line exceeds %d bytes (file may be corrupted; trim or split it manually)", path, offset, MaxLineBytes)
		}
		if len(line) > 0 {
			// Track only complete lines (terminated by \n). A trailing partial
			// line means the writer is mid-append; we leave the offset at the
			// start of that partial line so the next pass picks it up.
			if line[len(line)-1] != '\n' {
				if errors.Is(err, io.EOF) {
					return offset, nil
				}
				return offset, fmt.Errorf("eventlog: read %s: %w", path, err)
			}
			var e events.Event
			if jerr := json.Unmarshal(line[:len(line)-1], &e); jerr != nil {
				return offset, fmt.Errorf("eventlog: parse %s @%d: %w", path, offset, jerr)
			}
			nextOffset := offset + int64(len(line))
			if cberr := fn(e, nextOffset); cberr != nil {
				return offset, cberr
			}
			offset = nextOffset
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return offset, nil
			}
			return offset, fmt.Errorf("eventlog: read %s: %w", path, err)
		}
	}
}
