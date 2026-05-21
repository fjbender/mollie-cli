# Verbose Mode Design

Date: 2026-05-21
Status: Approved

## Overview

Add a `-v` / `-vv` persistent flag to the mollie-cli that logs HTTP traffic to stderr, modelled on `curl -v` / `-vv`. The two levels differ in how much of the HTTP exchange is exposed:

- `-v` — JSON request and response bodies only
- `-vv` — full HTTP wire format (method, path, version, headers, body) for both request and response

All verbose output goes to **stderr** so normal command output on stdout remains pipeable.

## Flag

Add a Cobra `CountVarP` persistent flag on the root command:

```go
var flagVerbose int
rootCmd.PersistentFlags().CountVarP(&flagVerbose, "verbose", "v",
    "Verbose output: -v logs JSON bodies, -vv logs full HTTP wire format")
```

`CountVarP` increments the integer once per `-v`, so `-v` = 1, `-vv` = 2. Values ≥ 2 are treated as level 2 (no third mode).

## Transport

A new package `internal/verbose` exposes a `LoggingTransport` that wraps any `http.RoundTripper`:

```go
type LoggingTransport struct {
    Level int
    Inner http.RoundTripper
}

func (t *LoggingTransport) RoundTrip(req *http.Request) (*http.Response, error)
```

When `Level == 0` the inner transport is called directly (no-op path; included so callers don't need to branch).

**Level 1** — before forwarding the request, reads and re-buffers the request body, pretty-prints it as JSON to stderr prefixed with `→ METHOD URL`. After the response arrives, reads and re-buffers the response body, pretty-prints it as JSON to stderr prefixed with `← STATUS`. Non-JSON bodies fall back to raw output.

**Level 2** — uses `net/http/httputil.DumpRequestOut` and `httputil.DumpResponse` to produce the canonical wire-format dump. Each line of the request dump is prefixed with `> `, each line of the response dump with `< `. After dumping, the `Authorization` header value is redacted: the credential type prefix (`Bearer test_`, `Bearer live_`, `Bearer `) is preserved and the remainder is replaced with `[REDACTED]`. A blank line separates the request block from the response block.

The transport is created in `mollieclient.New()` (and `mollieclient.NewOrganizationClient()`) when `verboseLevel > 0`:

```go
func New(cfg *config.Config, apiKeyOverride string, liveMode bool, profileID string, verboseLevel int) (*mollieapi.Client, error) {
    ...
    if verboseLevel > 0 {
        opts = append(opts, mollieapi.WithClient(&http.Client{
            Transport: &verbose.LoggingTransport{
                Level: verboseLevel,
                Inner: http.DefaultTransport,
            },
        }))
    }
    ...
}
```

Call sites in `cmd/` pass `flagVerbose` as the final argument.

## Output Format

### Level 1 (`-v`)

```
→ POST https://api.mollie.com/v2/payments
{
  "amount": { "value": "10.00", "currency": "EUR" },
  "description": "Test payment"
}
← 201 Created
{
  "resource": "payment",
  "id": "tr_xxx"
}
```

### Level 2 (`-vv`)

```
> POST /v2/payments HTTP/1.1
> Host: api.mollie.com
> Authorization: Bearer test_[REDACTED]
> Content-Type: application/json
>
> {"amount":{"value":"10.00","currency":"EUR"},...}

< HTTP/1.1 201 Created
< Content-Type: application/json
< X-Mollie-Request-Id: req_xxx
<
< {"resource":"payment","id":"tr_xxx",...}
```

## Files Changed

| File | Change |
|---|---|
| `cmd/root.go` | Add `flagVerbose int`, register `CountVarP` |
| `internal/mollieclient/mollieclient.go` | Accept `verboseLevel int`, inject `LoggingTransport` |
| `internal/verbose/transport.go` | New — `LoggingTransport` implementation |
| `cmd/*.go` (all call sites) | Pass `flagVerbose` to `mollieclient.New()` / `NewOrganizationClient()` |

## Out of Scope

- Color/styling on verbose output (stderr, not styled)
- Configuring verbose level via config file or env var
- A third level (`-vvv`) beyond wire format
