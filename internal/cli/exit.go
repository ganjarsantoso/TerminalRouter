package cli

import "errors"

// Exit codes from PRD §4.4.
const (
	ExitOK            = 0
	ExitGeneral       = 1
	ExitInvalidConfig = 2
	ExitAuth          = 3
	ExitUnavailable   = 4
	ExitConflict      = 5
	ExitPartial       = 6
	ExitNotFound      = 7
)

// exitError carries a process exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

// Exit wraps an error with a specific exit code.
func Exit(code int, err error) error {
	if err == nil {
		return nil
	}
	return &exitError{code: code, err: err}
}

// ExitCode extracts the process exit code from an error.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return ExitGeneral
}
