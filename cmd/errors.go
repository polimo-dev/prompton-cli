package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/polimo-dev/prompton-cli/internal/api"
)

// Exit codes. Anything the user could fix by retyping the command is 2;
// anything the server or the network decided is 1.
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// exitError carries an explicit process exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usagef builds an exit-2 error: the invocation was wrong.
func usagef(format string, args ...any) error {
	return &exitError{code: ExitUsage, err: fmt.Errorf(format, args...)}
}

// failf builds an exit-1 error: the request was well-formed but did not work.
func failf(format string, args ...any) error {
	return &exitError{code: ExitError, err: fmt.Errorf(format, args...)}
}

// codeFor maps an error to a process exit code.
func codeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	if looksLikeUsage(err) {
		return ExitUsage
	}
	return ExitError
}

// looksLikeUsage recognises the mistakes cobra reports as plain errors:
// unknown commands, unknown flags, and bad flag values.
func looksLikeUsage(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command",
		"unknown flag",
		"unknown shorthand flag",
		"invalid argument",
		"flag needs an argument",
		"bad flag syntax",
		"required flag",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// writeError renders a failure. Under --json it is a JSON envelope on stderr,
// the same shape the API uses, so a calling agent parses one format for both
// transport and CLI failures.
func writeError(w io.Writer, err error, asJSON bool) {
	if err == nil {
		return
	}
	if !asJSON {
		fmt.Fprintln(w, "Error: "+err.Error())
		if apiErr, ok := api.AsError(unwrapAPI(err)); ok {
			if hint := apiErr.Hint(); hint != "" {
				fmt.Fprintln(w, hint)
			}
		}
		return
	}

	env := struct {
		Error struct {
			Code    string                     `json:"code"`
			Message string                     `json:"message"`
			Status  int                        `json:"status,omitempty"`
			Details map[string]json.RawMessage `json:"details,omitempty"`
		} `json:"error"`
	}{}
	env.Error.Code = "cli_error"
	env.Error.Message = err.Error()
	if apiErr, ok := api.AsError(unwrapAPI(err)); ok {
		env.Error.Code = apiErr.Code
		env.Error.Message = apiErr.Message
		env.Error.Status = apiErr.Status
		env.Error.Details = apiErr.Details
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}

func unwrapAPI(err error) error {
	for err != nil {
		if _, ok := api.AsError(err); ok {
			return err
		}
		err = errors.Unwrap(err)
	}
	return nil
}
