package tunnel_test

import (
	"context"
	"testing"
	"time"

	"github.com/fjbender/mollie-cli/internal/tunnel"
)

func TestCloudflaredProvider_StartReturnsTunnelURL(t *testing.T) {
	path := fakeCloudflared(t, `
echo "your url is: https://fake-words-1111.trycloudflare.com"
sleep 5
`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := tunnel.NewCloudflaredProvider(path, 2*time.Second)
	url, err := p.Start(ctx, 12345)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if url != "https://fake-words-1111.trycloudflare.com" {
		t.Errorf("url = %q, want the fake trycloudflare URL", url)
	}
}

func TestCloudflaredProvider_WaitBlocksUntilContextCanceled(t *testing.T) {
	path := fakeCloudflared(t, `
echo "your url is: https://fake-words-2222.trycloudflare.com"
exec sleep 30
`)

	ctx, cancel := context.WithCancel(context.Background())

	p := tunnel.NewCloudflaredProvider(path, 2*time.Second)
	if _, err := p.Start(ctx, 12345); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()

	done := make(chan error, 1)
	go func() { done <- p.Wait() }()

	select {
	case <-done:
		// process exited, as expected once its context was canceled
	case <-time.After(3 * time.Second):
		t.Fatal("Wait did not return within 3s of context cancellation")
	}
}
