# mollie-cli

A command-line interface for the [Mollie](https://www.mollie.com/) payment platform, aimed at developers building and debugging Mollie integrations. Wraps the full Mollie REST API so you can create, inspect, and manage payment resources without writing throwaway scripts or crafting raw HTTP requests.

⚠️ This is not an official Mollie tool. Use at your own risk. ⚠️

## Installation

You'll need a somewhat current instance of `go`. Download or clone the repository, then build and put the binary somewhere in your `$PATH`, e.g.:

```bash
git clone https://github.com/fjbender/mollie-cli && cd mollie-cli && go build -o ~/bin/mollie
```

(Assuming the `~/bin` directory is in your `$PATH`)

## Quick start

```bash
mollie auth setup          # interactive first-run: paste token, pick a profile
mollie payments list       # list test payments
mollie payments list --live  # list live payments

# Work with multiple environments (e.g. separate test / live credentials)
mollie env copy default production  # copy settings, swap key later
mollie --env production auth setup  # configure the new environment
mollie env switch production        # make it the active environment
mollie env list                     # see all environments
```

## Authentication

Run `mollie auth setup` to get started. The setup wizard:

1. Prompts for a **Mollie Organization Access Token** (`access_…`) — input is masked.
2. Validates the token against `GET /v2/organizations/me` _in test mode_ before saving it; the key is never written to disk if validation fails.
3. Lets you pick a default profile from a list fetched from the API.

Other auth subcommands:

| Command | Description |
|---|---|
| `mollie auth status` | Show the active environment, key, profile, and output format |
| `mollie auth clear` | Clear the API key and profile ID for the active environment (use `mollie env delete` to remove the environment entirely) |

All `auth` subcommands operate on the **active environment**. Pass `--env <name>` to target a specific one:

```bash
mollie --env production auth setup    # configure a non-active environment
mollie --env production auth status   # inspect its credentials
```

## Global flags

These flags work on every command:

| Flag | Env var | Description |
|---|---|---|
| `--live`, `-l` | `MOLLIE_LIVE_MODE=true` | Operate on the live environment (default: test mode) |
| `--output`, `-o` | — | Output format: `table` (default), `json`, `yaml` |
| `--env`, `-e` | — | Use a specific config environment for this invocation (overrides active environment) |
| `--profile` | — | Override the profile ID for this invocation |
| `--api-key` | — | Override the stored API key for this invocation |
| `--no-color` | `NO_COLOR` | Disable ANSI colour output |

**Test mode is always on by default.** Tables printed in test mode are prefixed with a `[TEST]` badge so the active environment is always unambiguous. Pass `--live` (or set `MOLLIE_LIVE_MODE=true`) to operate on live data.

## Configuration

Configuration is stored in TOML format at:

```
$XDG_CONFIG_HOME/mollie-cli/config.toml   # if XDG_CONFIG_HOME is set
~/.config/mollie-cli/config.toml           # fallback
```

The file is managed by the CLI — editing it by hand is supported but not required. Each named environment is stored as a section in the file:

```toml
active_environment = 'default'

[environments.default]
api_key = 'access_…'
profile_id = 'pfl_…'
live_mode = false
output = 'table'

[environments.production]
api_key = 'access_…'
profile_id = 'pfl_…'
live_mode = false
output = 'table'
```

> **Migration**: if your config file was created with an older version of `mollie-cli` it will be automatically upgraded to this multi-environment format on first use. No action required.

### Environment variable overrides

Every config key can be overridden with a `MOLLIE_<KEY>` environment variable, so credentials and settings can be injected from CI/CD without touching the config file:

```bash
MOLLIE_API_KEY=access_xyz mollie payments list
MOLLIE_LIVE_MODE=true mollie payments list
```

## Environments

Config environments let you maintain multiple sets of credentials and defaults under named, easily switchable profiles.

### Concepts

- The **active environment** is used by every command unless overridden with `--env`.
- On first run, a `default` environment is created automatically.
- Each environment holds its own API key, profile ID, live-mode setting, output format, and payment defaults.

### Managing environments

```bash
mollie env list                       # show all environments; ✓ marks the active one
mollie env create staging             # create an empty environment
mollie env copy default production    # duplicate an environment (useful to swap only the API key)
mollie env switch production          # make production the active environment
mollie env delete staging             # permanently remove an environment (interactive confirmation)
```

`env create` offers to run interactive setup immediately. You can also configure a non-active environment at any time:

```bash
mollie --env production auth setup    # set up credentials without switching first
```

### Per-invocation override

The `--env` flag (short: `-e`) overrides the active environment **for a single invocation only** — the `active_environment` pointer in the config file is not modified:

```bash
mollie --env production payments list   # query production without switching
mollie --env staging auth status        # inspect staging credentials
```

## Defaults

Frequently used create-command parameters (description, amount, currency, redirect URL, webhook URL) can be saved as defaults so you don't have to retype them on every invocation.

```bash
# Interactive form (pre-filled with current values):
mollie defaults set

# Non-interactive:
mollie defaults set --amount 9.99 --currency EUR --description "Test order" \
  --redirect-url https://example.com/thanks

mollie defaults show        # print active defaults
mollie defaults unset       # interactive multi-select to clear specific defaults
mollie defaults unset --all # wipe every default at once
```

Defaults act as **fallbacks** — an explicit flag always takes priority over a stored default. Commands that honour defaults accept the same parameters (`--amount`, `--currency`, `--description`, `--redirect-url`, `--webhook-url`) and fall back to the stored values when those flags are omitted.

## Output formats

All commands support three output formats via `--output` / `-o`:

| Format | Flag | Notes |
|---|---|---|
| Table | `-o table` (default) | Colour-coded, human-readable, tab-aligned |
| JSON | `-o json` | Pretty-printed full API response object |
| YAML | `-o yaml` | Pretty-printed full API response object |

The default format can be persisted in the config file (`output = "json"`) or set interactively through `mollie auth setup`.

## Pagination

List commands use **cursor-based pagination** via `--from`. Pass the ID of the last item from the previous page to fetch the next one:

```bash
mollie payments list --limit 5
mollie payments list --limit 5 --from tr_lastIdFromPreviousPage
```

## Destructive operations

Commands that delete or cancel a resource require confirmation. Without `--confirm` the CLI shows an interactive yes/no prompt; pass `--confirm` to skip it in scripts:

```bash
mollie payments cancel tr_abc123 --confirm
mollie refunds cancel tr_abc123 re_xyz789 --confirm
mollie customers delete cst_abc123 --confirm
mollie mandates revoke cst_abc123 mdt_xyz789 --confirm
```

## Command reference

### `auth` — credentials

```
mollie auth setup     # interactive setup for the active (or --env) environment
mollie auth status    # show environment name, key, profile, and output format
mollie auth clear     # clear API key + profile ID for the active environment
```

### `env` — configuration environments

```
mollie env list
mollie env create [name]
mollie env copy <source> [destination]
mollie env switch <name>
mollie env delete <name>
```

### `payments` — payments

```
mollie payments create [flags]
mollie payments list [--limit N] [--from <id>]
mollie payments get <payment-id>
mollie payments update <payment-id> [flags]
mollie payments cancel <payment-id> [--confirm]
```

#### `payments create` advanced flags

`--with-lines` auto-generates order lines that sum to `--amount`. Optionally split out a separate shipping line:

```bash
mollie payments create \
  --amount 24.99 --currency EUR --description "Order #42" \
  --redirect-url https://example.com/thanks \
  --with-lines --lines-vat-rate 21.00 --lines-shipping-amount 4.99
```

`--with-billing` and `--with-shipping` attach address objects to the payment. Without additional flags a Dutch test address is used as a placeholder; override individual fields with `--billing-*` / `--shipping-*`:

```bash
mollie payments create ... \
  --with-billing \
  --billing-given-name Jane --billing-family-name Doe \
  --billing-email jane@example.com --billing-country NL
```

`--metadata` attaches arbitrary JSON to the payment:

```bash
mollie payments create ... --metadata '{"order_id": "42", "source": "cli"}'
```

### `refunds` — refunds

```
mollie refunds create <payment-id> [--amount N --currency C --description D]
mollie refunds list <payment-id>
mollie refunds list-all
mollie refunds get <payment-id> <refund-id>
mollie refunds cancel <payment-id> <refund-id> [--confirm]
```

Omitting `--amount` on `refunds create` refunds the full payment amount.

### `chargebacks` — chargebacks

```
mollie chargebacks list <payment-id>
mollie chargebacks list-all
mollie chargebacks get <payment-id> <chargeback-id>
```

### `captures` — captures

```
mollie captures create <payment-id> [flags]
mollie captures list <payment-id>
mollie captures get <payment-id> <capture-id>
```

### `customers` — customers

```
mollie customers create [flags]
mollie customers list
mollie customers get <customer-id>
mollie customers update <customer-id> [flags]
mollie customers delete <customer-id> [--confirm]
mollie customers payments create <customer-id> [flags]
mollie customers payments list <customer-id>
```

### `mandates` — mandates

```
mollie mandates create <customer-id> [flags]
mollie mandates list <customer-id>
mollie mandates get <customer-id> <mandate-id>
mollie mandates revoke <customer-id> <mandate-id> [--confirm]
```

### `subscriptions` — subscriptions

```
mollie subscriptions create <customer-id> [flags]
mollie subscriptions list <customer-id>
mollie subscriptions list-all
mollie subscriptions get <customer-id> <subscription-id>
mollie subscriptions update <customer-id> <subscription-id> [flags]
mollie subscriptions cancel <customer-id> <subscription-id> [--confirm]
mollie subscriptions payments <customer-id> <subscription-id>
```

### `payment-links` — payment links

```
mollie payment-links create [flags]
mollie payment-links list
mollie payment-links get <payment-link-id>
mollie payment-links update <payment-link-id> [flags]
mollie payment-links delete <payment-link-id> [--confirm]
mollie payment-links payments <payment-link-id>
```

### `balances` — balances

```
mollie balances list [--currency EUR]
mollie balances get <balance-id>
mollie balances primary
mollie balances report <balance-id> --from YYYY-MM-DD --until YYYY-MM-DD
mollie balances transactions <balance-id>
```

### `settlements` — settlements

```
mollie settlements list
mollie settlements get <settlement-id>
mollie settlements open
mollie settlements next
mollie settlements payments <settlement-id>
mollie settlements refunds <settlement-id>
mollie settlements captures <settlement-id>
mollie settlements chargebacks <settlement-id>
```

### `methods` — payment methods

```
mollie methods list [--sequence-type <type>] [--amount-value N --amount-currency C] ...
mollie methods list-all
mollie methods get <method-id>
```

### `profiles` — profiles

```
mollie profiles list
mollie profiles get <profile-id>
mollie profiles current
mollie profiles create [flags]
mollie profiles update <profile-id> [flags]
mollie profiles delete <profile-id> [--confirm]
```

### `organizations` — organizations

```
mollie organizations current
mollie organizations get <organization-id>
```

### `invoices` — invoices

```
mollie invoices list
mollie invoices get <invoice-id>
```

### `sessions` — sessions _(beta)_

Sessions support checkout flows built with Mollie Components. Supports the same `--with-lines`, `--with-billing`, and `--with-shipping` flags as `payments create`. The Sessions API specification may still change.

```
mollie sessions create [flags]
mollie sessions get <session-id>
```

### `defaults` — stored defaults

```
mollie defaults set [--amount N] [--currency C] [--description D] \
                    [--redirect-url U] [--webhook-url U]
mollie defaults show
mollie defaults unset [--all]
```

## Development

```bash
go build ./...
go test ./...
go vet ./...
golangci-lint run
```
