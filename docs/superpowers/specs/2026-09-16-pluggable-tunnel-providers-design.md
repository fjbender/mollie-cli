# Pluggable Tunnel Providers Design

Date: 2026-09-16
Status: Approved

## Overview

`mollie webhook-tunnel` currently always spawns a `cloudflared` quick tunnel to expose its local HTTP server publicly. Some developers already have their own way of exposing a local port to the internet — e.g. an SSH reverse tunnel into a self-hosted nginx host — and don't want the CLI spawning `cloudflared` at all. They just need the CLI to bind the local port and serve requests, and to trust a public URL they already know is routed back to it.

This introduces a small `tunnel.Provider` abstraction with two implementations — `cloudflared` (existing behavior, now the default) and `external` (new: no subprocess, user-supplied public URL) — so `webhook-tunnel`'s orchestration logic doesn't need to know which one is in play, and a third backend (e.g. ngrok) could be added later without touching `cmd/webhooktunnel.go`.

## `tunnel.Provider` Interface

New file `internal/tunnel/provider.go`:

```go
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
```

### `cloudflaredProvider`

Adapts the existing `Start`/`Tunnel` in `internal/tunnel/tunnel.go` to the interface — that file and its tests are unchanged.

```go
type cloudflaredProvider struct {
    path        string
    waitTimeout time.Duration
    tun         *Tunnel
}

func NewCloudflaredProvider(path string, waitTimeout time.Duration) Provider

func (p *cloudflaredProvider) Start(ctx context.Context, port int) (string, error) {
    tun, err := Start(ctx, p.path, port, p.waitTimeout)
    if err != nil {
        return "", err
    }
    p.tun = tun
    return tun.URL, nil
}

func (p *cloudflaredProvider) Wait() error { return p.tun.Wait() }
```

### `externalProvider`

New file `internal/tunnel/external.go`. Trusts a URL the caller already knows is publicly reachable and routed back to the local port some other way (SSH reverse tunnel, existing reverse proxy, etc.). The CLI's own port-binding and HTTP server (`net.Listen` + `http.Server` in `cmd/webhooktunnel.go`) are unchanged and still do the actual listening — this provider only supplies the URL and has no subprocess to manage.

```go
type externalProvider struct {
    publicURL string
}

func NewExternalProvider(publicURL string) Provider

func (p *externalProvider) Start(ctx context.Context, _ int) (string, error) {
    return p.publicURL, nil
}

func (p *externalProvider) Wait() error {
    <-ctx.Done() // via a context captured in Start
    return ctx.Err()
}
```

(`Wait` needs the `ctx` passed to `Start` — the implementation stores it on the struct in `Start` before returning.)

No reachability check is performed on `--public-url`; if it's wrong, Mollie's webhook create/update call will fail the same way it does today for a bad URL, and the existing retry-with-backoff logic (built for Cloudflare DNS propagation delay) will simply exhaust its attempts and surface that error.

## Command Changes (`cmd/webhooktunnel.go`)

Two new flags:

- `--tunnel <cloudflared|external>` (default `cloudflared`) — which provider to use.
- `--public-url <url>` — required when `--tunnel external`; rejected with an error if set together with `--tunnel cloudflared` (avoids silently ignoring a flag the user thought was doing something).

Preflight changes:

1. `--tunnel cloudflared` (default): unchanged — `exec.LookPath("cloudflared")`, install hint on failure.
2. `--tunnel external`: skip the `cloudflared` binary check entirely; require `--public-url` to be non-empty, erroring out before any other side effect (port bind, subscription mutation) if it's missing.
3. Unknown `--tunnel` value: error out listing the two valid values.

Provider construction replaces the current direct `tunnel.Start(...)` call:

```go
var provider tunnel.Provider
switch whtTunnel {
case "cloudflared":
    cloudflaredPath, err := exec.LookPath("cloudflared")
    if err != nil {
        return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
    }
    provider = tunnel.NewCloudflaredProvider(cloudflaredPath, 20*time.Second)
case "external":
    if whtPublicURL == "" {
        return errors.New("--public-url is required when --tunnel external")
    }
    provider = tunnel.NewExternalProvider(whtPublicURL)
default:
    return fmt.Errorf("unknown --tunnel value %q (want \"cloudflared\" or \"external\")", whtTunnel)
}
```

Everything downstream of obtaining the URL — subscription resolution, the retry-with-backoff webhook create/patch calls, signature verification, tail output, state-file bookkeeping, and cleanup on Ctrl-C — is unchanged. It only ever consumed a URL string (previously `t.URL`, now the string returned by `provider.Start`) and a way to block until the tunnel process is done (previously `t.Wait()`, now `provider.Wait()`).

`Long` help text gains a line mentioning `--tunnel external` for users who already expose a local port themselves.

## Files Changed

| File | Change |
|---|---|
| `internal/tunnel/provider.go` | New — `Provider` interface, `cloudflaredProvider`, `NewCloudflaredProvider`. |
| `internal/tunnel/external.go` | New — `externalProvider`, `NewExternalProvider`. |
| `internal/tunnel/tunnel.go` | No change. |
| `cmd/webhooktunnel.go` | Add `--tunnel`/`--public-url` flags, preflight branching, provider construction replacing the direct `tunnel.Start` call, updated `Long` text. |

## Testing

- `internal/tunnel`: new tests for `cloudflaredProvider` (reusing the existing `fakeCloudflared` helper — asserts it returns the same URL `Start` would) and `externalProvider` (returns the given URL immediately from `Start`; `Wait` blocks until context cancellation, mirroring `TestTunnel_KilledWhenContextCanceled`'s pattern).
- `cmd/webhooktunnel_test.go`: flag-validation tests — missing `--public-url` with `--tunnel external` errors before touching the network; unknown `--tunnel` value errors; `--public-url` set together with `--tunnel cloudflared` errors.

## Out of Scope

- Managing the SSH tunnel / nginx / any other external exposure mechanism on the user's behalf.
- Validating that `--public-url` is actually reachable before attempting to use it.
- Additional provider types (ngrok, etc.) — the interface leaves room, but only `cloudflared` and `external` ship now.
- Changing how the local port is bound or served — identical in both modes.
