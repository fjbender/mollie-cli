# Webhook Tunnel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `mollie webhook-tunnel`, a test-mode-only command that tunnels a local HTTP server to the public internet via `cloudflared`, points a Mollie webhook subscription at it, and logs every incoming event live until Ctrl-C.

**Architecture:** Four small, independently-testable `internal/` packages do the real work — `tunnelstate` (crash-recoverable JSON state file), `webhookevents` (permission-aware event type resolution), `tunnel` (cloudflared subprocess management), and `webhookserver` (the receiving HTTP handler + HMAC signature verification). A new `cmd/webhooktunnel.go` orchestrates them: it contains one pure, unit-tested decision function (`resolveSubscriptionAction`) plus the actual API-calling glue code, following this codebase's existing convention that `cmd/*.go` orchestration isn't itself unit tested (see `cmd/webhooks.go`) but the logic it depends on is.

**Tech Stack:** Go stdlib (`net/http`, `os/exec`, `os/signal`, `crypto/hmac`), Cobra, `charmbracelet/huh` (via `internal/prompt`), Mollie Go SDK v0.10.3 (`Permissions` API only — webhook CRUD continues to bypass the SDK via the existing `whClient`, see `docs/superpowers/specs/2026-07-21-webhook-tunnel-design.md`), and the external `cloudflared` binary (not vendored — detected via `PATH`).

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/tunnelstate/tunnelstate.go` | Create | State file read/write; per-environment ownership + pending-restore tracking |
| `internal/tunnelstate/tunnelstate_test.go` | Create | Tests for load/save round-trip and per-env `Get` |
| `internal/webhookevents/events.go` | Create | Static event-type → permission table + `Resolve()` |
| `internal/webhookevents/events_test.go` | Create | Tests for the API-key and access-token resolution paths |
| `internal/tunnel/tunnel.go` | Create | Spawn/monitor the `cloudflared` subprocess, parse its tunnel URL |
| `internal/tunnel/tunnel_test.go` | Create | Tests using a fake `cloudflared` script |
| `internal/webhookserver/handler.go` | Create | Local HTTP handler + `X-Mollie-Signature` verification |
| `internal/webhookserver/handler_test.go` | Create | Tests for verified / unverified / missing-secret cases |
| `internal/prompt/prompt.go` | Modify | Add a generic `Select`/`SelectOption` helper |
| `cmd/webhooktunnel.go` | Create | Command, flags, preflight checks, subscription resolution, full lifecycle |
| `cmd/webhooktunnel_test.go` | Create | Test for the pure `resolveSubscriptionAction` decision function |

---

### Task 1: `internal/tunnelstate` — crash-recoverable state file

**Files:**
- Create: `internal/tunnelstate/tunnelstate.go`
- Test: `internal/tunnelstate/tunnelstate_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/tunnelstate/tunnelstate_test.go
package tunnelstate_test

import (
	"testing"

	"github.com/fjbender/mollie-cli/internal/tunnelstate"
)

func TestLoad_MissingFileReturnsEmptyState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	f, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Environments == nil {
		t.Fatal("expected a non-nil, empty Environments map")
	}
	if len(f.Environments) != 0 {
		t.Errorf("expected no environments, got %d", len(f.Environments))
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	f, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := f.Get("default")
	env.OwnedSubscriptionID = "wh_123"
	env.PendingRestore = &tunnelstate.SubscriptionSnapshot{
		ID:         "wh_456",
		Name:       "My backend",
		URL:        "https://example.com/webhooks",
		EventTypes: []string{"payment-link.paid"},
	}

	if err := tunnelstate.Save(f); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	reloaded, err := tunnelstate.Load()
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	got := reloaded.Get("default")
	if got.OwnedSubscriptionID != "wh_123" {
		t.Errorf("OwnedSubscriptionID = %q, want %q", got.OwnedSubscriptionID, "wh_123")
	}
	if got.PendingRestore == nil || got.PendingRestore.ID != "wh_456" {
		t.Errorf("PendingRestore = %+v, want ID wh_456", got.PendingRestore)
	}
}

func TestFile_Get_CreatesEmptyStateForUnknownEnv(t *testing.T) {
	f := &tunnelstate.File{}

	env := f.Get("staging")
	if env == nil {
		t.Fatal("expected a non-nil EnvState")
	}
	if env.OwnedSubscriptionID != "" || env.PendingRestore != nil {
		t.Errorf("expected zero-value EnvState, got %+v", env)
	}

	// Getting the same name again must return the same instance, so
	// mutations made through one Get() call are visible via another.
	env.OwnedSubscriptionID = "wh_1"
	if f.Get("staging").OwnedSubscriptionID != "wh_1" {
		t.Error("expected Get to return the same EnvState instance on repeat calls")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tunnelstate/...
```

Expected: `no such file or directory` / `cannot find package` — the package doesn't exist yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/tunnelstate/tunnelstate_test.go
git commit -m "test: add failing tests for tunnelstate"
```

- [ ] **Step 4: Implement the package**

```go
// internal/tunnelstate/tunnelstate.go

// Package tunnelstate persists cross-invocation state for `mollie
// webhook-tunnel`: which webhook subscription (if any) this tool currently
// owns, and — when an existing foreign subscription was temporarily
// repointed — what to restore it to if the process is killed before it can
// clean up after itself.
package tunnelstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fjbender/mollie-cli/internal/config"
)

// SubscriptionSnapshot captures a webhook subscription's identity and
// configuration before webhook-tunnel repoints it, so it can be restored.
type SubscriptionSnapshot struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
}

// EnvState is the webhook-tunnel state for a single config environment.
type EnvState struct {
	// OwnedSubscriptionID is the ID of a webhook subscription this tool
	// created in a previous run, so a later run can recognize and safely
	// replace it without asking the user.
	OwnedSubscriptionID string `json:"owned_subscription_id,omitempty"`
	// PendingRestore holds the pre-repoint state of a foreign subscription
	// that was patched to point at a tunnel. It's cleared once restored;
	// if it's non-nil on startup, the previous run didn't shut down cleanly.
	PendingRestore *SubscriptionSnapshot `json:"pending_restore,omitempty"`
}

// File is the on-disk representation of webhook-tunnel state, keyed per
// config environment (mirrors config.File).
type File struct {
	Environments map[string]*EnvState `json:"environments"`
}

// Get returns the EnvState for name, creating and registering an empty one
// if none exists yet. Never returns nil.
func (f *File) Get(name string) *EnvState {
	if f.Environments == nil {
		f.Environments = map[string]*EnvState{}
	}
	if s, ok := f.Environments[name]; ok {
		return s
	}
	s := &EnvState{}
	f.Environments[name] = s
	return s
}

const fileName = "webhook-tunnel-state.json"

func statePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load reads the state file, returning an empty File if it doesn't exist yet.
func Load() (*File, error) {
	p, err := statePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Environments: map[string]*EnvState{}}, nil
		}
		return nil, fmt.Errorf("reading tunnel state file: %w", err)
	}

	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing tunnel state file: %w", err)
	}
	if f.Environments == nil {
		f.Environments = map[string]*EnvState{}
	}
	return &f, nil
}

// Save writes f to the state file, creating the config directory if needed.
func Save(f *File) error {
	p, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing tunnel state: %w", err)
	}
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("writing tunnel state file: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/tunnelstate/...
```

Expected: all 3 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tunnelstate/tunnelstate.go
git commit -m "feat: add tunnelstate package for webhook-tunnel crash recovery"
```

---

### Task 2: `internal/webhookevents` — permission-aware event type resolution

**Files:**
- Create: `internal/webhookevents/events.go`
- Test: `internal/webhookevents/events_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/webhookevents/events_test.go
package webhookevents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"

	"github.com/fjbender/mollie-cli/internal/webhookevents"
)

type fakeLister struct {
	resp   *operations.ListPermissionsResponse
	err    error
	called bool
}

func (f *fakeLister) List(_ context.Context, _ *string, _ ...operations.Option) (*operations.ListPermissionsResponse, error) {
	f.called = true
	return f.resp, f.err
}

func TestResolve_APIKeyReturnsFullListWithoutCallingLister(t *testing.T) {
	lister := &fakeLister{err: errors.New("should never be called")}

	got, err := webhookevents.Resolve(context.Background(), true, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lister.called {
		t.Error("expected Resolve to skip the Permissions API entirely for an API key")
	}
	if len(got) != len(webhookevents.GlobalEventPermissions) {
		t.Errorf("expected all %d global event types, got %d: %v", len(webhookevents.GlobalEventPermissions), len(got), got)
	}
}

func TestResolve_AccessTokenFiltersByGrantedPermissions(t *testing.T) {
	lister := &fakeLister{
		resp: &operations.ListPermissionsResponse{
			Object: &operations.ListPermissionsResponseBody{
				Embedded: operations.ListPermissionsEmbedded{
					Permissions: []components.ListEntityPermission{
						{ID: "payment-links.read", Granted: true},
						{ID: "balances.read", Granted: false},
					},
				},
			},
		},
	}

	got, err := webhookevents.Resolve(context.Background(), false, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"payment-link.paid"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolve_AccessTokenNoGrantedPermissionsReturnsEmpty(t *testing.T) {
	lister := &fakeLister{
		resp: &operations.ListPermissionsResponse{
			Object: &operations.ListPermissionsResponseBody{},
		},
	}

	got, err := webhookevents.Resolve(context.Background(), false, lister)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no event types, got %v", got)
	}
}

func TestResolve_AccessTokenPropagatesListError(t *testing.T) {
	lister := &fakeLister{err: errors.New("boom")}

	_, err := webhookevents.Resolve(context.Background(), false, lister)
	if err == nil {
		t.Fatal("expected an error")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/webhookevents/...
```

Expected: `cannot find package` — the package doesn't exist yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/webhookevents/events_test.go
git commit -m "test: add failing tests for webhookevents"
```

- [ ] **Step 4: Implement the package**

```go
// internal/webhookevents/events.go

// Package webhookevents resolves which Mollie Next-gen Webhook event types a
// credential can subscribe to, without using the "*" wildcard — which
// silently misbehaves for tokens lacking some of the underlying permissions.
package webhookevents

import (
	"context"
	"fmt"
	"sort"

	"github.com/mollie/mollie-api-golang/models/operations"
)

// GlobalEventPermissions maps every documented "global" Next-gen Webhook
// event type to the permission required to receive it. Beta event types
// (dispute.*, file.*, unmatched-credit-transfer.*, connect-balance-transfer.*)
// are deliberately excluded — see the design doc.
var GlobalEventPermissions = map[string]string{
	"payment-link.paid":                        "payment-links.read",
	"balance-transaction.created":               "balances.read",
	"sales-invoice.created":                     "invoices.read",
	"sales-invoice.issued":                      "invoices.read",
	"sales-invoice.canceled":                    "invoices.read",
	"sales-invoice.paid":                        "invoices.read",
	"business-account-transfer.requested":       "business-account-transfers.read",
	"business-account-transfer.initiated":       "business-account-transfers.read",
	"business-account-transfer.pending-review":  "business-account-transfers.read",
	"business-account-transfer.processed":       "business-account-transfers.read",
	"business-account-transfer.failed":          "business-account-transfers.read",
	"business-account-transfer.blocked":         "business-account-transfers.read",
	"business-account-transfer.returned":        "business-account-transfers.read",
	"payout.initiated":                          "payouts.read",
	"payout.completed":                          "payouts.read",
	"payout.processing-at-bank":                 "payouts.read",
	"payout.canceled":                           "payouts.read",
	"payout.failed":                             "payouts.read",
}

// PermissionLister is the subset of the Mollie SDK's Permissions client that
// Resolve needs. The SDK's real client (`(*mollieapi.Client).Permissions`)
// satisfies this interface, so production callers pass it directly; tests
// pass a fake.
type PermissionLister interface {
	List(ctx context.Context, idempotencyKey *string, opts ...operations.Option) (*operations.ListPermissionsResponse, error)
}

// Resolve returns every event type the given credential can subscribe to.
//
// API keys (test_/live_) are fully privileged and the Permissions API
// rejects them outright, so lister is never used when isAPIKey is true —
// nil is a valid argument in that case.
func Resolve(ctx context.Context, isAPIKey bool, lister PermissionLister) ([]string, error) {
	if isAPIKey {
		return allEventTypes(), nil
	}

	resp, err := lister.List(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing permissions: %w", err)
	}

	granted := map[string]bool{}
	if resp.Object != nil {
		for _, p := range resp.Object.Embedded.Permissions {
			if p.Granted {
				granted[p.ID] = true
			}
		}
	}

	var types []string
	for eventType, permission := range GlobalEventPermissions {
		if granted[permission] {
			types = append(types, eventType)
		}
	}
	sort.Strings(types)
	return types, nil
}

func allEventTypes() []string {
	types := make([]string, 0, len(GlobalEventPermissions))
	for t := range GlobalEventPermissions {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/webhookevents/...
```

Expected: all 4 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/webhookevents/events.go
git commit -m "feat: add webhookevents package for permission-aware event resolution"
```

---

### Task 3: `internal/tunnel` — cloudflared subprocess management

**Files:**
- Create: `internal/tunnel/tunnel.go`
- Test: `internal/tunnel/tunnel_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/tunnel/tunnel_test.go
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
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/tunnel/...
```

Expected: `cannot find package` — the package doesn't exist yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/tunnel/tunnel_test.go
git commit -m "test: add failing tests for tunnel"
```

- [ ] **Step 4: Implement the package**

```go
// internal/tunnel/tunnel.go

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
	URL      string
	cmd      *exec.Cmd
	waitDone chan struct{}
	waitErr  error
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

	// exec.Cmd never closes cmd.Stdout/Stderr itself when they're not
	// *os.File — it only closes its own internal copy of the pipe. Since we
	// gave it our io.Pipe writer, nothing closes pw when the process exits,
	// which would otherwise leave the scanning goroutine below blocked on
	// pr.Read() forever. This reaper goroutine calls cmd.Wait() exactly once
	// (safe: it only returns after all output has already reached pw) and
	// closes pw right after, unblocking the scanner. Tunnel.Wait() reads the
	// result from here instead of calling cmd.Wait() a second time, which
	// would otherwise fail with "Wait was already called".
	waitDone := make(chan struct{})
	tun := &Tunnel{cmd: cmd, waitDone: waitDone}
	go func() {
		tun.waitErr = cmd.Wait()
		_ = pw.Close()
		close(waitDone)
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
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/tunnel/...
```

Expected: all 5 tests pass (`TestParseURL` runs 2 subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/tunnel/tunnel.go
git commit -m "feat: add tunnel package for cloudflared subprocess management"
```

---

### Task 4: `internal/webhookserver` — local HTTP handler + signature verification

**Files:**
- Create: `internal/webhookserver/handler.go`
- Test: `internal/webhookserver/handler_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/webhookserver/handler_test.go
package webhookserver_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fjbender/mollie-cli/internal/webhookserver"
)

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHandler_VerifiesCorrectSignature(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1","type":"payment-link.paid","entityId":"pl_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", sign(secret, body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !got.Verified {
		t.Error("expected event to be marked verified")
	}
	if string(got.Body) != body {
		t.Errorf("Body = %q, want %q", got.Body, body)
	}
}

func TestHandler_RejectsWrongSignature(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	called := false
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e; called = true })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", "not-the-right-signature")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (we still ack Mollie even on a bad signature)", rec.Code)
	}
	if !called {
		t.Fatal("expected onEvent to still be called for a logged-but-unverified event")
	}
	if got.Verified {
		t.Error("expected event to be marked unverified")
	}
}

func TestHandler_MissingSignatureHeaderIsUnverified(t *testing.T) {
	const secret = "whsec_test"
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler(secret, func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got.Verified {
		t.Error("expected event with no signature header to be unverified")
	}
}

func TestHandler_EmptySecretAlwaysUnverified(t *testing.T) {
	body := `{"id":"event_1"}`

	var got webhookserver.Event
	handler := webhookserver.Handler("", func(e webhookserver.Event) { got = e })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("X-Mollie-Signature", sign("some-secret-we-dont-have", body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if got.Verified {
		t.Error("expected event to be unverified when no secret is known (foreign-subscription case)")
	}
}

func TestHandler_OversizedBodyRejected(t *testing.T) {
	oversized := strings.Repeat("a", 10<<20+1) // one byte over the 10 MiB cap

	called := false
	handler := webhookserver.Handler("whsec_test", func(webhookserver.Event) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversized))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if called {
		t.Error("expected onEvent not to be called for a rejected oversized body")
	}
}

// errReader is an io.ReadCloser that always fails, simulating a body-read
// error unrelated to size (e.g. a broken connection mid-request).
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("simulated read error") }
func (errReader) Close() error              { return nil }

func TestHandler_BodyReadErrorRejected(t *testing.T) {
	called := false
	handler := webhookserver.Handler("whsec_test", func(webhookserver.Event) { called = true })

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = errReader{}
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if called {
		t.Error("expected onEvent not to be called when the body can't be read")
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
go test ./internal/webhookserver/...
```

Expected: `cannot find package` — the package doesn't exist yet.

- [ ] **Step 3: Commit the failing tests**

```bash
git add internal/webhookserver/handler_test.go
git commit -m "test: add failing tests for webhookserver"
```

- [ ] **Step 4: Implement the package**

```go
// internal/webhookserver/handler.go

// Package webhookserver implements the local HTTP endpoint that receives
// webhook deliveries forwarded by the cloudflared tunnel, verifying Mollie's
// X-Mollie-Signature header when a signing secret is known.
package webhookserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"time"
)

// Event is a single received webhook delivery.
type Event struct {
	ReceivedAt time.Time
	Body       []byte
	Verified   bool
}

// maxBodyBytes caps how much of a request body this handler will read.
// Mollie's webhook payloads are small JSON documents, but the local server
// sits behind a public (if unguessable) tunnel URL — this bounds memory use
// against an oversized POST from anyone who stumbles onto that URL.
const maxBodyBytes = 10 << 20 // 10 MiB

// Handler returns an http.Handler that reads each request's raw body,
// verifies it against secret (skipped entirely when secret is ""), invokes
// onEvent exactly once per request regardless of verification outcome, and
// always responds 200 OK. A body over maxBodyBytes gets 413 instead; any
// other read failure gets 400 — both skip the onEvent call entirely, since
// there's no complete body to report.
func Handler(secret string, onEvent func(Event)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
			}
			return
		}

		verified := secret != "" && verifySignature(secret, body, r.Header.Get("X-Mollie-Signature"))

		onEvent(Event{
			ReceivedAt: time.Now(),
			Body:       body,
			Verified:   verified,
		})

		w.WriteHeader(http.StatusOK)
	})
}

// verifySignature reports whether sig is a valid hex-encoded HMAC-SHA256
// digest of body using secret, compared in constant time.
func verifySignature(secret string, body []byte, sig string) bool {
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/webhookserver/...
```

Expected: all 6 tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/webhookserver/handler.go
git commit -m "feat: add webhookserver package with signature verification"
```

---

### Task 5: Add a generic `Select` prompt helper

**Files:**
- Modify: `internal/prompt/prompt.go`

No test file — like the existing `Confirm`, `APIKey`, and `ProfileSelect` helpers in this file, an interactive `huh` form isn't practical to unit test, and the codebase doesn't attempt to for the others either.

- [ ] **Step 1: Add `Select` and `SelectOption` to the file**

Append to `internal/prompt/prompt.go` (after `ProfileSelect`):

```go
// SelectOption is a display-label / value pair used to populate a generic
// selection list. T must be comparable — this matches the constraint on
// huh.Select[T] itself.
type SelectOption[T comparable] struct {
	Label string
	Value T
}

// Select presents the user with a titled list of options and returns the
// selected value.
func Select[T comparable](title string, options []SelectOption[T]) (T, error) {
	opts := make([]huh.Option[T], 0, len(options))
	for _, o := range options {
		opts = append(opts, huh.NewOption(o.Label, o.Value))
	}

	var selected T
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[T]().
				Title(title).
				Options(opts...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		var zero T
		return zero, err
	}
	return selected, nil
}
```

- [ ] **Step 2: Verify the package builds**

```bash
go build ./internal/prompt/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/prompt/prompt.go
git commit -m "feat: add generic Select prompt helper"
```

---

### Task 6: `cmd/webhooktunnel.go` — command skeleton and preflight checks

**Files:**
- Create: `cmd/webhooktunnel.go`

- [ ] **Step 1: Create the command with flags and preflight checks only**

```go
// cmd/webhooktunnel.go
package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	whtPort       int
	whtEventTypes string
)

var webhookTunnelCmd = &cobra.Command{
	Use:   "webhook-tunnel",
	Short: "Tunnel Mollie test-mode webhook events to your terminal",
	Long: `Spins up a public cloudflared tunnel to a local HTTP server, points a
Mollie test-mode webhook subscription at it, and logs every incoming event
until you press Ctrl-C. Test mode only — live mode is not yet supported.`,
	RunE: runWebhookTunnel,
}

func init() {
	webhookTunnelCmd.Flags().IntVar(&whtPort, "port", 10153, "Local port for the tunnel's HTTP server")
	webhookTunnelCmd.Flags().StringVar(&whtEventTypes, "event-types", "", `Comma-separated event types to subscribe to (default: every event type this credential can access)`)

	rootCmd.AddCommand(webhookTunnelCmd)
}

// cloudflaredInstallHint returns an OS-appropriate suggestion for installing
// cloudflared, since v1 never downloads it automatically.
func cloudflaredInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install it with: brew install cloudflared"
	case "linux":
		return "cloudflared isn't in default OS repos. See https://pkg.cloudflare.com/index.html for apt/yum setup,\nor download a binary directly from https://github.com/cloudflare/cloudflared/releases"
	case "windows":
		return "Install it with: winget install --exact --id Cloudflare.cloudflared\nor download a binary from https://github.com/cloudflare/cloudflared/releases"
	default:
		return "Download a binary for your platform from https://github.com/cloudflare/cloudflared/releases"
	}
}

func runWebhookTunnel(_ *cobra.Command, _ []string) error {
	if flagLive {
		return errors.New("webhook-tunnel only supports test mode for now — it won't run against a live-mode credential")
	}

	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
	}

	fmt.Println("Preflight checks passed.")
	return nil
}
```

- [ ] **Step 2: Verify the binary builds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Manually verify the two preflight paths**

```bash
go run . webhook-tunnel --live
```

Expected: exits with `Error: webhook-tunnel only supports test mode for now — it won't run against a live-mode credential`.

```bash
go run . webhook-tunnel
```

Expected: either `Preflight checks passed.` (if `cloudflared` is on your `PATH`) or a `cloudflared not found on PATH` error with an install hint.

- [ ] **Step 4: Commit**

```bash
git add cmd/webhooktunnel.go
git commit -m "feat: add webhook-tunnel command skeleton with preflight checks"
```

---

### Task 7: Subscription resolution decision logic + event type resolution

**Files:**
- Modify: `cmd/webhooktunnel.go`
- Test: `cmd/webhooktunnel_test.go`

This adds the pure decision function that Task 8 will wire up to real API calls, and tests it in isolation — this is the one piece of `cmd/webhooktunnel.go` worth unit testing, since (unlike the rest of the file) it makes no API calls and has no side effects.

- [ ] **Step 1: Write the failing test for `resolveSubscriptionAction`**

```go
// cmd/webhooktunnel_test.go
package cmd

import "testing"

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
```

- [ ] **Step 2: Run the test to confirm it fails**

```bash
go test ./cmd/... -run TestResolveSubscriptionAction
```

Expected: compile error — `resolveSubscriptionAction`, `actionCreateFresh`, etc. don't exist yet.

- [ ] **Step 3: Commit the failing test**

```bash
git add cmd/webhooktunnel_test.go
git commit -m "test: add failing tests for resolveSubscriptionAction"
```

- [ ] **Step 4: Add the decision types, `resolveSubscriptionAction`, and `resolveEventTypes` to `cmd/webhooktunnel.go`**

Replace the existing import block with this (it adds `context` plus three new internal packages to what Task 6 had):

```go
import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/webhookevents"
)
```

Add this new code after `cloudflaredInstallHint` and before `runWebhookTunnel`:

```go
// subscriptionActionKind describes what webhook-tunnel should do about
// test-mode webhook subscriptions before it can start receiving events.
type subscriptionActionKind int

const (
	// actionCreateFresh means a free subscription slot exists — create one
	// directly. Never destructive; we get a fresh signing secret.
	actionCreateFresh subscriptionActionKind = iota
	// actionRecreateOwned means both slots are full but one of them is a
	// subscription this tool created previously — delete and recreate it.
	// Nobody outside this tool depends on its secret, so this is also
	// never destructive.
	actionRecreateOwned
	// actionPickForeign means both slots are full and neither belongs to
	// this tool — the user must choose which one to temporarily repoint.
	actionPickForeign
)

// subscriptionAction is the result of resolveSubscriptionAction.
type subscriptionAction struct {
	Kind     subscriptionActionKind
	Existing *whWebhook // set for actionRecreateOwned
}

// maxTestSubscriptions is the number of test-mode webhook subscriptions
// Mollie currently allows per organization.
const maxTestSubscriptions = 2

// resolveSubscriptionAction decides what to do about test-mode webhook
// subscriptions given the current list and the ID of a subscription this
// tool created in a previous run (from the state file; "" if none/unknown).
// It makes no API calls — this is pure decision logic, kept separate from
// the actual HTTP/prompt side effects so it can be unit tested directly.
func resolveSubscriptionAction(existing []whWebhook, ownedID string) subscriptionAction {
	if len(existing) < maxTestSubscriptions {
		return subscriptionAction{Kind: actionCreateFresh}
	}

	if ownedID != "" {
		for i := range existing {
			if existing[i].ID == ownedID {
				return subscriptionAction{Kind: actionRecreateOwned, Existing: &existing[i]}
			}
		}
	}

	return subscriptionAction{Kind: actionPickForeign}
}

// resolveEventTypes returns the event types to subscribe to: whtEventTypes
// verbatim if the user set it, otherwise every event type the current
// credential can access (see internal/webhookevents).
func resolveEventTypes(ctx context.Context) ([]string, error) {
	if whtEventTypes != "" {
		return parseWebhookEventTypes(whtEventTypes), nil
	}

	key := cfg.APIKey
	if flagAPIKey != "" {
		key = flagAPIKey
	}
	if config.IsAPIKey(key) {
		return webhookevents.Resolve(ctx, true, nil)
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return nil, err
	}
	return webhookevents.Resolve(ctx, false, client.Permissions)
}
```

Leave `runWebhookTunnel` unchanged for now — Task 8 wires these into it.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./cmd/... -run TestResolveSubscriptionAction -v
```

Expected: all 3 tests pass.

- [ ] **Step 6: Verify the full build still succeeds**

```bash
go build ./...
```

Expected: no errors (note: `resolveEventTypes` is unused until Task 8 — Go doesn't complain about unused package-level functions, only unused local variables and imports, so this is fine).

- [ ] **Step 7: Commit**

```bash
git add cmd/webhooktunnel.go
git commit -m "feat: add subscription resolution decision logic and event type resolution"
```

---

### Task 8: Full tunnel lifecycle wiring

**Files:**
- Modify: `cmd/webhooktunnel.go`

This replaces the placeholder `runWebhookTunnel` body with the real lifecycle: recovery check, subscription resolution and API calls, starting the tunnel and local server, tailing events, and cleanup on Ctrl-C.

One deliberate refinement versus the design doc's listed order: the design doc lists "start local server, then spawn cloudflared, then apply subscription resolution." This implementation starts cloudflared *first* to obtain the tunnel URL, then applies the subscription action (which needs that URL), and only then starts the local server — once the signing secret (if any) is already known. This avoids ever needing a mutable "secret arrives later" hand-off into the running HTTP handler, and is equivalent in effect: nothing depends on the local server being up before the subscription is registered, since Mollie can't send anything to a URL it doesn't know about yet.

- [ ] **Step 1: Replace the full contents of `cmd/webhooktunnel.go`**

```go
// cmd/webhooktunnel.go
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/fjbender/mollie-cli/internal/tunnel"
	"github.com/fjbender/mollie-cli/internal/tunnelstate"
	"github.com/fjbender/mollie-cli/internal/webhookevents"
	"github.com/fjbender/mollie-cli/internal/webhookserver"
)

var (
	whtPort       int
	whtEventTypes string
)

var webhookTunnelCmd = &cobra.Command{
	Use:   "webhook-tunnel",
	Short: "Tunnel Mollie test-mode webhook events to your terminal",
	Long: `Spins up a public cloudflared tunnel to a local HTTP server, points a
Mollie test-mode webhook subscription at it, and logs every incoming event
until you press Ctrl-C. Test mode only — live mode is not yet supported.`,
	RunE: runWebhookTunnel,
}

func init() {
	webhookTunnelCmd.Flags().IntVar(&whtPort, "port", 10153, "Local port for the tunnel's HTTP server")
	webhookTunnelCmd.Flags().StringVar(&whtEventTypes, "event-types", "", `Comma-separated event types to subscribe to (default: every event type this credential can access)`)

	rootCmd.AddCommand(webhookTunnelCmd)
}

// cloudflaredInstallHint returns an OS-appropriate suggestion for installing
// cloudflared, since v1 never downloads it automatically.
func cloudflaredInstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "Install it with: brew install cloudflared"
	case "linux":
		return "cloudflared isn't in default OS repos. See https://pkg.cloudflare.com/index.html for apt/yum setup,\nor download a binary directly from https://github.com/cloudflare/cloudflared/releases"
	case "windows":
		return "Install it with: winget install --exact --id Cloudflare.cloudflared\nor download a binary from https://github.com/cloudflare/cloudflared/releases"
	default:
		return "Download a binary for your platform from https://github.com/cloudflare/cloudflared/releases"
	}
}

// subscriptionActionKind describes what webhook-tunnel should do about
// test-mode webhook subscriptions before it can start receiving events.
type subscriptionActionKind int

const (
	// actionCreateFresh means a free subscription slot exists — create one
	// directly. Never destructive; we get a fresh signing secret.
	actionCreateFresh subscriptionActionKind = iota
	// actionRecreateOwned means both slots are full but one of them is a
	// subscription this tool created previously — delete and recreate it.
	// Nobody outside this tool depends on its secret, so this is also
	// never destructive.
	actionRecreateOwned
	// actionPickForeign means both slots are full and neither belongs to
	// this tool — the user must choose which one to temporarily repoint.
	actionPickForeign
)

// subscriptionAction is the result of resolveSubscriptionAction.
type subscriptionAction struct {
	Kind     subscriptionActionKind
	Existing *whWebhook // set for actionRecreateOwned
}

// maxTestSubscriptions is the number of test-mode webhook subscriptions
// Mollie currently allows per organization.
const maxTestSubscriptions = 2

// resolveSubscriptionAction decides what to do about test-mode webhook
// subscriptions given the current list and the ID of a subscription this
// tool created in a previous run (from the state file; "" if none/unknown).
// It makes no API calls — this is pure decision logic, kept separate from
// the actual HTTP/prompt side effects so it can be unit tested directly.
func resolveSubscriptionAction(existing []whWebhook, ownedID string) subscriptionAction {
	if len(existing) < maxTestSubscriptions {
		return subscriptionAction{Kind: actionCreateFresh}
	}

	if ownedID != "" {
		for i := range existing {
			if existing[i].ID == ownedID {
				return subscriptionAction{Kind: actionRecreateOwned, Existing: &existing[i]}
			}
		}
	}

	return subscriptionAction{Kind: actionPickForeign}
}

// resolveEventTypes returns the event types to subscribe to: whtEventTypes
// verbatim if the user set it, otherwise every event type the current
// credential can access (see internal/webhookevents).
func resolveEventTypes(ctx context.Context) ([]string, error) {
	if whtEventTypes != "" {
		return parseWebhookEventTypes(whtEventTypes), nil
	}

	key := cfg.APIKey
	if flagAPIKey != "" {
		key = flagAPIKey
	}
	if config.IsAPIKey(key) {
		return webhookevents.Resolve(ctx, true, nil)
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return nil, err
	}
	return webhookevents.Resolve(ctx, false, client.Permissions)
}

// restoreSnapshot patches a webhook subscription back to a previously
// captured state. It never needs the subscription's signing secret — PATCH
// doesn't change it.
func restoreSnapshot(ctx context.Context, c *whClient, snap *tunnelstate.SubscriptionSnapshot) error {
	body := whUpdateBody{
		Name:       &snap.Name,
		URL:        &snap.URL,
		EventTypes: snap.EventTypes,
	}
	if c.needsTestmode() {
		t := true
		body.Testmode = &t
	}
	return c.mutate(ctx, http.MethodPatch, "/webhooks/"+snap.ID, body, nil)
}

// printEvent logs a single received webhook event to stdout.
func printEvent(ev webhookserver.Event) {
	badge := "unverified"
	if ev.Verified {
		badge = "verified"
	}

	var e whEvent
	if err := json.Unmarshal(ev.Body, &e); err != nil {
		fmt.Printf("%s  (unparseable body: %v)  [%s]\n", ev.ReceivedAt.Format(time.RFC3339), err, badge)
		return
	}
	fmt.Printf("%s  %-28s  %-30s  [%s]\n", ev.ReceivedAt.Format(time.RFC3339), e.Type, e.EntityID, badge)
}

func runWebhookTunnel(_ *cobra.Command, _ []string) error {
	if flagLive {
		return errors.New("webhook-tunnel only supports test mode for now — it won't run against a live-mode credential")
	}

	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	envFile, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	envName := envFile.ActiveEnvName()

	state, err := tunnelstate.Load()
	if err != nil {
		return fmt.Errorf("loading tunnel state: %w", err)
	}
	envState := state.Get(envName)

	c := newWhClient()

	if envState.PendingRestore != nil {
		fmt.Printf("A previous webhook-tunnel session for %q didn't shut down cleanly.\n", envName)
		restore, err := prompt.Confirm(fmt.Sprintf("Restore webhook subscription %q to its original URL before continuing?", envState.PendingRestore.Name))
		if err != nil {
			return err
		}
		if restore {
			if err := restoreSnapshot(ctx, c, envState.PendingRestore); err != nil {
				return fmt.Errorf("restoring previous subscription: %w", err)
			}
			envState.PendingRestore = nil
			if err := tunnelstate.Save(state); err != nil {
				return fmt.Errorf("saving tunnel state: %w", err)
			}
			fmt.Println("✓ Restored.")
		}
	}

	var list whWebhookList
	if err := c.get(ctx, "/webhooks", nil, &list); err != nil {
		return fmt.Errorf("listing webhook subscriptions: %w", err)
	}

	action := resolveSubscriptionAction(list.Embedded.Webhooks, envState.OwnedSubscriptionID)

	eventTypes, err := resolveEventTypes(ctx)
	if err != nil {
		return fmt.Errorf("resolving event types: %w", err)
	}
	if len(eventTypes) == 0 {
		return errors.New("no event types available to subscribe to — the current credential has no granted permissions for any known event type")
	}

	fmt.Printf("Starting tunnel on port %d...\n", whtPort)
	t, err := tunnel.Start(ctx, cloudflaredPath, whtPort, 20*time.Second)
	if err != nil {
		return fmt.Errorf("starting cloudflared tunnel: %w", err)
	}
	fmt.Printf("✓ Tunnel ready: %s\n", t.URL)

	var (
		secret  string
		created *whWebhook
	)

	switch action.Kind {
	case actionCreateFresh, actionRecreateOwned:
		if action.Kind == actionRecreateOwned {
			if err := c.mutate(ctx, http.MethodDelete, "/webhooks/"+action.Existing.ID, c.testmodeBody(), nil); err != nil {
				return fmt.Errorf("deleting previous mollie-cli subscription: %w", err)
			}
		}

		body := whCreateBody{
			Name:       "mollie-cli webhook-tunnel",
			URL:        t.URL,
			EventTypes: eventTypes,
		}
		if c.needsTestmode() {
			tm := true
			body.Testmode = &tm
		}

		var wh whWebhook
		if err := c.mutate(ctx, http.MethodPost, "/webhooks", body, &wh); err != nil {
			return fmt.Errorf("creating webhook subscription: %w", err)
		}
		created = &wh
		secret = wh.WebhookSecret
		envState.OwnedSubscriptionID = wh.ID
		if err := tunnelstate.Save(state); err != nil {
			return fmt.Errorf("saving tunnel state: %w", err)
		}

		fmt.Printf("✓ Webhook subscription %s created — signature verification enabled\n", wh.ID)
		fmt.Printf("  Signing secret: %s\n", secret)

	case actionPickForeign:
		opts := make([]prompt.SelectOption[string], 0, len(list.Embedded.Webhooks))
		for _, wh := range list.Embedded.Webhooks {
			opts = append(opts, prompt.SelectOption[string]{
				Label: fmt.Sprintf("%s — %s (%s)", wh.Name, truncateURL(wh.URL, 50), summarizeEventTypes(wh.EventTypes)),
				Value: wh.ID,
			})
		}
		chosenID, err := prompt.Select("Both test-mode webhook slots are in use. Which one should be temporarily repointed to the tunnel?", opts)
		if err != nil {
			return err
		}

		var chosen *whWebhook
		for i := range list.Embedded.Webhooks {
			if list.Embedded.Webhooks[i].ID == chosenID {
				chosen = &list.Embedded.Webhooks[i]
				break
			}
		}
		if chosen == nil {
			return fmt.Errorf("selected webhook %s not found", chosenID)
		}

		envState.PendingRestore = &tunnelstate.SubscriptionSnapshot{
			ID:         chosen.ID,
			Name:       chosen.Name,
			URL:        chosen.URL,
			EventTypes: chosen.EventTypes,
		}
		if err := tunnelstate.Save(state); err != nil {
			return fmt.Errorf("saving tunnel state: %w", err)
		}

		patchBody := whUpdateBody{URL: &t.URL}
		if whtEventTypes != "" {
			patchBody.EventTypes = eventTypes
		}
		if c.needsTestmode() {
			tm := true
			patchBody.Testmode = &tm
		}
		if err := c.mutate(ctx, http.MethodPatch, "/webhooks/"+chosen.ID, patchBody, nil); err != nil {
			return fmt.Errorf("repointing webhook subscription %s: %w", chosen.ID, err)
		}

		fmt.Printf("⚠ Repointed existing subscription %q — its signing secret is unknown, so incoming events cannot be verified this session.\n", chosen.Name)
	}

	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", whtPort),
		Handler: webhookserver.Handler(secret, printEvent),
	}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "local server error: %v\n", err)
		}
	}()

	fmt.Println("Listening for webhook events. Press Ctrl-C to stop.")
	<-ctx.Done()
	fmt.Println("\nShutting down...")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdownCtx)

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCleanup()

	switch action.Kind {
	case actionPickForeign:
		if envState.PendingRestore != nil {
			if err := restoreSnapshot(cleanupCtx, c, envState.PendingRestore); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to restore original webhook subscription: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run `mollie webhook-tunnel` again to retry automatically, or fix it manually with `mollie webhooks update`.")
			} else {
				envState.PendingRestore = nil
				fmt.Println("✓ Restored original webhook subscription.")
			}
		}
	default:
		if created != nil {
			if err := c.mutate(cleanupCtx, http.MethodDelete, "/webhooks/"+created.ID, c.testmodeBody(), nil); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete temporary webhook subscription %s: %v\n", created.ID, err)
			} else {
				envState.OwnedSubscriptionID = ""
			}
		}
	}

	if err := tunnelstate.Save(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save tunnel state: %v\n", err)
	}

	_ = t.Wait() // cloudflared is killed automatically because ctx was canceled

	return nil
}
```

- [ ] **Step 2: Verify the build succeeds**

```bash
go build ./...
```

Expected: no errors.

- [ ] **Step 3: Run the full test suite**

```bash
go test ./...
```

Expected: all tests pass, including the Task 1–4 and Task 7 tests.

- [ ] **Step 4: Run `go vet` (matches CI)**

```bash
go vet ./...
```

Expected: no issues.

- [ ] **Step 5: Commit**

```bash
git add cmd/webhooktunnel.go
git commit -m "feat: wire up full webhook-tunnel lifecycle"
```

---

### Task 9: Manual end-to-end smoke test

Requires: `cloudflared` installed, network access, and a Mollie test-mode credential already configured via `mollie auth setup`. This cannot be automated in CI and should be run by a human before considering the feature done.

- [ ] **Step 1: Build the binary**

```bash
go build -o mollie-cli .
```

- [ ] **Step 2: Start the tunnel (free-slot path)**

In a terminal with no existing test-mode webhook subscriptions configured:

```bash
./mollie-cli webhook-tunnel
```

Expected output, in order:
1. `Starting tunnel on port 10153...`
2. `✓ Tunnel ready: https://<random-words>.trycloudflare.com`
3. `✓ Webhook subscription wh_... created — signature verification enabled` followed by a `Signing secret: ...` line
4. `Listening for webhook events. Press Ctrl-C to stop.`

- [ ] **Step 3: Trigger a real event and confirm it's logged**

In a second terminal, note the webhook ID printed in step 2's output (`wh_...`) and ping it:

```bash
./mollie-cli webhooks ping wh_...
```

Expected: within a couple of seconds, the first terminal prints a log line like:

```
2026-07-21T15:04:05Z  payment-link.paid            pl_...                          [verified]
```

confirming the tunnel, subscription, and signature verification all work end-to-end. (The exact event type Mollie's ping sends may vary — any single log line with `[verified]` confirms success.)

- [ ] **Step 4: Verify clean shutdown and restore**

Press Ctrl-C in the first terminal. Expected:
1. `Shutting down...`
2. Since this was the free-slot/fresh-create path, no restore message — the temporary subscription is deleted instead.
3. `./mollie-cli webhooks list` afterward should no longer show the `mollie-cli webhook-tunnel` subscription.

- [ ] **Step 5: Verify crash recovery (foreign-subscription path)**

This requires two existing test-mode subscriptions to force the `actionPickForeign` path, so it's optional if that's inconvenient to arrange, but valuable if you already have a real local dev webhook configured for test mode:

1. Run `./mollie-cli webhook-tunnel` with both test slots full. Confirm the interactive picker appears listing both subscriptions.
2. Pick the one that is NOT your real dev subscription (if applicable) and confirm the `⚠ Repointed existing subscription... unverified` warning appears.
3. Kill the process hard (`kill -9 <pid>` from another terminal, not Ctrl-C) to simulate a crash.
4. Run `./mollie-cli webhook-tunnel` again. Confirm it detects the leftover pending restore and offers to restore the original subscription before proceeding.

- [ ] **Step 6: Clean up any leftover binary**

```bash
rm -f mollie-cli
```
