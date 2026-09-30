# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- GPL v3 `LICENSE`
- `mollie --version`, showing version, commit and build date
- Homebrew formula published to `fjbender/homebrew-tap` on each release (`brew install fjbender/tap/mollie`)
- CI job that snapshot-builds the release to catch GoReleaser config errors on pull requests

### Changed
- README installation section now covers Homebrew, release binaries, `go install` and building from source

## [v0.8.0] — 2026-09-21

### Added
- `sales-invoices` command: `create`, `list`, `get`, `update`, `delete` ([#10](https://github.com/fjbender/mollie-cli/pull/10))
- `webhook-tunnel --tunnel external` mode, letting you supply your own public URL (e.g. an existing reverse proxy) instead of spinning up a `cloudflared` tunnel ([#5](https://github.com/fjbender/mollie-cli/pull/5))

### Changed
- `webhooks` and `webhook-tunnel` now go through the `mollie-api-golang` SDK client instead of a raw `net/http` bypass ([#9](https://github.com/fjbender/mollie-cli/pull/9))
- Bumped `mollie-api-golang` from v0.10.3 to v1.3.42 ([#8](https://github.com/fjbender/mollie-cli/pull/8))

### Fixed
- `X-Mollie-Signature` verification no longer fails on the `sha256=` prefix ([#7](https://github.com/fjbender/mollie-cli/pull/7))
- `webhook-tunnel` now subscribes to `payment`, `refund`, `capture`, and `chargeback` events, not just `payment` ([#6](https://github.com/fjbender/mollie-cli/pull/6))
- Handle `f.Close` error in the tunnel event log writer ([#5](https://github.com/fjbender/mollie-cli/pull/5))

## [v0.7.0] — 2026-08-06

Baseline for this changelog. See `git log` for history prior to this point.
