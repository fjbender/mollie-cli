# Functional Requirements Document — Mollie CLI

## Overview

`mollie-cli` is a command-line interface aimed at developers building integrations with the [Mollie](https://www.mollie.com/) payment platform. It wraps the Mollie REST API and provides ergonomic commands for creating, inspecting, and managing Mollie resources without having to craft raw HTTP requests or write throwaway scripts.

The primary user persona is a **backend developer** who is actively integrating Mollie into an application and needs to quickly create test data, inspect live or test resources, debug webhook flows, and validate their integration logic from the terminal.

---

## Design Principles

- **Developer-first UX** — Commands should be obvious, flags should be self-documenting (`--help` is always accurate), and output should be easy to pipe into other tools.
- **Test-mode by default** — All commands run against Mollie's test environment unless `--live` is explicitly passed (or `MOLLIE_LIVE_MODE=true` is set). This prevents accidental mutations on live data during development.
- **Safe by default** — Destructive operations (delete, cancel, revoke) require explicit confirmation or a `--confirm` flag.
- **Composable output** — Default output is a human-readable table. `--output json` emits clean JSON for use with tools like `jq`.
- **Config over repetition** — Credentials, default profile IDs, and test-mode preference are stored in a config file so they need not be supplied on every invocation.

---

## Milestones

### Milestone 1 — Foundation & Authentication

The first milestone lays the groundwork that every subsequent command depends on.

#### Goals
- Project scaffolding (repository structure, module, CI skeleton).
- First-run experience: detect missing credentials and prompt the user to configure them interactively.
- Persistent configuration storage.
- Global flags available on every command.

#### Features

**1.1 First-run setup (`mollie auth setup`)**
- Detect on startup whether an API key is already configured.
- If not, show a clear, friendly prompt asking the user to paste their Organization Access Token (format `access_*`).
- Validate the token with a lightweight API call (`GET /v2/organizations/me`) before storing it.
- Inform the user where the config file was written.

**1.2 Config management (`mollie auth`)**
- `mollie auth setup` — Interactive first-run flow (see above).
- `mollie auth status` — Show the currently configured token (masked, e.g. `access_****xyz`), the active profile ID (if set), and whether live mode is enabled (test mode is the default).
- `mollie auth clear` — Remove stored credentials.

**1.3 Configuration file**
- Stored at `$XDG_CONFIG_HOME/mollie-cli/config.toml` (fallback: `~/.config/mollie-cli/config.toml`).
- Fields: `api_key`, `profile_id` (optional), `live_mode` (default: `false`), `default_output` (default: `table`).

**1.4 Global flags (all commands)**

| Flag | Short | Description |
|---|---|---|
| `--live` | `-l` | Run against the live Mollie environment. By default all commands run in test mode. |
| `--output <format>` | `-o` | Output format: `table` (default), `json`, `yaml`. |
| `--profile <id>` | | Override the profile ID for this invocation. |
| `--api-key <key>` | | Override the stored API key for this invocation. |
| `--no-color` | | Disable ANSI color output. |

**Acceptance Criteria**
- Running any command without a configured API key shows a clear error with instructions to run `mollie auth setup`.
- Running `mollie auth setup` with an invalid token shows an error and does **not** persist the key.
- Config file is created with restrictive file permissions (`0600`).
- Commands running in test mode display a subtle `[TEST]` indicator in table output so the active environment is always unambiguous.

---

### Milestone 2 — Core Payment Resources

Developers spend most of their time working with payments, refunds, and captures. This milestone covers the full lifecycle of these three resources.

#### Payments (`mollie payments`)

| Command | Description |
|---|---|
| `mollie payments create` | Create a new payment (interactive flag-based flow). |
| `mollie payments list` | List payments with optional filters. |
| `mollie payments get <id>` | Fetch and display a single payment. |
| `mollie payments update <id>` | Update mutable fields (description, redirect URL, metadata). |
| `mollie payments cancel <id>` | Cancel a cancelable payment. |

Key flags for `payments create`:
- `--amount <value>` (required, e.g. `10.00`)
- `--currency <code>` (required, e.g. `EUR`)
- `--description <text>` (required)
- `--redirect-url <url>` (required)
- `--method <method>` (optional, e.g. `ideal`, `creditcard`)
- `--customer-id <id>` (optional, links payment to a Customer)
- `--sequence-type <type>` (optional: `oneoff`, `first`, `recurring`)
- `--webhook-url <url>` (optional)
- `--metadata <json>` (optional JSON string)

Key flags for `payments list`:
- `--limit <n>` (max results per page, default 50)
- `--from <id>` (cursor pagination)

#### Refunds (`mollie refunds`)

| Command | Description |
|---|---|
| `mollie refunds create <payment-id>` | Create a refund for a payment. |
| `mollie refunds list <payment-id>` | List refunds for a payment. |
| `mollie refunds list-all` | List all refunds across all payments. |
| `mollie refunds get <payment-id> <refund-id>` | Get a single refund. |
| `mollie refunds cancel <payment-id> <refund-id>` | Cancel a queued refund. |

Key flags for `refunds create`:
- `--amount <value>` (optional, defaults to full payment amount)
- `--currency <code>` (required if `--amount` is set)
- `--description <text>` (optional)

#### Captures (`mollie captures`)

| Command | Description |
|---|---|
| `mollie captures create <payment-id>` | Create a capture for an authorized payment. |
| `mollie captures list <payment-id>` | List captures for a payment. |
| `mollie captures get <payment-id> <capture-id>` | Get a single capture. |

**Acceptance Criteria**
- `payments create` outputs the new payment ID and checkout URL on success.
- All list commands support `--limit` and `--from` flags.
- Destructive commands (`cancel`) ask for `--confirm` or prompt interactively.
- `--output json` produces valid, pretty-printed JSON matching the API response shape.

---

### Milestone 3 — Customers & Recurring Payments

This milestone covers the customer, mandate, and subscription resources that power Mollie's recurring payment flows.

#### Customers (`mollie customers`)

| Command | Description |
|---|---|
| `mollie customers create` | Create a new customer. |
| `mollie customers list` | List customers. |
| `mollie customers get <id>` | Get a customer. |
| `mollie customers update <id>` | Update a customer's name/email/metadata. |
| `mollie customers delete <id>` | Delete a customer. |
| `mollie customers payments create <id>` | Create a payment for a customer. |
| `mollie customers payments list <id>` | List payments for a customer. |

#### Mandates (`mollie mandates`)

| Command | Description |
|---|---|
| `mollie mandates create <customer-id>` | Create a mandate. |
| `mollie mandates list <customer-id>` | List mandates for a customer. |
| `mollie mandates get <customer-id> <mandate-id>` | Get a mandate. |
| `mollie mandates revoke <customer-id> <mandate-id>` | Revoke a mandate. |

#### Subscriptions (`mollie subscriptions`)

| Command | Description |
|---|---|
| `mollie subscriptions create <customer-id>` | Create a subscription. |
| `mollie subscriptions list <customer-id>` | List subscriptions for a customer. |
| `mollie subscriptions list-all` | List all subscriptions. |
| `mollie subscriptions get <customer-id> <sub-id>` | Get a subscription. |
| `mollie subscriptions update <customer-id> <sub-id>` | Update a subscription. |
| `mollie subscriptions cancel <customer-id> <sub-id>` | Cancel a subscription. |
| `mollie subscriptions payments <customer-id> <sub-id>` | List payments generated by a subscription. |

**Acceptance Criteria**
- `customers delete` and `mandates revoke` require `--confirm`.
- Subscription `create` prompts for at minimum: `--amount`, `--currency`, `--interval`, `--description`.

---

### Milestone 4 — Financial Insights

This milestone focuses on read-only reporting commands that help developers and finance teams understand the current financial state of the account.

#### Balances (`mollie balances`)

| Command | Description |
|---|---|
| `mollie balances list` | List all balances. |
| `mollie balances get <id>` | Get a specific balance. |
| `mollie balances primary` | Get the primary balance. |
| `mollie balances report <id>` | Get a balance report (supports `--from` and `--until` date flags). |
| `mollie balances transactions <id>` | List transactions for a balance (paginated). |

#### Settlements (`mollie settlements`)

| Command | Description |
|---|---|
| `mollie settlements list` | List settlements. |
| `mollie settlements get <id>` | Get a settlement. |
| `mollie settlements open` | Get the open (not-yet-settled) settlement. |
| `mollie settlements next` | Get the next scheduled settlement. |
| `mollie settlements payments <id>` | List payments in a settlement. |
| `mollie settlements refunds <id>` | List refunds in a settlement. |
| `mollie settlements captures <id>` | List captures in a settlement. |
| `mollie settlements chargebacks <id>` | List chargebacks in a settlement. |

#### Chargebacks (`mollie chargebacks`)

| Command | Description |
|---|---|
| `mollie chargebacks list <payment-id>` | List chargebacks for a specific payment. |
| `mollie chargebacks list-all` | List all chargebacks. |
| `mollie chargebacks get <payment-id> <chargeback-id>` | Get a single chargeback. |

**Acceptance Criteria**
- Date range flags (`--from`, `--until`) accept `YYYY-MM-DD` format.
- Balance transaction listing auto-paginates when `--all` is passed.

---

### Milestone 5 — Extended Resources

This milestone rounds out the API surface with resources that are commonly needed during integration development.

#### Payment Links (`mollie payment-links`)

| Command | Description |
|---|---|
| `mollie payment-links create` | Create a payment link. |
| `mollie payment-links list` | List payment links. |
| `mollie payment-links get <id>` | Get a payment link. |
| `mollie payment-links update <id>` | Update a payment link. |
| `mollie payment-links delete <id>` | Delete a payment link. |
| `mollie payment-links payments <id>` | List payments created from a link. |

#### Payment Methods (`mollie methods`)

| Command | Description |
|---|---|
| `mollie methods list` | List enabled payment methods for the profile. |
| `mollie methods list-all` | List all available payment methods. |
| `mollie methods get <id>` | Get details for a specific method. |

#### Profiles (`mollie profiles`)

| Command | Description |
|---|---|
| `mollie profiles list` | List profiles. |
| `mollie profiles get <id>` | Get a profile. |
| `mollie profiles current` | Get the profile linked to the current API key. |
| `mollie profiles create` | Create a profile. |
| `mollie profiles update <id>` | Update a profile. |
| `mollie profiles delete <id>` | Delete a profile. |

#### Organizations (`mollie organizations`)

| Command | Description |
|---|---|
| `mollie organizations current` | Get the organization linked to the current API key. |
| `mollie organizations get <id>` | Get an organization by ID. |

#### Invoices (`mollie invoices`)

| Command | Description |
|---|---|
| `mollie invoices list` | List invoices. |
| `mollie invoices get <id>` | Get an invoice. |

---

### Milestone 6 — Webhooks

This milestone provides full management of Mollie's next-gen Webhooks API and tooling to make local webhook development easier.

#### Webhook Management (`mollie webhooks`)

| Command | Description |
|---|---|
| `mollie webhooks create` | Register a new webhook. |
| `mollie webhooks list` | List all registered webhooks. |
| `mollie webhooks get <id>` | Get a webhook. |
| `mollie webhooks update <id>` | Update a webhook URL or event subscriptions. |
| `mollie webhooks delete <id>` | Delete a webhook. |
| `mollie webhooks test <id>` | Send a test event to a webhook. |

#### Webhook Events (`mollie webhooks events`)

| Command | Description |
|---|---|
| `mollie webhooks events get <event-id>` | Get a specific webhook event by ID. |

#### Local Webhook Forwarding (`mollie webhooks forward`) *(stretch goal within this milestone)*

- Start a local HTTP listener on a specified port.
- Use a tunneling mechanism (e.g. a Cloudflare Tunnel or `ngrok`-compatible approach) or accept a pre-existing public URL as input.
- Register the public URL as a Mollie webhook automatically.
- Forward received webhook payloads to a local development server URL.
- Print each incoming webhook event to stdout for inspection.

**Acceptance Criteria**
- `webhooks test` displays the HTTP response status and body returned by the target URL.
- `webhooks create` requires `--url` and `--events` flags.
- `webhooks forward` cleans up the registered webhook on exit (SIGINT/SIGTERM).

---

### Milestone 7 — Developer Experience Improvements

This milestone focuses entirely on quality-of-life improvements rather than new API surface.

#### Tab Autocompletion
- Generate shell completion scripts for `bash`, `zsh`, `fish`, and PowerShell via `mollie completion <shell>`.
- Completions cover subcommands and static flag values (e.g. `--output` completing to `json`, `table`, `yaml`; `--currency` completing to common ISO 4217 codes).

#### Interactive Mode
- For create/update commands, if required flags are not provided, drop into an interactive prompt-based flow using a TUI library.
- Allow `--interactive` / `-i` to force the interactive flow even when flags are provided.

#### Output Enhancements
- `--output yaml` support alongside existing `table` and `json`.
- Wide table format with `--wide` flag that shows additional columns hidden by default.
- `--quiet` / `-q` flag that suppresses all output except the resource ID (useful in scripts).

#### Configuration Profiles
- Support multiple named configurations (e.g. `staging`, `production`) via `mollie config use <profile-name>`.
- Config file groups credentials and defaults under named sections.

**Acceptance Criteria**
- Completion script can be sourced and provides completions in all supported shells.
- `--quiet` on a create command outputs only the created resource's ID and exits with code 0.

---

### Milestone 8 — Future / TBD

The following areas are identified for future exploration and will be scoped in detail as earlier milestones are completed.

- **Mollie Connect / OAuth tooling** — Commands for managing clients, client links, and partner status, targeting ISVs and platforms built on top of Mollie. With automatic access token renewal
- **Terminals (Point of Sale)** — List and inspect POS terminals.
- **Sales Invoices** — Full CRUD for the Sales Invoices API.
- **Onboarding** — Get onboarding status and submit onboarding data on behalf of a client.
- **Capabilities** — List capabilities for an account.
- **Delayed Routing** — Create and inspect payment routes.
- **Diff / watch mode** — `mollie payments watch <id>` polls and displays live status changes for a payment, useful during integration testing.
- **Import / seed commands** — Bulk-create test data (customers, payments) from a JSON or YAML seed file, enabling repeatable test environments.
- **OpenTelemetry tracing** — Optional structured trace output for debugging latency in API calls.

---

## Non-Goals (v1)

- No OAuth flow support (Organizational Access Tokens only).
- No support for the deprecated Orders API or Shipments API.
- No browser-based UI or TUI dashboard.
- No persistent local database of API responses.
