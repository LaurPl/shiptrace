package claudecode

import (
	"github.com/LaurPl/shiptrace/internal/session"
)

// projectPointerPath / readActivePointer are thin wrappers around the
// session package, kept in this file so the handler tests can avoid
// importing it directly and end up with a tidy single import.
func projectPointerPath(home, cwd string) (string, error) {
	return session.ProjectPointerPath(home, cwd)
}

func readActivePointer(path string) (*session.ActivePointer, error) {
	p, err := session.ReadActive(path)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errPointerMissing
	}
	return p, nil
}

var errPointerMissing = pointerMissing{}

type pointerMissing struct{}

func (pointerMissing) Error() string { return "pointer missing" }
