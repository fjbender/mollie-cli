// cmd/webhooktunnel.go
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/fjbender/mollie-cli/internal/tunnel"
	"github.com/fjbender/mollie-cli/internal/tunnelstate"
	"github.com/fjbender/mollie-cli/internal/webhookevents"
	"github.com/fjbender/mollie-cli/internal/webhookserver"
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

	// The Permissions API only works against live mode and rejects a
	// profileId, regardless of which mode/profile the rest of this command
	// runs with — mollieclient.NewOrganizationClient already builds exactly
	// this kind of minimal client (no WithTestmode, no WithProfileID), the
	// same one used for Invoices for the same reason. Safe to use here: this
	// is a read-only GET reporting which permissions the token has, not
	// mode- or profile-specific data.
	client, err := mollieclient.NewOrganizationClient(cfg, flagAPIKey, flagVerbose)
	if err != nil {
		return nil, err
	}
	return webhookevents.Resolve(ctx, false, client.Permissions)
}

// restoreSnapshot patches a webhook subscription back to a previously
// captured state. It never needs the subscription's signing secret — PATCH
// doesn't change it.
func restoreSnapshot(ctx context.Context, c *whClient, snap *tunnelstate.SubscriptionSnapshot) error {
	body := whUpdateBody{
		Name:       &snap.Name,
		URL:        &snap.URL,
		EventTypes: snap.EventTypes,
	}
	if c.needsTestmode() {
		t := true
		body.Testmode = &t
	}
	return c.mutate(ctx, http.MethodPatch, "/webhooks/"+snap.ID, body, nil)
}

// webhookURLRetryAttempts and webhookURLRetryDelay bound how long we'll
// tolerate Mollie rejecting a webhook create/update because it can't yet
// resolve the tunnel's hostname. A freshly-started cloudflared quick tunnel
// is usually resolvable within a few seconds, but not always instantly —
// Mollie validates the URL synchronously when the subscription is
// created/patched, so a request fired immediately after cloudflared reports
// its URL can fail with a DNS-lookup error that has nothing to do with the
// request itself.
const (
	webhookURLRetryAttempts = 5
	webhookURLRetryDelay    = 2 * time.Second
)

// retryTunnelWebhookCall retries fn up to webhookURLRetryAttempts times,
// waiting webhookURLRetryDelay between attempts, to absorb the tunnel-DNS-
// not-ready-yet failure described above. It gives up and returns the last
// error once attempts are exhausted, or immediately if ctx is canceled
// while waiting between attempts.
func retryTunnelWebhookCall(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 1; attempt <= webhookURLRetryAttempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt < webhookURLRetryAttempts {
			fmt.Printf("  Attempt %d/%d failed (%v) — often just the tunnel not being resolvable yet, retrying...\n", attempt, webhookURLRetryAttempts, err)
			select {
			case <-time.After(webhookURLRetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return err
}

// handlerSwap lets the local server's handler be replaced after the server
// has already started serving requests. This is needed because Mollie
// validates a webhook subscription's URL synchronously when it's
// created/patched — including expecting an HTTP 200 response — so the local
// server must already be serving before that call is made. But for a freshly
// created subscription, the signing secret needed for signature verification
// isn't known until that same call succeeds. handlerSwap lets the server
// start with a secret-less handler (every event unverified) and be swapped
// for the real one the instant the secret becomes known.
type handlerSwap struct {
	h atomic.Pointer[http.Handler]
}

// Store replaces the handler used for all subsequent requests.
func (s *handlerSwap) Store(h http.Handler) {
	s.h.Store(&h)
}

// ServeHTTP delegates to whichever handler was most recently stored.
func (s *handlerSwap) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.h.Load()).ServeHTTP(w, r)
}

// printEvent logs a single received webhook event to stdout.
func printEvent(ev webhookserver.Event) {
	badge := "unverified"
	if ev.Verified {
		badge = "verified"
	}

	var e whEvent
	if err := json.Unmarshal(ev.Body, &e); err != nil {
		fmt.Printf("%s  (unparseable body: %v)  [%s]\n", ev.ReceivedAt.Format(time.RFC3339), err, badge)
		return
	}
	fmt.Printf("%s  %-28s  %-30s  [%s]\n", ev.ReceivedAt.Format(time.RFC3339), e.Type, e.EntityID, badge)
}

func runWebhookTunnel(_ *cobra.Command, _ []string) error {
	if flagLive {
		return errors.New("webhook-tunnel only supports test mode for now — it won't run against a live-mode credential")
	}

	cloudflaredPath, err := exec.LookPath("cloudflared")
	if err != nil {
		return fmt.Errorf("cloudflared not found on PATH: %w\n\n%s", err, cloudflaredInstallHint())
	}

	// Bind the local port up front, before anything with an external side
	// effect (tunnel, subscription mutation) happens. Binding late would mean
	// a busy port fails only after we've already repointed a real webhook
	// subscription at a tunnel nothing is listening behind — silently
	// misrouting events instead of the fail-fast "error instead of falling
	// back" behavior this command promises.
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", whtPort))
	if err != nil {
		return fmt.Errorf("cannot bind local port %d (is it already in use?): %w", whtPort, err)
	}
	defer func() { _ = ln.Close() }() // harmless if server.Shutdown already closed it

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	envFile, err := config.LoadFile()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	envName := envFile.ActiveEnvName()

	state, err := tunnelstate.Load()
	if err != nil {
		return fmt.Errorf("loading tunnel state: %w", err)
	}
	envState := state.Get(envName)

	c := newWhClient()

	if envState.PendingRestore != nil {
		fmt.Printf("A previous webhook-tunnel session for %q didn't shut down cleanly.\n", envName)
		restore, err := prompt.Confirm(fmt.Sprintf("Restore webhook subscription %q to its original URL before continuing?", envState.PendingRestore.Name))
		if err != nil {
			return err
		}
		if restore {
			if err := restoreSnapshot(ctx, c, envState.PendingRestore); err != nil {
				return fmt.Errorf("restoring previous subscription: %w", err)
			}
			envState.PendingRestore = nil
			if err := tunnelstate.Save(state); err != nil {
				return fmt.Errorf("saving tunnel state: %w", err)
			}
			fmt.Println("✓ Restored.")
		}
	}

	var list whWebhookList
	if err := c.get(ctx, "/webhooks", nil, &list); err != nil {
		return fmt.Errorf("listing webhook subscriptions: %w", err)
	}

	action := resolveSubscriptionAction(list.Embedded.Webhooks, envState.OwnedSubscriptionID)

	eventTypes, err := resolveEventTypes(ctx)
	if err != nil {
		return fmt.Errorf("resolving event types: %w", err)
	}
	if len(eventTypes) == 0 {
		return errors.New("no event types available to subscribe to — the current credential has no granted permissions for any known event type")
	}

	fmt.Printf("Starting tunnel on port %d...\n", whtPort)
	t, err := tunnel.Start(ctx, cloudflaredPath, whtPort, 20*time.Second)
	if err != nil {
		return fmt.Errorf("starting cloudflared tunnel: %w", err)
	}
	fmt.Printf("✓ Tunnel ready: %s\n", t.URL)

	// The local server must already be serving before the subscription is
	// created/patched below — Mollie's create/update validates the URL
	// synchronously, including expecting an HTTP 200, so cloudflared needs
	// somewhere live to forward that validation request to right now. The
	// initial handler uses a no-op callback: nothing genuine can reach the
	// tunnel yet (no subscription points at it), so the only thing that will
	// hit this handler before it's swapped is Mollie's own validation ping —
	// which isn't a real event and shouldn't be printed as one. Both branches
	// below swap in printEvent once they're done mutating the subscription.
	var handler handlerSwap
	handler.Store(webhookserver.Handler("", func(webhookserver.Event) {}))
	server := &http.Server{Handler: &handler}
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "local server error: %v\n", err)
		}
	}()

	var (
		secret  string
		created *whWebhook
	)

	switch action.Kind {
	case actionCreateFresh, actionRecreateOwned:
		if action.Kind == actionRecreateOwned {
			if err := c.mutate(ctx, http.MethodDelete, "/webhooks/"+action.Existing.ID, c.testmodeBody(), nil); err != nil {
				return fmt.Errorf("deleting previous mollie-cli subscription: %w", err)
			}
		}

		body := whCreateBody{
			Name:       "mollie-cli webhook-tunnel",
			URL:        t.URL,
			EventTypes: eventTypes,
		}
		if c.needsTestmode() {
			tm := true
			body.Testmode = &tm
		}

		var wh whWebhook
		if err := retryTunnelWebhookCall(ctx, func() error {
			return c.mutate(ctx, http.MethodPost, "/webhooks", body, &wh)
		}); err != nil {
			return fmt.Errorf("creating webhook subscription: %w", err)
		}
		created = &wh
		secret = wh.WebhookSecret
		handler.Store(webhookserver.Handler(secret, printEvent))
		envState.OwnedSubscriptionID = wh.ID
		if err := tunnelstate.Save(state); err != nil {
			return fmt.Errorf("saving tunnel state: %w", err)
		}

		fmt.Printf("✓ Webhook subscription %s created — signature verification enabled\n", wh.ID)
		fmt.Printf("  Signing secret: %s\n", secret)

	case actionPickForeign:
		opts := make([]prompt.SelectOption[string], 0, len(list.Embedded.Webhooks))
		for _, wh := range list.Embedded.Webhooks {
			opts = append(opts, prompt.SelectOption[string]{
				Label: fmt.Sprintf("%s — %s (%s)", wh.Name, truncateURL(wh.URL, 50), summarizeEventTypes(wh.EventTypes)),
				Value: wh.ID,
			})
		}
		chosenID, err := prompt.Select("Both test-mode webhook slots are in use. Which one should be temporarily repointed to the tunnel?", opts)
		if err != nil {
			return err
		}

		var chosen *whWebhook
		for i := range list.Embedded.Webhooks {
			if list.Embedded.Webhooks[i].ID == chosenID {
				chosen = &list.Embedded.Webhooks[i]
				break
			}
		}
		if chosen == nil {
			return fmt.Errorf("selected webhook %s not found", chosenID)
		}

		switch {
		case envState.PendingRestore == nil:
			envState.PendingRestore = &tunnelstate.SubscriptionSnapshot{
				ID:         chosen.ID,
				Name:       chosen.Name,
				URL:        chosen.URL,
				EventTypes: chosen.EventTypes,
			}
		case envState.PendingRestore.ID == chosen.ID:
			// Re-picking the very subscription an earlier crash already left
			// mid-repoint — keep its original pre-crash snapshot. Re-snapshotting
			// its current (already-repointed) state here would make a future
			// restore a no-op back to the broken tunnel URL instead of the truth.
			fmt.Println("Reusing the existing restore snapshot for this subscription from an earlier session.")
		default:
			// The user declined the earlier "restore from a previous crash?"
			// prompt and is now repointing a *different* subscription.
			// Overwriting happens immediately below — there is no later chance
			// to recover the discarded snapshot, so say so plainly rather than
			// implying a future run could still restore it.
			fmt.Fprintf(os.Stderr, "warning: discarding the unresolved restore snapshot for %q — its original pre-repoint state is now permanently lost.\n", envState.PendingRestore.Name)
			envState.PendingRestore = &tunnelstate.SubscriptionSnapshot{
				ID:         chosen.ID,
				Name:       chosen.Name,
				URL:        chosen.URL,
				EventTypes: chosen.EventTypes,
			}
		}
		if err := tunnelstate.Save(state); err != nil {
			return fmt.Errorf("saving tunnel state: %w", err)
		}

		patchBody := whUpdateBody{URL: &t.URL}
		if whtEventTypes != "" {
			patchBody.EventTypes = eventTypes
		}
		if c.needsTestmode() {
			tm := true
			patchBody.Testmode = &tm
		}
		if err := retryTunnelWebhookCall(ctx, func() error {
			return c.mutate(ctx, http.MethodPatch, "/webhooks/"+chosen.ID, patchBody, nil)
		}); err != nil {
			return fmt.Errorf("repointing webhook subscription %s: %w", chosen.ID, err)
		}
		handler.Store(webhookserver.Handler("", printEvent))

		fmt.Printf("⚠ Repointed existing subscription %q — its signing secret is unknown, so incoming events cannot be verified this session.\n", chosen.Name)
	}

	fmt.Println("Listening for webhook events. Press Ctrl-C to stop.")
	<-ctx.Done()
	fmt.Println("\nShutting down...")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	_ = server.Shutdown(shutdownCtx)

	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCleanup()

	switch action.Kind {
	case actionPickForeign:
		if envState.PendingRestore != nil {
			if err := restoreSnapshot(cleanupCtx, c, envState.PendingRestore); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to restore original webhook subscription: %v\n", err)
				fmt.Fprintln(os.Stderr, "Run `mollie webhook-tunnel` again to retry automatically, or fix it manually with `mollie webhooks update`.")
			} else {
				envState.PendingRestore = nil
				fmt.Println("✓ Restored original webhook subscription.")
			}
		}
	default:
		if created != nil {
			if err := c.mutate(cleanupCtx, http.MethodDelete, "/webhooks/"+created.ID, c.testmodeBody(), nil); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to delete temporary webhook subscription %s: %v\n", created.ID, err)
			} else {
				envState.OwnedSubscriptionID = ""
			}
		}
	}

	if err := tunnelstate.Save(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save tunnel state: %v\n", err)
	}

	_ = t.Wait() // cloudflared is killed automatically because ctx was canceled

	return nil
}
