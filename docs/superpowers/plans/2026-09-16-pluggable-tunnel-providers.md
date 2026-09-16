# Pluggable Tunnel Providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `mollie webhook-tunnel` skip spawning `cloudflared` and instead use a public URL the user already has routed back to the local port some other way (e.g. an SSH reverse tunnel into a self-hosted nginx host), via a new `--tunnel external --public-url <url>` mode.

**Architecture:** Introduce a `tunnel.Provider` interface in `internal/tunnel` with two implementations — `cloudflaredProvider` (adapts the existing, untouched `Start`/`Tunnel`) and `externalProvider` (no subprocess, just echoes back a user-supplied URL). `cmd/webhooktunnel.go` gains a `newTunnelProvider(kind, publicURL string) (tunnel.Provider, error)` factory and two new flags (`--tunnel`, `--public-url`); the rest of the command's orchestration (port binding, HTTP server, subscription resolution, retries, tail output, cleanup) is untouched because it only ever consumed a URL string and a `Wait()` call.

**Tech Stack:** Go stdlib (`context`, `os/exec`, `net/http`), Cobra. No new dependencies.

**Reference:** `docs/superpowers/specs/2026-09-16-pluggable-tunnel-providers-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/tunnel/provider.go` | Create | `Provider` interface + `cloudflaredProvider` adapter around the existing `Start`/`Tunnel` |
| `internal/tunnel/provider_test.go` | Create | Tests for `cloudflaredProvider`, reusing the existing `fakeCloudflared` helper |
| `internal/tunnel/external.go` | Create | `externalProvider` — no subprocess, echoes back a caller-supplied URL |
| `internal/tunnel/external_test.go` | Create | Tests for `externalProvider` |
| `cmd/webhooktunnel.go` | Modify | New `--tunnel`/`--public-url` flags, `newTunnelProvider` factory, `runWebhookTunnel` wired to `tunnel.Provider` instead of calling `tunnel.Start` directly |
| `cmd/webhooktunnel_test.go` | Modify | Tests for `newTunnelProvider`'s validation branches |
| `README.md` | Modify | Document `--tunnel`/`--public-url` in the `webhook-tunnel` section |

---

### Task 1: `tunnel.Provider` interface + `cloudflaredProvider`

**Files:**
- Create: `internal/tunnel/provider.go`
- Test: `internal/tunnel/provider_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/tunnel/provider_test.go
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
```

This reuses the `fakeCloudflared` helper already defined in `internal/tunnel/tunnel_test.go` — same package (`tunnel_test`), same directory, so it's visible without any import.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tunnel/... -run TestCloudflaredProvider -v`
Expected: FAIL — compile error, `undefined: tunnel.NewCloudflaredProvider`

- [ ] **Step 3: Write the implementation**

```go
// internal/tunnel/provider.go
package tunnel

import (
	"context"
	"time"
)

// Provider exposes a local port at a URL reachable from the public
// internet, so a Mollie webhook subscription can point at it.
type Provider interface {
	// Start begins exposing localhost:port publicly and returns the
	// externally reachable URL once ready.
	Start(ctx context.Context, port int) (url string, err error)
	// Wait blocks until the tunnel's underlying subprocess (if any) exits,
	// or until ctx is done for providers with no subprocess of their own.
	Wait() error
}

// cloudflaredProvider adapts Start/Tunnel to the Provider interface.
type cloudflaredProvider struct {
	path        string
	waitTimeout time.Duration
	tun         *Tunnel
}

// NewCloudflaredProvider returns a Provider that spawns a cloudflared quick
// tunnel pointed at the given local port. cloudflaredPath is the path to the
// cloudflared binary (e.g. from exec.LookPath); waitTimeout bounds how long
// to wait for cloudflared to report its public URL.
func NewCloudflaredProvider(cloudflaredPath string, waitTimeout time.Duration) Provider {
	return &cloudflaredProvider{path: cloudflaredPath, waitTimeout: waitTimeout}
}

func (p *cloudflaredProvider) Start(ctx context.Context, port int) (string, error) {
	tun, err := Start(ctx, p.path, port, p.waitTimeout)
	if err != nil {
		return "", err
	}
	p.tun = tun
	return tun.URL, nil
}

func (p *cloudflaredProvider) Wait() error {
	return p.tun.Wait()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tunnel/... -v`
Expected: PASS (all tests in the package, including the pre-existing `tunnel_test.go` ones)

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/provider.go internal/tunnel/provider_test.go
git commit -m "feat: add tunnel.Provider interface and cloudflaredProvider adapter"
```

---

### Task 2: `externalProvider`

**Files:**
- Create: `internal/tunnel/external.go`
- Test: `internal/tunnel/external_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/tunnel/external_test.go
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
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tunnel/... -run TestExternalProvider -v`
Expected: FAIL — compile error, `undefined: tunnel.NewExternalProvider`

- [ ] **Step 3: Write the implementation**

```go
// internal/tunnel/external.go
package tunnel

import "context"

// externalProvider is a Provider that spawns no subprocess. It trusts a URL
// the caller already knows is publicly reachable and routed back to the
// local port some other way — e.g. an SSH reverse tunnel into a
// self-hosted reverse proxy. No reachability check is performed; a wrong
// URL simply causes whatever webhook create/update call uses it to fail the
// same way it would for any other invalid URL.
type externalProvider struct {
	publicURL string
	ctx       context.Context
}

// NewExternalProvider returns a Provider that always reports publicURL as
// the externally reachable URL, without spawning any tunnel process.
func NewExternalProvider(publicURL string) Provider {
	return &externalProvider{publicURL: publicURL}
}

func (p *externalProvider) Start(ctx context.Context, _ int) (string, error) {
	p.ctx = ctx
	return p.publicURL, nil
}

// Wait blocks until the context passed to Start is done, since there is no
// subprocess of its own to wait on.
func (p *externalProvider) Wait() error {
	<-p.ctx.Done()
	return p.ctx.Err()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tunnel/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tunnel/external.go internal/tunnel/external_test.go
git commit -m "feat: add externalProvider for user-supplied public URLs"
```

---

### Task 3: Wire `webhook-tunnel` command to `tunnel.Provider`

**Files:**
- Modify: `cmd/webhooktunnel.go`
- Modify: `cmd/webhooktunnel_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `cmd/webhooktunnel_test.go` (it already imports `"strings"`, `"testing"` — no new imports needed):

```go
func TestNewTunnelProvider_UnknownKindErrors(t *testing.T) {
	_, err := newTunnelProvider("bogus", "")
	if err == nil {
		t.Fatal("expected an error for an unknown --tunnel value")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error = %v, want it to mention the invalid value", err)
	}
}

func TestNewTunnelProvider_ExternalWithoutPublicURLErrors(t *testing.T) {
	_, err := newTunnelProvider("external", "")
	if err == nil {
		t.Fatal("expected an error when --tunnel external is used without --public-url")
	}
}

func TestNewTunnelProvider_ExternalWithPublicURLSucceeds(t *testing.T) {
	p, err := newTunnelProvider("external", "https://webhooks.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected a non-nil provider")
	}
}

func TestNewTunnelProvider_CloudflaredWithPublicURLErrors(t *testing.T) {
	_, err := newTunnelProvider("cloudflared", "https://webhooks.example.com")
	if err == nil {
		t.Fatal("expected an error when --public-url is set together with --tunnel cloudflared")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/... -run TestNewTunnelProvider -v`
Expected: FAIL — compile error, `undefined: newTunnelProvider`

- [ ] **Step 3: Add the new flags**

In `cmd/webhooktunnel.go`, replace:

```go
var (
	whtPort       int
	whtEventTypes string
	whtLogFile    string
)
```

with:

```go
var (
	whtPort       int
	whtEventTypes string
	whtLogFile    string
	whtTunnel     string
	whtPublicURL  string
)
```

- [ ] **Step 4: Update the command's `Long` text and register the new flags**

Replace:

```go
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
	webhookTunnelCmd.Flags().StringVar(&whtLogFile, "logfile", "/tmp/mollie-webhook-log", "File to append a raw log of every incoming webhook HTTP call to (method, URL, headers, body, timestamp)")

	rootCmd.AddCommand(webhookTunnelCmd)
}
```

with:

```go
var webhookTunnelCmd = &cobra.Command{
	Use:   "webhook-tunnel",
	Short: "Tunnel Mollie test-mode webhook events to your terminal",
	Long: `Spins up a public tunnel to a local HTTP server — a cloudflared quick
tunnel by default, or a URL you already have routed to --port yourself via
--tunnel external — points a Mollie test-mode webhook subscription at it,
and logs every incoming event until you press Ctrl-C. Test mode only — live
mode is not yet supported.`,
	RunE: runWebhookTunnel,
}

func init() {
	webhookTunnelCmd.Flags().IntVar(&whtPort, "port", 10153, "Local port for the tunnel's HTTP server")
	webhookTunnelCmd.Flags().StringVar(&whtEventTypes, "event-types", "", `Comma-separated event types to subscribe to (default: every event type this credential can access)`)
	webhookTunnelCmd.Flags().StringVar(&whtLogFile, "logfile", "/tmp/mollie-webhook-log", "File to append a raw log of every incoming webhook HTTP call to (method, URL, headers, body, timestamp)")
	webhookTunnelCmd.Flags().StringVar(&whtTunnel, "tunnel", "cloudflared", `Tunnel provider: "cloudflared" (spawns a cloudflared quick tunnel) or "external" (use a public URL you already have routed to --port yourself, e.g. via an SSH reverse tunnel)`)
	webhookTunnelCmd.Flags().StringVar(&whtPublicURL, "public-url", "", "Publicly reachable URL already routed to --port; required when --tunnel external")

	rootCmd.AddCommand(webhookTunnelCmd)
}
```

- [ ] **Step 5: Add the `newTunnelProvider` factory**

Add this function anywhere in `cmd/webhooktunnel.go` above `runWebhookTunnel` (e.g. right after `cloudflaredInstallHint`):

```go
// newTunnelProvider builds the tunnel.Provider selected by --tunnel,
// validating the flag combination before anything else in
// runWebhookTunnel touches the network or the local port.
func newTunnelProvider(kind, publicURL string) (tunnel.Provider, error) {
	switch kind {
	case "cloudflared":
		if publicURL != "" {
			return nil, errors.New("--public-url can only be used with --tunnel external")
		}
		cloudflaredPath, err := exec.LookPath("cloudflared")
		if err != nil {
			return nil, fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
		}
		return tunnel.NewCloudflaredProvider(cloudflaredPath, 20*time.Second), nil
	case "external":
		if publicURL == "" {
			return nil, errors.New("--public-url is required when --tunnel external")
		}
		return tunnel.NewExternalProvider(publicURL), nil
	default:
		return nil, fmt.Errorf("unknown --tunnel value %q (want \"cloudflared\" or \"external\")", kind)
	}
}
```

- [ ] **Step 6: Run the new tests to verify they pass**

Run: `go test ./cmd/... -run TestNewTunnelProvider -v`
Expected: PASS

- [ ] **Step 7: Wire `runWebhookTunnel` to use the provider**

Replace:

```go
	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
	}
```

with:

```go
	provider, err := newTunnelProvider(whtTunnel, whtPublicURL)
	if err != nil {
		return err
	}
```

Replace:

```go
	fmt.Printf("Starting tunnel on port %d...\n", whtPort)
	t, err := tunnel.Start(ctx, cloudflaredPath, whtPort, 20*time.Second)
	if err != nil {
		return fmt.Errorf("starting cloudflared tunnel: %w", err)
	}
	fmt.Printf("✓ Tunnel ready: %s\n", t.URL)
```

with:

```go
	fmt.Printf("Starting tunnel on port %d...\n", whtPort)
	tunnelURL, err := provider.Start(ctx, whtPort)
	if err != nil {
		return fmt.Errorf("starting tunnel: %w", err)
	}
	fmt.Printf("✓ Tunnel ready: %s\n", tunnelURL)
```

Replace (inside the `actionCreateFresh, actionRecreateOwned` case):

```go
		body := whCreateBody{
			Name:       "mollie-cli webhook-tunnel",
			URL:        t.URL,
			EventTypes: eventTypes,
		}
```

with:

```go
		body := whCreateBody{
			Name:       "mollie-cli webhook-tunnel",
			URL:        tunnelURL,
			EventTypes: eventTypes,
		}
```

Replace (inside the `actionPickForeign` case):

```go
		patchBody := whUpdateBody{URL: &t.URL}
```

with:

```go
		patchBody := whUpdateBody{URL: &tunnelURL}
```

Replace the final cleanup line:

```go
	_ = t.Wait() // cloudflared is killed automatically because ctx was canceled
```

with:

```go
	_ = provider.Wait() // underlying subprocess (if any) is killed/unblocked because ctx was canceled
```

- [ ] **Step 8: Run the full test suite and build**

Run: `go build ./... && go test ./... -v`
Expected: PASS, no compile errors, no leftover references to `t.URL`/`t.Wait()`/`cloudflaredPath` in `cmd/webhooktunnel.go`

Run: `grep -n "t\.URL\|t\.Wait\|cloudflaredPath" cmd/webhooktunnel.go`
Expected: no output (empty) — confirms the old variable is fully replaced

- [ ] **Step 9: Commit**

```bash
git add cmd/webhooktunnel.go cmd/webhooktunnel_test.go
git commit -m "feat: add --tunnel external mode to webhook-tunnel"
```

---

### Task 4: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the `webhook-tunnel` section**

In `README.md`, replace:

```
### `webhook-tunnel` — local webhook testing

```
mollie webhook-tunnel [--port N] [--event-types <types>] [--logfile <path>]
```

Spins up a public [`cloudflared`](https://github.com/cloudflare/cloudflared) tunnel to a local HTTP server, points a test-mode webhook subscription at it, and prints every incoming event to your terminal until you press Ctrl-C. Test mode only — requires `cloudflared` on your `PATH` (e.g. `brew install cloudflared` on macOS).

| Flag | Default | Description |
|---|---|---|
| `--port` | `10153` | Local port the tunnel's HTTP server listens on |
| `--event-types` | every type the active credential can access | Comma-separated event types to subscribe to |
| `--logfile` | `/tmp/mollie-webhook-log` | Appends a raw HTTP-shaped record (method, URL, headers, body, timestamp) of every incoming call |
```

with:

```
### `webhook-tunnel` — local webhook testing

```
mollie webhook-tunnel [--port N] [--event-types <types>] [--logfile <path>] \
                       [--tunnel cloudflared|external] [--public-url <url>]
```

Spins up a public tunnel to a local HTTP server, points a test-mode webhook subscription at it, and prints every incoming event to your terminal until you press Ctrl-C. Test mode only.

By default this uses [`cloudflared`](https://github.com/cloudflare/cloudflared) (requires it on your `PATH`, e.g. `brew install cloudflared` on macOS). If you already expose a local port to the internet some other way — e.g. an SSH reverse tunnel into a self-hosted nginx host — pass `--tunnel external --public-url <url>` instead: the CLI still binds `--port` and serves requests there, it just skips spawning `cloudflared` and trusts the URL you give it.

| Flag | Default | Description |
|---|---|---|
| `--port` | `10153` | Local port the tunnel's HTTP server listens on |
| `--event-types` | every type the active credential can access | Comma-separated event types to subscribe to |
| `--logfile` | `/tmp/mollie-webhook-log` | Appends a raw HTTP-shaped record (method, URL, headers, body, timestamp) of every incoming call |
| `--tunnel` | `cloudflared` | Tunnel provider: `cloudflared` or `external` |
| `--public-url` | — | Publicly reachable URL already routed to `--port`; required when `--tunnel external` |
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document webhook-tunnel --tunnel and --public-url flags"
```

---

## Self-Review Notes

- **Spec coverage:** `Provider` interface (Task 1), `cloudflaredProvider` (Task 1), `externalProvider` (Task 2), `--tunnel`/`--public-url` flags + validation + wiring (Task 3), README (Task 4). All spec sections have a corresponding task.
- **No placeholders:** every step has complete, concrete code.
- **Type consistency:** `Provider.Start(ctx, port) (string, error)` and `Provider.Wait() error` are used identically in `cloudflaredProvider`, `externalProvider`, and `cmd/webhooktunnel.go` across all tasks.
