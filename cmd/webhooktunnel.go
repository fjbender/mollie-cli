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
		return "Install it with your package manager, e.g. `sudo apt install cloudflared`,\nor download a binary from https://github.com/cloudflare/cloudflared/releases"
	case "windows":
		return "Install it with: winget install --id Cloudflare.cloudflared\nor download a binary from https://github.com/cloudflare/cloudflared/releases"
	default:
		return "Download a binary for your platform from https://github.com/cloudflare/cloudflared/releases"
	}
}

func runWebhookTunnel(_ *cobra.Command, _ []string) error {
	if flagLive {
		return errors.New("webhook-tunnel does not support --live yet; test mode only")
	}

	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
	}

	fmt.Println("Preflight checks passed.")
	return nil
}
