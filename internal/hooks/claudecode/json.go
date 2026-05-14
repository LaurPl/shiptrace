package claudecode

import "encoding/json"

// jsonUnmarshalSafely wraps json.Unmarshal so callers can use it as a
// best-effort decoder for partially-known shapes. Errors are returned for
// the caller to ignore.
func jsonUnmarshalSafely(data json.RawMessage, dst any) error {
	return json.Unmarshal(data, dst)
}
