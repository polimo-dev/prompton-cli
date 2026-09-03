package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Error codes the server uses. See docs/management-api.md §1.
const (
	CodeUnauthorized  = "unauthorized"
	CodeNotFound      = "not_found"
	CodeInvalidReq    = "invalid_request"
	CodeConflict      = "conflict"
	CodeForbidden     = "forbidden"
	CodeInternalError = "internal_error"

	// Device flow codes (RFC 8628 shaped), returned with HTTP 400.
	CodeAuthorizationPending = "authorization_pending"
	CodeSlowDown             = "slow_down"
	CodeExpiredToken         = "expired_token"
	CodeAccessDenied         = "access_denied"
)

// Error is the decoded {"error": {...}} envelope. Every non-2xx response
// becomes one of these, including responses whose body is not the envelope
// (a proxy's HTML 502, say) — those keep the raw body in Message.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]json.RawMessage
}

func (e *Error) Error() string {
	code := e.Code
	if code == "" {
		code = fmt.Sprintf("http_%d", e.Status)
	}
	if e.Message == "" {
		return code
	}
	return fmt.Sprintf("%s: %s", code, e.Message)
}

// Is reports whether err is an *Error carrying the given code.
func Is(err error, code string) bool {
	e, ok := err.(*Error)
	return ok && e.Code == code
}

// IsStatus reports whether err is an *Error carrying the given HTTP status.
func IsStatus(err error, status int) bool {
	e, ok := err.(*Error)
	return ok && e.Status == status
}

// AsError returns err as an *Error when it is one.
func AsError(err error) (*Error, bool) {
	e, ok := err.(*Error)
	return e, ok
}

// Conflict returns the existing resource carried by a 409. The server puts it
// under a single descriptive key ("project", "use_case", "model", …), so the
// name is returned alongside the JSON for callers that want to print it.
func (e *Error) Conflict() (name string, raw json.RawMessage, ok bool) {
	if e.Code != CodeConflict || len(e.Details) == 0 {
		return "", nil, false
	}
	keys := make([]string, 0, len(e.Details))
	for k := range e.Details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := e.Details[k]
		if len(v) > 0 && v[0] == '{' {
			return k, v, true
		}
	}
	return "", nil, false
}

// Hint renders the machine-readable parts of Details that are worth showing to
// a human: field validation errors, and the "here is what you could have
// picked" lists the API attaches to 404s.
func (e *Error) Hint() string {
	if len(e.Details) == 0 {
		return ""
	}
	var out []string

	if raw, ok := e.Details["errors"]; ok {
		var fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &fields) == nil {
			for _, f := range fields {
				out = append(out, fmt.Sprintf("  %s: %s", f.Field, f.Message))
			}
		}
	}
	for _, key := range []string{"available_prompts", "available_revisions", "available_environments"} {
		raw, ok := e.Details[key]
		if !ok {
			continue
		}
		var vals []any
		if json.Unmarshal(raw, &vals) == nil && len(vals) > 0 {
			parts := make([]string, 0, len(vals))
			for _, v := range vals {
				parts = append(parts, fmt.Sprint(v))
			}
			out = append(out, fmt.Sprintf("  %s: %s", strings.ReplaceAll(key, "_", " "), strings.Join(parts, ", ")))
		}
	}
	return strings.Join(out, "\n")
}
