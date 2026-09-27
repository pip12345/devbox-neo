// Package commanderror carries actionable failures without owning presentation
// or lifecycle policy. Causes remain available to errors.Is/As and exit handling.
package commanderror

type Step struct {
	Command []string `json:"command"`
	Reason  string   `json:"reason"`
}

type Error struct {
	Code    string
	Message string
	Target  string
	Next    []Step
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func New(code, message, target string, cause error, next ...Step) *Error {
	return &Error{Code: code, Message: message, Target: target, Cause: cause, Next: next}
}

func Next(reason string, args ...string) Step {
	return Step{Command: append([]string{"dbx"}, args...), Reason: reason}
}
