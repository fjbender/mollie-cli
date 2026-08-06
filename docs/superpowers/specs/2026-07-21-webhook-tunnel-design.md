# Webhook Tunnel Design

Date: 2026-07-21
Status: Approved

## Overview

Add `mollie webhook-tunnel`, a command that spins up a public tunnel to a local HTTP server, points a Mollie test-mode webhook subscription at it, and tails incoming events live until the user hits Ctrl-C. This lets a developer see real webhook deliveries from the Mollie sandbox without deploying anything.

**v1 is observe-only: it logs events, it does not forward them anywhere.** The CLI's own HTTP server, sitting behind the tunnel, is the only thing that ever receives these requests in v1. Relaying them to a developer's local application (à la `stripe listen --forward-to`) is a distinct, explicitly deferred capability — see "Milestone 2 — `--forward`" below.

**v1 is test-mode only.** Live mode is explicitly out of scope (see below) — the risk of silently breaking a merchant's production webhook delivery is high enough that it deserves its own follow-up design once the core tunnel/recovery machinery has proven itself.

## Command & Flags

```
mollie webhook-tunnel [--port <int>] [--event-types <comma-separated>]
```

- `--port` — local port for the HTTP server and the tunnel target (default `10153`).
- `--event-types` — optional. If set, used verbatim (same parsing as `webhooks create --event-types`), bypassing the permission-aware resolution described below. If omitted, defaults to "every event type this credential can access."
- `--live` is rejected with an explicit "live mode not supported yet" error.

Reuses the existing `whClient` (raw `net/http` client in `cmd/webhooks.go`, package-private, same package as the new command file) for all `/webhooks` CRUD — no reason to duplicate it.

## Preflight Checks

1. Reject `--live`.
2. `exec.LookPath("cloudflared")` — if not found, print an OS-appropriate install hint (`brew install cloudflared` / apt / direct download link) and exit non-zero. No auto-download of the binary — avoids adding binary supply-chain trust logic for v1.
3. Check the state file (see below) for a leftover pending restore from a previous, uncleanly-terminated run, and offer to fix it before proceeding.

## State File & Crash Recovery

New package `internal/tunnelstate`, file stored next to `config.toml` (same XDG dir) as `webhook-tunnel-state.json`:

```go
type SubscriptionSnapshot struct {
    ID         string   `json:"id"`
    Name       string   `json:"name"`
    URL        string   `json:"url"`
    EventTypes []string `json:"event_types"`
}

type EnvState struct {
    OwnedSubscriptionID string                `json:"owned_subscription_id,omitempty"`
    PendingRestore      *SubscriptionSnapshot `json:"pending_restore,omitempty"`
}

type File struct {
    Environments map[string]*EnvState `json:"environments"`
}
```

Keyed per active environment name (mirrors `config.File`), so separate environments never collide.

- `OwnedSubscriptionID` — the ID of a subscription this tool created, so a future run can recognize and safely reuse/replace it.
- `PendingRestore` — a snapshot of a **foreign** subscription's prior state, written *before* it's patched to point at the tunnel. Cleared once successfully restored on exit. If a run dies without clearing it, the next run's preflight detects it and offers to restore before doing anything else.

## Subscription Resolution

Mollie currently allows **2 active test-mode webhook subscriptions**. On startup:

1. `GET /webhooks` (test mode) → list existing subscriptions.
2. **A free slot exists (< 2 subscriptions)** → `POST` create a new one directly. We receive the `webhookSecret` in the response → full signature verification for this session. Never destructive. The new subscription's ID is written to `OwnedSubscriptionID` in the state file so a future run recognizes it as ours.
3. **No free slot:**
   - If one of the two matches `OwnedSubscriptionID` from the state file → **ours**: `DELETE` it, then `POST` create a fresh replacement, writing the new ID back to `OwnedSubscriptionID` (it changes every cycle). Nobody outside this tool depends on its secret, so this is safe to always cycle, and we get full verification again.
   - Otherwise, **both are foreign** → show an interactive picker (new `prompt.Select`-style helper, modeled on the existing `prompt.ProfileSelect`) listing both subscriptions' name/URL/event types, and let the user choose which to temporarily repoint. Snapshot the chosen one into `PendingRestore` *before* mutating, then `PATCH` its URL (and `EventTypes`, if `--event-types` was given). We never see its secret — see "Signature Verification" below.

## Event Type Resolution ("all available events")

New package `internal/webhookevents`, scoped to `webhook-tunnel` only (existing `webhooks create`/`update --event-types` behavior is unchanged).

An embedded static table maps documented **global** Next-gen Webhook event types to their required permission, e.g.:

```go
var globalEventPermissions = map[string]string{
    "payment-link.paid":                          "payment-links.read",
    "balance-transaction.created":                 "balances.read",
    "sales-invoice.created":                        "invoices.read",
    "sales-invoice.issued":                         "invoices.read",
    "sales-invoice.canceled":                       "invoices.read",
    "sales-invoice.paid":                           "invoices.read",
    "business-account-transfer.requested":          "business-account-transfers.read",
    "business-account-transfer.initiated":          "business-account-transfers.read",
    "business-account-transfer.pending-review":     "business-account-transfers.read",
    "business-account-transfer.processed":          "business-account-transfers.read",
    "business-account-transfer.failed":             "business-account-transfers.read",
    "business-account-transfer.blocked":            "business-account-transfers.read",
    "business-account-transfer.returned":           "business-account-transfers.read",
    "payout.initiated":                             "payouts.read",
    "payout.completed":                             "payouts.read",
    "payout.processing-at-bank":                    "payouts.read",
    "payout.canceled":                              "payouts.read",
    "payout.failed":                                "payouts.read",
}
```

Beta event types (`dispute.*`, `file.*`, `unmatched-credit-transfer.*`, `connect-balance-transfer.*`) are **excluded by default** — not in this table for v1.

Resolution (`webhookevents.Resolve(ctx, sdkClient, isAPIKey bool) ([]string, error)`):

- **API key** (`test_`/`live_` prefix) → these are fully privileged and the Permissions API rejects them anyway (Advanced/OAuth tokens only) → return the full table's event types directly, no API call.
- **Access token** → call the SDK's existing `client.Permissions.List(ctx, nil)` (already supported by `mollie-api-golang`, no bypass needed), collect the set of `ID`s where `Granted == true`, and return only the table's event types whose required permission is in that set. This call always uses `mollieclient.NewOrganizationClient` (the same minimal client already used for Invoices), never the regular per-command client — the Permissions API only works against live mode and rejects a profileId, regardless of the mode/profile the rest of the command runs with. Safe to do silently: it's a read-only GET reporting the token's permissions, not mode- or profile-specific data.

This is the resolved list passed as `eventTypes` on the create/recreate/patch call whenever `--event-types` wasn't explicitly given.

## Tunnel Lifecycle

1. Bind the local port up front, before any external side effect (tunnel or subscription mutation) — a busy `--port` fails fast with nothing else touched yet.
2. Spawn `cloudflared tunnel --url http://localhost:<port>` (new package `internal/tunnel`), scanning combined stdout/stderr for a `https://*.trycloudflare.com` URL with a bounded timeout (e.g. 20s); fail with a clear error if it never appears.
3. Start the local HTTP server on the already-bound port. This has to happen *before* the next step: Mollie validates a webhook subscription's URL synchronously on create/update, including expecting an HTTP 200 response, so something must already be listening behind the tunnel. For a freshly created subscription the signing secret isn't known until that same create call returns it, so the server starts with a secret-less handler with a no-op event callback and swaps in the real handler (real secret, real event logging) the instant the create/patch call finishes — nothing genuine can reach the tunnel URL in that gap since no subscription points at it yet, so the only thing that can hit this initial handler is Mollie's own URL-validation ping, which the no-op callback correctly keeps out of the tail output.
4. Apply the subscription resolution above, using the tunnel URL. The `POST`/`PATCH` call that actually points a subscription at the tunnel URL is retried a bounded number of times (5 attempts, 2s apart) before giving up — a `cloudflared` quick tunnel's DNS record isn't always resolvable the instant the URL is reported, so the very first attempt can fail transiently for reasons unrelated to the request itself.
5. Enter tail mode.
6. On SIGINT/SIGTERM (`signal.NotifyContext`): restore (foreign case) or delete (ours/fresh case) the subscription, terminate the `cloudflared` subprocess (SIGTERM, short grace period, then kill), clear the state file's pending-restore entry on success.

## Signature Verification

The `webhookSecret` is only ever returned in a `POST /webhooks` **create** response — never on `GET`/`PATCH`/`LIST`. This means:

- **Free slot / ours** (always freshly created) → secret is known once the create call succeeds → verify every incoming `X-Mollie-Signature` header as HMAC-SHA256 over the raw request body, compared with `hmac.Equal` (timing-safe). Requests that fail verification are still logged, but clearly marked invalid, and are not treated as authentic Mollie events. Between the server starting and the create call succeeding, the handler briefly runs secret-less with a no-op event callback (see Tunnel Lifecycle above) — nothing genuine can reach the tunnel during that window, so the only thing that can hit it is Mollie's own URL-validation ping, which is plumbing, not an event, and shouldn't show up in the tail output as one.
- **Foreign (patched in place)** → secret is never known to us (rotating it would require deleting and recreating the subscription, which would silently break the secret its actual owner uses to verify it downstream — permanently, even after we restore the URL — so we never do this by default) → verification is skipped; every event in this mode is tagged `unverified` in the tail output, and a one-time warning banner is shown when the session starts in this mode.

## Tail Output (v1)

Simple scrolling log lines — one per event as it arrives: timestamp, event type, entity ID, and a verified/unverified badge. The full JSON body is available from day one (Next-gen Webhooks deliver the full payload, not just an ID), so richer inspection (e.g. `-v` full-body dump, filtering, a TUI) can be layered on later without changing the receiving/verification plumbing.

This is the only thing v1 does with an incoming request: log it and respond `200 OK` to Mollie. Nothing is relayed anywhere else.

## Milestone 2 — `--forward <url>`

Deferred, not built in v1. Modeled on `stripe listen --forward-to`, simplified: Mollie's Next-gen Webhooks don't have Stripe's snapshot/thin/Connect event distinctions, so a single `--forward` flag and a single target URL is enough — no need for the four separate `--forward-*-to` flags Stripe has.

Sketch of the intended behavior:

- `mollie webhook-tunnel --forward http://localhost:3000/webhooks` — in addition to logging, every incoming request is also re-issued as a new `POST` to the forward target: same raw body, `Content-Type`, and `X-Mollie-Signature` header passed through unchanged.
- **Fire-and-forget, not blocking the ack to Mollie.** We still respond `200 OK` to Mollie immediately after logging (and verifying, where possible) — the forward happens as a separate outgoing request with its own short timeout (e.g. 10s). This avoids coupling Mollie's delivery/retry behavior to how fast or reliable the developer's local app is.
- The forward's outcome (status code, or connection error/timeout) is appended to that event's log line, so failures to reach the local app are visible without affecting delivery from Mollie's side.
- Forwarding happens regardless of verification outcome (including the "foreign, unverified" subscription case) — forwarding and verification are orthogonal; an unverified event is still forwarded, just still logged as unverified.
- When the signing secret is known (free-slot/"ours" case), print it prominently in the startup banner so the user can paste it into their local app's own signature verification, mirroring Stripe's `whsec_...` UX.

**Known limitation vs. Stripe:** Stripe's local `whsec_...` stays stable across `listen` restarts. Ours can't — the Mollie API never lets us set or re-read a subscription's secret, and our "ours" resolution deletes and recreates that subscription every run (see "Subscription Resolution" above), which mints a new secret each time. A developer using `--forward` would need to re-copy the printed secret into their local `.env` every session. There's no API-level way around this today; worth revisiting if Mollie ever exposes secret rotation or read access.

## Files Changed

| File | Change |
|---|---|
| `cmd/webhooktunnel.go` | New — command definition, flags, orchestration of the lifecycle above. Reuses `whClient` from `cmd/webhooks.go` (same package). |
| `internal/tunnel/cloudflared.go` | New — spawn/monitor/terminate the `cloudflared` subprocess, parse the tunnel URL. |
| `internal/tunnelstate/tunnelstate.go` | New — read/write `webhook-tunnel-state.json`. |
| `internal/webhookevents/events.go` | New — static event-type/permission table and `Resolve()`. |
| `internal/prompt/prompt.go` | Add a generic list-selection helper (modeled on `ProfileSelect`) for picking which foreign subscription to overwrite. |
| `internal/mollieclient/mollieclient.go` | No change expected — `New()` already builds an SDK client suitable for `client.Permissions.List()`. |

## Out of Scope (v1)

- Request forwarding to a local app (`--forward`) — deferred to Milestone 2, see above.
- Live mode support (explicit follow-up design once test mode has proven out).
- Auto-downloading/installing `cloudflared`.
- Beta webhook event types.
- Exposing permission-aware `--event-types all-available` on the existing `webhooks create`/`update` commands.
- Rich/TUI tail output (bubbletea dashboard) — noted as a natural future iteration given the full JSON payload is already available.
- Auto-picking a port fallback if `--port` is already in use (errors instead).
- A manual `mollie webhook-tunnel restore` subcommand — recovery is auto-detected on next start instead.

## Addendum (2026-08-06): retry backoff revamp + full webhook call logging

Two small follow-ups after initial shipping, based on real usage: the fixed 5×2s DNS-propagation retry occasionally wasn't enough for slower Cloudflare propagation, and there was no durable record of raw incoming webhook calls for debugging.

**Retry backoff.** `webhookURLRetryAttempts`/`webhookURLRetryDelay` (`cmd/webhooktunnel.go:161-195`) become 10 attempts with a linear increasing delay of `attempt` seconds between them (1s, 2s, ..., 9s — 9 waits summing to exactly 45s). The retry loop is split into a pure, injectable-delay `retryWithBackoff` engine plus a `retryDelay` function, so tests don't have to sleep for real seconds; `retryTunnelWebhookCall` becomes a thin wrapper preserving its existing call sites. Per-attempt feedback is upgraded to show the upcoming wait and elapsed time.

**Webhook call logging.** New `--logfile` flag (default `/tmp/mollie-webhook-log`) always appends a raw, HTTP-wire-shaped record of every incoming call — including Mollie's own validation ping, since this is a diagnostic artifact distinct from the console event tail — to that file: timestamp, method, URL, Host, all headers (Cloudflare-forwarded ones included as received), and the raw body. `webhookserver.Event` gains `Method`, `URL`, `Host`, and `Header` fields (additive, populated from the request in `Handler`). This deliberately writes to `/tmp` rather than `config.Dir()` per explicit request, and always opens in append mode so repeated runs accumulate rather than overwrite.
