package tunnel_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fjbender/mollie-cli/internal/tunnel"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
		ok   bool
	}{
		{
			name: "typical cloudflared log line",
			line: "2026-07-21T10:00:00Z INF |  https://random-words-1234.trycloudflare.com                                     |",
			want: "https://random-words-1234.trycloudflare.com",
			ok:   true,
		},
		{
			name: "unrelated line",
			line: "2026-07-21T10:00:00Z INF Starting tunnel",
			want: "",
			ok:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tunnel.ParseURL(tc.line)
			if ok != tc.ok || got != tc.want {
				t.Errorf("ParseURL(%q) = (%q, %v), want (%q, %v)", tc.line, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// fakeCloudflared writes an executable shell script standing in for the real
// cloudflared binary, so these tests don't depend on it being installed.
func fakeCloudflared(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake cloudflared script requires a POSIX shell")
	}

	path := filepath.Join(t.TempDir(), "fake-cloudflared")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatalf("writing fake cloudflared script: %v", err)
	}
	return path
}

func TestStart_CapturesTunnelURL(t *testing.T) {
	path := fakeCloudflared(t, `
echo "starting tunnel"
echo "your url is: https://fake-words-5678.trycloudflare.com"
sleep 5
`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tun, err := tunnel.Start(ctx, path, 12345, 2*time.Second)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if tun.URL != "https://fake-words-5678.trycloudflare.com" {
		t.Errorf("URL = %q, want the fake trycloudflare URL", tun.URL)
	}
}

func TestStart_TimesOutWithoutURL(t *testing.T) {
	path := fakeCloudflared(t, `sleep 5`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := tunnel.Start(ctx, path, 12345, 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

func TestStart_MissingBinaryReturnsError(t *testing.T) {
	_, err := tunnel.Start(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"), 12345, time.Second)
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}

func TestTunnel_KilledWhenContextCanceled(t *testing.T) {
	path := fakeCloudflared(t, `
echo "your url is: https://fake-words-9999.trycloudflare.com"
exec sleep 30
`)

	ctx, cancel := context.WithCancel(context.Background())

	tun, err := tunnel.Start(ctx, path, 12345, 2*time.Second)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	cancel()

	done := make(chan error, 1)
	go func() { done <- tun.Wait() }()

	select {
	case <-done:
		// process exited, as expected once its context was canceled
	case <-time.After(3 * time.Second):
		t.Fatal("cloudflared subprocess was not killed within 3s of context cancellation")
	}
}
