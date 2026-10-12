package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func response(body string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Header:        make(http.Header),
	}
}

func request(t *testing.T, target string) *http.Request {
	t.Helper()
	built, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// A transport failure must release its concurrency slot, otherwise one broken
// request would permanently reduce the budget for the rest of the run.
func TestFailedRoundTripReleasesItsSlot(t *testing.T) {
	var calls atomic.Int64
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("dial tcp: connection refused")
		}
		return response("ok"), nil
	})
	bounded := NewBoundedTransport(base, 1, 1000, 1<<20, nil)
	client := &http.Client{Transport: bounded}

	if _, err := client.Do(request(t, "http://example.test/first")); err == nil {
		t.Fatal("transport failure was not reported")
	}
	resp, err := client.Do(request(t, "http://example.test/second"))
	if err != nil {
		t.Fatalf("slot was not released after a transport failure: %v", err)
	}
	defer resp.Body.Close()
	if body, err := io.ReadAll(resp.Body); err != nil || string(body) != "ok" {
		t.Fatalf("second request could not proceed: %q, %v", body, err)
	}
}

// A rejected URL must fail before occupying a slot, so an invalid link cannot
// consume the source budget.
func TestRejectedURLDoesNotConsumeASlot(t *testing.T) {
	var reached atomic.Int64
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		reached.Add(1)
		return response("ok"), nil
	})
	validate := func(target *url.URL) error {
		if target.Host != "allowed.test" {
			return &PermanentError{Err: fmt.Errorf("request URL is outside the selected origin")}
		}
		return nil
	}
	bounded := NewBoundedTransport(base, 1, 1000, 1<<20, validate)
	client := &http.Client{Transport: bounded}

	if _, err := client.Do(request(t, "http://blocked.test/page")); err == nil {
		t.Fatal("origin rejection was not reported")
	} else {
		var permanent *PermanentError
		if !errors.As(err, &permanent) || CanRetry(err) {
			t.Fatalf("origin rejection was not permanent: %v", err)
		}
	}
	if reached.Load() != 0 {
		t.Fatal("rejected request still reached the network")
	}
	resp, err := client.Do(request(t, "http://allowed.test/page"))
	if err != nil {
		t.Fatalf("allowed request was blocked by the rejected one: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
}

// A body that exceeds the cap must be cut at the limit and classified permanent,
// including when a single Read would cross the boundary.
func TestBodyLimitStopsOversizedReadsWithPermanentError(t *testing.T) {
	const limit = 32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 4*limit))), ContentLength: -1}, nil
	})
	bounded := NewBoundedTransport(base, 1, 1000, limit, nil)
	client := &http.Client{Transport: bounded}
	resp, err := client.Do(request(t, "http://example.test/large"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	var permanent *PermanentError
	if !errors.As(err, &permanent) || CanRetry(err) {
		t.Fatalf("oversized body was not permanently rejected: %v", err)
	}
}

// Pacing must never let requests go faster than the configured budget, while the
// gaps themselves stay non-uniform so the request sequence is not a fixed clock.
func TestPacingStaysWithinBudgetAndVaries(t *testing.T) {
	const perSecond = 20 // 50 ms average interval
	var started []time.Time
	var mu sync.Mutex
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		mu.Lock()
		started = append(started, time.Now())
		mu.Unlock()
		return response("ok"), nil
	})
	bounded := NewBoundedTransport(base, 1, perSecond, 1<<20, nil)
	client := &http.Client{Transport: bounded}
	for range 8 {
		resp, err := client.Do(request(t, "http://example.test/page"))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	if len(started) < 8 {
		t.Fatalf("not all requests were issued: %d", len(started))
	}
	interval := time.Second / perSecond
	minimum, maximum := time.Duration(1<<62), time.Duration(0)
	for i := 1; i < len(started); i++ {
		gap := started[i].Sub(started[i-1])
		minimum = min(minimum, gap)
		maximum = max(maximum, gap)
	}
	if minimum < interval*4/5 {
		t.Fatalf("pacing exceeded the configured rate: fastest gap %v < %v", minimum, interval)
	}
	if maximum <= minimum {
		t.Fatalf("pacing produced a perfectly uniform clock: %v", minimum)
	}
}

// A fractional budget below one request per second must not panic or disable
// pacing, since Yamibo-style conservative rates are legitimate configurations.
func TestPacingHandlesFractionalRates(t *testing.T) {
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return response("ok"), nil })
	bounded := NewBoundedTransport(base, 1, 0.5, 1<<20, nil)
	if bounded.interval != 2*time.Second || bounded.extra != time.Second {
		t.Fatalf("fractional rate produced pacing %v/%v", bounded.interval, bounded.extra)
	}
}

// A transport configured with a nonsensical rate must not distort pacing.
func TestPacingDisabledWithoutPositiveRate(t *testing.T) {
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return response("ok"), nil })
	bounded := NewBoundedTransport(base, 1, 0, 1<<20, nil)
	if bounded.interval != 0 || bounded.extra != 0 {
		t.Fatalf("zero rate produced pacing %v/%v", bounded.interval, bounded.extra)
	}
}
func TestCanceledRequestReleasesSlot(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/slow" {
			close(entered)
			select {
			case <-release:
			case <-request.Context().Done():
			}
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return response("late"), nil
		}
		return response("ok"), nil
	})
	bounded := NewBoundedTransport(base, 1, 1000, 1<<20, nil)
	client := &http.Client{Transport: bounded}
	ctx, cancel := context.WithCancel(context.Background())
	slow, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test/slow", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := client.Do(slow); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled request reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the waiting request")
	}
	close(release)
	resp, err := client.Do(request(t, "http://example.test/fast"))
	if err != nil {
		t.Fatalf("slot was not released after cancellation: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
}
