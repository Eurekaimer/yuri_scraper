package transport

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// HTTPError retains the status and server retry guidance for failed requests.
type HTTPError struct {
	URL        string
	StatusCode int
	Delay      time.Duration
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d %s", e.URL, e.StatusCode, http.StatusText(e.StatusCode))
}
func (e *HTTPError) Retryable() bool {
	return e.StatusCode == http.StatusRequestTimeout || e.StatusCode == http.StatusTooManyRequests || e.StatusCode == http.StatusInternalServerError || e.StatusCode == http.StatusBadGateway || e.StatusCode == http.StatusServiceUnavailable || e.StatusCode == http.StatusGatewayTimeout
}
func (e *HTTPError) RetryAfter() time.Duration { return e.Delay }

// PermanentError identifies invalid requests, authentication and parser failures.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string   { return e.Err.Error() }
func (e *PermanentError) Unwrap() error   { return e.Err }
func (e *PermanentError) Retryable() bool { return false }

// CanRetry is deliberately conservative: unknown parser errors are not retried.
func CanRetry(err error) bool {
	var guidance interface{ Retryable() bool }
	if errors.As(err, &guidance) {
		return guidance.Retryable()
	}
	var network net.Error
	return errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

func RetryAfter(err error) time.Duration {
	var guidance interface{ RetryAfter() time.Duration }
	if errors.As(err, &guidance) {
		return max(guidance.RetryAfter(), 0)
	}
	return 0
}
