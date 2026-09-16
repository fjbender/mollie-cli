package tunnel_test

import (
	"context"
	"testing"
	"time"

	"github.com/fjbender/mollie-cli/internal/tunnel"
)

func TestExternalProvider_StartReturnsGivenURL(t *testing.T) {
	p := tunnel.NewExternalProvider("https://webhooks.example.com")

	url, err := p.Start(context.Background(), 8000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://webhooks.example.com" {
		t.Errorf("url = %q, want https://webhooks.example.com", url)
	}
}

func TestExternalProvider_WaitBlocksUntilContextCanceled(t *testing.T) {
	p := tunnel.NewExternalProvider("https://webhooks.example.com")

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := p.Start(ctx, 8000); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()

	select {
	case <-done:
		t.Fatal("Wait returned before the context was canceled")
	case <-time.After(100 * time.Millisecond):
		// still blocked, as expected
	}

	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Wait() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return within 2s of context cancellation")
	}
}
