---
name: mollie-cli
description: Use when working with the mollie CLI — creating payments, managing customers, configuring auth, switching environments, or piping API output between commands
---

# mollie CLI

## Overview

`mollie` is a Cobra-based CLI for the Mollie payments API. **Test mode is on by default** — live mutations require an explicit `--live` flag. All global flags (`--live`, `--output`, `--env`, `--no-color`) must come **before** the subcommand.

Precedence chain: **CLI flags > stdin JSON > stored defaults > API defaults**

## Auth & Environment Setup

```bash
mollie auth setup                        # interactive first-run wizard (paste token, pick profile)
mollie auth status                       # show active env, masked key, profile, output format
mollie auth clear                        # remove credentials for active environment

mollie env list
mollie env create <name>
mollie env copy <src> <dst>
mollie env switch <name>                 # permanently switch active environment
mollie env delete <name>

# Override for a single command (global flag — before subcommand):
mollie --env production payments list
mollie -e staging payments get tr_abc
```

## Stored Defaults

```bash
mollie defaults set --amount 10.00 --currency EUR --description "Test" \
  --redirect-url https://example.com/return --webhook-url https://example.com/wh
mollie defaults show
mollie defaults unset                    # interactive
mollie defaults unset --all             # clear everything
```

## Quick Reference

| Resource | Commands |
|----------|----------|
| `payments` | `create` `list` `get <id>` `update <id>` `cancel <id>` |
| `refunds` | `create <payment-id>` `list <payment-id>` `list-all` `get <payment-id> <refund-id>` `cancel <payment-id> <refund-id>` |
| `captures` | `create <payment-id>` `list <payment-id>` `get <payment-id> <capture-id>` |
| `chargebacks` | `list <payment-id>` `list-all` `get <payment-id> <chargeback-id>` |
| `customers` | `create` `list` `get <id>` `update <id>` `delete <id>` |
| `customers payments` | `create <customer-id>` `list <customer-id>` |
| `mandates` | `create <customer-id>` `list <customer-id>` `get <customer-id> <mandate-id>` `revoke <customer-id> <mandate-id>` |
| `subscriptions` | `create <customer-id>` `list <customer-id>` `list-all` `get <cid> <sid>` `update <cid> <sid>` `cancel <cid> <sid>` `payments <cid> <sid>` |
| `payment-links` | `create` `list` `get <id>` `update <id>` `delete <id>` `payments <id>` |
| `sessions` | `create` `get <id>` |
| `balances` | `list` `get <id>` `primary` `report <id>` `transactions <id>` |
| `settlements` | `list` `get <id>` `open` `next` `payments <id>` `refunds <id>` `captures <id>` `chargebacks <id>` |
| `methods` | `list` `list-all` `get <id>` |
| `profiles` | `list` `get <id>` `current` `create` `update <id>` `delete <id>` |
| `organizations` | `current` `get <id>` |
| `invoices` | `list` `get <id>` |
| `terminals` | `list` `get <id>` |

## Common Workflows

### Create a payment (minimal)
```bash
mollie payments create --amount 25.00 --currency EUR \
  --description "My order" --redirect-url https://example.com/return
```

### Create a payment with auto-generated order lines
```bash
# --with-lines generates 2 item lines + 1 shipping line summing exactly to --amount
# --with-discount appends a ~10% discount line (requires --with-lines)
mollie payments create --amount 25.00 --currency EUR \
  --description "Order" --redirect-url https://example.com/return \
  --with-lines --with-discount -o json
```

Optional line tuning flags: `--lines-vat-rate 21.00` (default), `--lines-shipping-amount 4.99` (default)

### Refund a payment
```bash
# Full refund (payment-id is a positional argument):
mollie refunds create tr_abc123

# Partial refund:
mollie refunds create tr_abc123 --amount 10.00 --currency EUR
```

### Pipe: create payment → get its ID → create refund
```bash
ID=$(mollie payments create --amount 5.00 --currency EUR \
  --description "Test" --redirect-url https://example.com -o json | jq -r '.id')
mollie refunds create "$ID"
```

### Pagination
```bash
mollie payments list --limit 5
mollie payments list --limit 5 --from tr_lastIdFromPreviousPage  # cursor-based
```

### Recurring: customer → mandate → subscription
```bash
# 1. Create customer
mollie customers create --name "Jan Janssen" --email jan@example.com

# 2. Create a first-payment to collect mandate
mollie payments create --amount 0.01 --currency EUR \
  --description "Mandate" --redirect-url https://example.com/return \
  --customer-id cst_xyz --sequence-type first

# 3. Create subscription (customer-id is positional):
mollie subscriptions create cst_xyz \
  --amount 9.99 --currency EUR \
  --interval "1 month" --description "Monthly plan"
```

Interval examples: `"1 month"`, `"14 days"`, `"1 year"`

### Live mode
```bash
# Single command:
mollie --live payments list
mollie -l payments create --amount 10.00 --currency EUR ...

# Or via env var:
MOLLIE_LIVE_MODE=true mollie payments list
```

## Output Formats & Piping

```bash
mollie payments list -o json   # or --output json
mollie payments list -o yaml
mollie payments list -o table  # default; colored, [TEST] badge in test mode

# Pipe a payment response as stdin into another create command:
mollie payments get tr_abc -o json | mollie payments create
# Explicit flags always override stdin values
```

Piping disables color automatically (non-TTY detection). Also: `--no-color` or `NO_COLOR=1`.

## Safety Features

- **Test mode by default** — all commands operate on test data unless `--live` / `-l` is set
- Table output shows `[TEST]` badge as a reminder
- `test_*` API keys are always test-only; `live_*` keys are always live
- Destructive operations (`cancel`, `delete`, `revoke`) prompt for confirmation unless `--confirm` is passed

## Common Mistakes

| Mistake | Correct |
|---------|---------|
| `mollie payments --env prod list` | `mollie --env prod payments list` (global flags before subcommand) |
| `mollie config set amount 10.00` | `mollie defaults set --amount 10.00` |
| `mollie refunds create --payment-id tr_x` | `mollie refunds create tr_x` (positional arg) |
| `mollie subscriptions create --customer-id cst_x` | `mollie subscriptions create cst_x` (positional arg) |
| `--order-lines '[{...}]'` | `--with-lines` (auto-generates; no manual JSON needed) |
| `mollie payments refund create tr_x` | `mollie refunds create tr_x` |
