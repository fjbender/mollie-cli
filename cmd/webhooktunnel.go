package cmd

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
