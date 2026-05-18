package eventlog

import (
	"bufio"
	"bytes"
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
//
// The cap is enforced by bufio.Scanner.Buffer, which refuses to grow past
// MaxLineBytes and returns bufio.ErrTooLong from Scan(). That gives us a
// hard ceiling on allocation *before* the read completes — a bufio.Reader
// alternative would only fault after the buffer had already doubled past
// the limit.
const MaxLineBytes = 4 << 20

// initialScanBuf is the starting size of the scanner's working buffer.
// Most lines are well under 64 KiB; the scanner grows up to MaxLineBytes
// only when a long line demands it.
const initialScanBuf = 64 << 10

// scanCompleteLines is a bufio.SplitFunc that emits only newline-terminated
// lines. A trailing partial line at EOF (i.e. the writer is mid-append) is
// left in the buffer un-consumed so the next ScanFile pass picks it up once
// the writer flushes the newline. Default bufio.ScanLines would emit the
// partial line as the last token, which would silently misparse mid-append
// records.
func scanCompleteLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	// No newline in buffer: either we need more data, or we hit EOF on a
	// partial line. Either way, return (0, nil, nil) — at EOF the scanner
	// stops cleanly; otherwise it reads more bytes.
	return 0, nil, nil
}

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

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, initialScanBuf), MaxLineBytes)
	sc.Split(scanCompleteLines)
	offset := startOffset

	for sc.Scan() {
		line := sc.Bytes()
		var e events.Event
		if jerr := json.Unmarshal(line, &e); jerr != nil {
			return offset, fmt.Errorf("eventlog: parse %s @%d: %w", path, offset, jerr)
		}
		// Scanner stripped the trailing \n; advance includes it.
		nextOffset := offset + int64(len(line)) + 1
		if cberr := fn(e, nextOffset); cberr != nil {
			return offset, cberr
		}
		offset = nextOffset
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return offset, fmt.Errorf("eventlog: %s @%d: line exceeds %d bytes (file may be corrupted; trim or split it manually)", path, offset, MaxLineBytes)
		}
		return offset, fmt.Errorf("eventlog: read %s: %w", path, err)
	}
	return offset, nil
}
