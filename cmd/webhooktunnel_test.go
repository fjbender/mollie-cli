package cmd

import (
	"context"
	"errors"
	"testing"
	"time"
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
