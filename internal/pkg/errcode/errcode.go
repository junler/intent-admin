// Package errcode defines the application error codes and a rich error type
// carrying them. Business failures are returned to clients inside the response
// envelope (see internal/pkg/response) with HTTP 200, except for the few codes
// that map onto real HTTP status codes so the frontend can react generically.
package errcode

import (
	"errors"
	"fmt"
	"net/http"
)

// Application error codes. These double as the `code` field of the response
// envelope.
const (
	OK            = 200
	BadRequest    = 400
	Unauthorized  = 401
	Forbidden     = 403
	NotFound      = 404
	Conflict      = 409
	StaleData     = 412 // preview no longer matches current data (see plan §5.3)
	RiskExceeded  = 423 // capability risk is above the caller's allowed ceiling
	TooManyReq    = 429
	InternalError = 500
)

// httpStatus maps codes that must surface as real HTTP status codes. Everything
// else is reported with HTTP 200 and discriminated by the envelope `code`.
var httpStatus = map[int]int{
	Unauthorized: http.StatusUnauthorized,
	Forbidden:    http.StatusForbidden,
	StaleData:    http.StatusPreconditionFailed,
	RiskExceeded: http.StatusLocked,
	TooManyReq:   http.StatusTooManyRequests,
}

// HTTPStatus returns the HTTP status code to use for an application code.
func HTTPStatus(code int) int {
	if s, ok := httpStatus[code]; ok {
		return s
	}
	return http.StatusOK
}

// Error is an application error carrying a code, a human readable message and
// an optional machine readable detail payload (serialised into the envelope's
// `data` field so the frontend can act on it, e.g. the risk hint for 423).
type Error struct {
	Code   int
	Msg    string
	Detail any
	cause  error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%d: %s: %v", e.Code, e.Msg, e.cause)
	}
	return fmt.Sprintf("%d: %s", e.Code, e.Msg)
}

func (e *Error) Unwrap() error { return e.cause }

// WithDetail attaches a machine readable payload to the error.
func (e *Error) WithDetail(detail any) *Error {
	e.Detail = detail
	return e
}

// Wrap attaches an underlying cause, kept out of the client-facing message.
func (e *Error) Wrap(err error) *Error {
	e.cause = err
	return e
}

// New builds an error with an explicit code.
func New(code int, msg string) *Error { return &Error{Code: code, Msg: msg} }

// Newf builds an error with an explicit code and a formatted message.
func Newf(code int, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// From extracts an *Error from err, or synthesises a 500 for unknown errors.
// The original error is preserved as the cause for logging.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: InternalError, Msg: "服务异常，请稍后重试", cause: err}
}

// Constructors for the common cases. Keep the messages user-facing and Chinese;
// callers add detail via Wrap/WithDetail.

func BadRequestf(format string, args ...any) *Error   { return Newf(BadRequest, format, args...) }
func Unauthorizedf(format string, args ...any) *Error { return Newf(Unauthorized, format, args...) }
func Forbiddenf(format string, args ...any) *Error    { return Newf(Forbidden, format, args...) }
func NotFoundf(format string, args ...any) *Error     { return Newf(NotFound, format, args...) }
func Conflictf(format string, args ...any) *Error     { return Newf(Conflict, format, args...) }
func StaleDataf(format string, args ...any) *Error    { return Newf(StaleData, format, args...) }
func Internalf(format string, args ...any) *Error     { return Newf(InternalError, format, args...) }
