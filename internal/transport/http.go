package transport

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// BoundedTransport budgets every HTTP request, including redirect hops. A slot
// is held until the response body closes; limits therefore include slow readers.
//
// A token bucket alone enforces the mean rate but releases requests at exactly
// even intervals, which is itself a machine signature. Pacing therefore also
// enforces a randomized minimum spacing between releases: each gap is at least
// the configured interval and at most 1.5x it. Pacing can only make the sequence
// slower than the configured budget, never faster, and consecutive gaps differ.
type BoundedTransport struct {
	base         http.RoundTripper
	limiter      *rate.Limiter
	interval     time.Duration
	extra        time.Duration
	mu           sync.Mutex
	nextRelease  time.Time
	slots        chan struct{}
	maxBodyBytes int64
	validate     func(*url.URL) error
}

func NewBoundedTransport(base http.RoundTripper, maxConcurrency int64, maxRPS float64, maxBodyBytes int64, validate func(*url.URL) error) *BoundedTransport {
	var interval time.Duration
	if maxRPS > 0 {
		// Convert through float64: time.Duration(0.5) is 0 and would divide by zero
		// for sub-1 request-per-second budgets.
		interval = time.Duration(float64(time.Second) / maxRPS)
	}
	return &BoundedTransport{
		base:         base,
		limiter:      rate.NewLimiter(rate.Limit(maxRPS), 1),
		interval:     interval,
		extra:        interval / 2,
		slots:        make(chan struct{}, maxConcurrency),
		maxBodyBytes: maxBodyBytes,
		validate:     validate,
	}
}

func (t *BoundedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.validate != nil {
		if err := t.validate(req.URL); err != nil {
			return nil, err
		}
	}
	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case t.slots <- struct{}{}:
	}
	release := func() { <-t.slots }
	if err := t.limiter.Wait(req.Context()); err != nil {
		release()
		return nil, err
	}
	if err := t.pace(req.Context()); err != nil {
		release()
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		release()
		return nil, err
	}
	if resp.ContentLength > t.maxBodyBytes {
		_ = resp.Body.Close()
		release()
		return nil, &PermanentError{Err: fmt.Errorf("response exceeds %d-byte body limit", t.maxBodyBytes)}
	}
	resp.Body = &boundedBody{body: resp.Body, context: req.Context(), remaining: t.maxBodyBytes, release: release}
	return resp, nil
}

func (t *BoundedTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// pace delays a request so consecutive releases are separated by a randomized
// minimum spacing. It only ever waits longer than the configured interval.
func (t *BoundedTransport) pace(ctx context.Context) error {
	if t.interval <= 0 {
		return nil
	}
	now := time.Now()
	t.mu.Lock()
	if t.nextRelease.IsZero() {
		// The token bucket already permits one immediate request; pacing only
		// spaces out the requests that follow it.
		t.nextRelease = now
		t.mu.Unlock()
		return nil
	}
	earliest := t.nextRelease
	if earliest.Before(now) {
		earliest = now
	}
	release := earliest.Add(t.interval + rand.N(t.extra))
	t.nextRelease = release
	t.mu.Unlock()
	delay := time.Until(release)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type boundedBody struct {
	body      io.ReadCloser
	context   context.Context
	remaining int64
	release   func()
	once      sync.Once
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if err := b.context.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	// One sentinel byte detects chunked bodies larger than the allowed maximum.
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.body.Read(p)
	if int64(n) > b.remaining {
		allowed := int(b.remaining)
		b.remaining = 0
		return allowed, &PermanentError{Err: fmt.Errorf("response exceeds body limit")}
	}
	b.remaining -= int64(n)
	return n, err
}

func (b *boundedBody) Close() error {
	err := b.body.Close()
	b.once.Do(b.release)
	return err
}
