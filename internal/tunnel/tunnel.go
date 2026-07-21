// Package tunnel manages a cloudflared "quick tunnel" subprocess that
// exposes a local port at a random *.trycloudflare.com URL.
package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"time"
)

var trycloudflareRe = regexp.MustCompile(`https://[a-zA-Z0-9.-]+\.trycloudflare\.com`)

// ParseURL scans a single line of cloudflared output for a quick-tunnel URL.
func ParseURL(line string) (string, bool) {
	m := trycloudflareRe.FindString(line)
	return m, m != ""
}

// Tunnel represents a running cloudflared quick tunnel subprocess.
type Tunnel struct {
	URL string

	cmd      *exec.Cmd
	waitErr  error
	waitDone chan struct{}
}

// Start launches `cloudflared tunnel --url http://localhost:<port>` and waits
// up to waitTimeout for the public tunnel URL to appear in its combined
// stdout/stderr output.
//
// The subprocess is killed automatically when ctx is canceled (this is
// exec.CommandContext's default behavior) — callers don't need a separate
// Stop method, just cancel ctx and optionally call Wait to block until the
// process has actually exited. Killing outright rather than sending SIGTERM
// first keeps this portable to Windows, where Go can't deliver SIGTERM to an
// arbitrary process; cloudflared has no state to flush on exit, so this is
// safe.
func Start(ctx context.Context, cloudflaredPath string, port int, waitTimeout time.Duration) (*Tunnel, error) {
	cmd := exec.CommandContext(ctx, cloudflaredPath, "tunnel", "--url", fmt.Sprintf("http://localhost:%d", port))

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting cloudflared: %w", err)
	}

	tun := &Tunnel{cmd: cmd, waitDone: make(chan struct{})}

	// Reap the process exactly once, no matter which branch of the select
	// below fires (URL found, timeout, or context cancellation). This is
	// also what stops the scanning goroutine below from leaking: closing
	// pw is what makes its pr.Read (inside bufio.Scanner.Scan) return EOF.
	// exec.Cmd never closes a caller-supplied Stdout/Stderr writer itself —
	// it only closes the OS-level pipe it copies into that writer — so
	// without this explicit Close, killing the subprocess would not be
	// enough to unblock the scanner; it would still block on pr.Read
	// forever. Waiting for cmd.Wait to return before closing pw is also
	// what makes this safe: Wait only returns after exec.Cmd's internal
	// copy-to-pw goroutine has finished, so every line cloudflared wrote
	// has already reached pw by the time we close it.
	go func() {
		tun.waitErr = cmd.Wait()
		_ = pw.Close()
		close(tun.waitDone)
	}()

	urlCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			if url, ok := ParseURL(sc.Text()); ok {
				select {
				case urlCh <- url:
				default:
				}
			}
		}
	}()

	select {
	case url := <-urlCh:
		tun.URL = url
		return tun, nil
	case <-time.After(waitTimeout):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("timed out after %s waiting for cloudflared to report a tunnel URL", waitTimeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Wait blocks until the cloudflared subprocess exits.
func (t *Tunnel) Wait() error {
	<-t.waitDone
	return t.waitErr
}
