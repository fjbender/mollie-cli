package cmd

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fjbender/mollie-cli/internal/webhookserver"
)

func TestResolveSubscriptionAction_FreeSlotCreatesFresh(t *testing.T) {
	cases := [][]whWebhook{
		nil,
		{{ID: "wh_1"}},
	}
	for _, existing := range cases {
		got := resolveSubscriptionAction(existing, "")
		if got.Kind != actionCreateFresh {
			t.Errorf("with %d existing subscriptions: Kind = %v, want actionCreateFresh", len(existing), got.Kind)
		}
	}
}

func TestResolveSubscriptionAction_FullSlotsRecreatesOwned(t *testing.T) {
	existing := []whWebhook{
		{ID: "wh_1", Name: "mollie-cli webhook-tunnel"},
		{ID: "wh_2", Name: "Someone else's backend"},
	}

	got := resolveSubscriptionAction(existing, "wh_1")

	if got.Kind != actionRecreateOwned {
		t.Fatalf("Kind = %v, want actionRecreateOwned", got.Kind)
	}
	if got.Existing == nil || got.Existing.ID != "wh_1" {
		t.Errorf("Existing = %+v, want the subscription with ID wh_1", got.Existing)
	}
}

func TestResolveSubscriptionAction_FullSlotsNoOwnedMatchPicksForeign(t *testing.T) {
	existing := []whWebhook{
		{ID: "wh_1", Name: "Someone's backend"},
		{ID: "wh_2", Name: "Someone else's backend"},
	}

	got := resolveSubscriptionAction(existing, "")
	if got.Kind != actionPickForeign {
		t.Errorf("Kind = %v, want actionPickForeign", got.Kind)
	}

	got = resolveSubscriptionAction(existing, "wh_does_not_exist")
	if got.Kind != actionPickForeign {
		t.Errorf("with an owned ID that matches neither subscription: Kind = %v, want actionPickForeign", got.Kind)
	}
}

func TestRetryDelay_LinearIncreaseSummingTo45Seconds(t *testing.T) {
	var total time.Duration
	for attempt := 1; attempt <= 9; attempt++ {
		got := retryDelay(attempt)
		want := time.Duration(attempt) * time.Second
		if got != want {
			t.Errorf("retryDelay(%d) = %s, want %s", attempt, got, want)
		}
		total += got
	}
	if total != 45*time.Second {
		t.Errorf("sum of retryDelay(1..9) = %s, want 45s", total)
	}
}

func TestRetryWithBackoff_SucceedsWithoutRetryingOnFirstTry(t *testing.T) {
	calls := 0
	noDelay := func(int) time.Duration { return 0 }

	err := retryWithBackoff(context.Background(), 10, noDelay, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestRetryWithBackoff_RetriesUntilSuccessWithinAttempts(t *testing.T) {
	calls := 0
	noDelay := func(int) time.Duration { return 0 }

	err := retryWithBackoff(context.Background(), 10, noDelay, func() error {
		calls++
		if calls < 4 {
			return errors.New("not ready yet")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 4 {
		t.Errorf("calls = %d, want 4", calls)
	}
}

func TestRetryWithBackoff_GivesUpAfterAllAttemptsAndReturnsLastError(t *testing.T) {
	calls := 0
	noDelay := func(int) time.Duration { return 0 }
	wantErr := errors.New("still not resolvable")

	err := retryWithBackoff(context.Background(), 3, noDelay, func() error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 (all attempts used)", calls)
	}
}

func TestRetryWithBackoff_StopsImmediatelyWhenContextCanceledDuringWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	delayThenCancel := func(int) time.Duration {
		cancel()
		return time.Hour // would hang forever if ctx.Done() weren't checked
	}

	start := time.Now()
	err := retryWithBackoff(ctx, 10, delayThenCancel, func() error {
		calls++
		return errors.New("always fails")
	})
	elapsed := time.Since(start)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (should stop after the first failed attempt once canceled)", calls)
	}
	if elapsed > time.Second {
		t.Errorf("took %s, want it to return promptly once ctx is canceled instead of waiting out the delay", elapsed)
	}
}

func TestAppendEventLog_WritesTimestampMethodURLHeadersAndBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhook-log")

	ev := webhookserver.Event{
		ReceivedAt: time.Date(2026, 8, 6, 13, 45, 0, 0, time.UTC),
		Method:     http.MethodPost,
		URL:        "/webhook",
		Host:       "abcd1234.trycloudflare.com",
		Header:     http.Header{"Cf-Ray": {"abc123"}, "Content-Type": {"application/json"}},
		Body:       []byte(`{"id":"event_1"}`),
	}

	if err := appendEventLog(path, ev); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	got := string(data)

	for _, want := range []string{
		"2026-08-06T13:45:00Z",
		"POST /webhook HTTP/1.1",
		"Host: abcd1234.trycloudflare.com",
		"Cf-Ray: abc123",
		"Content-Type: application/json",
		`{"id":"event_1"}`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log output missing %q, got:\n%s", want, got)
		}
	}
}

func TestAppendEventLog_AppendsRatherThanTruncating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webhook-log")

	first := webhookserver.Event{ReceivedAt: time.Now(), Method: "POST", URL: "/a", Body: []byte("first-body")}
	second := webhookserver.Event{ReceivedAt: time.Now(), Method: "POST", URL: "/b", Body: []byte("second-body")}

	if err := appendEventLog(path, first); err != nil {
		t.Fatalf("unexpected error on first write: %v", err)
	}
	if err := appendEventLog(path, second); err != nil {
		t.Fatalf("unexpected error on second write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log file: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "first-body") || !strings.Contains(got, "second-body") {
		t.Errorf("expected both entries present, got:\n%s", got)
	}
	if strings.Index(got, "first-body") > strings.Index(got, "second-body") {
		t.Error("expected the first entry to appear before the second")
	}
}

func TestCombineEventHandlers_CallsAllInOrder(t *testing.T) {
	var calls []string
	a := func(webhookserver.Event) { calls = append(calls, "a") }
	b := func(webhookserver.Event) { calls = append(calls, "b") }

	combined := combineEventHandlers(a, b)
	combined(webhookserver.Event{})

	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Errorf("calls = %v, want [a b]", calls)
	}
}
