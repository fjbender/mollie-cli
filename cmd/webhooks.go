package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// whWebhook is a normalized view over the SDK's three distinct-but-
// identically-shaped webhook response types (CreateWebhook, EntityWebhook,
// ListEntityWebhook — Speakeasy generates no shared interface between them),
// used for table rendering and by the webhook-tunnel command's subscription
// bookkeeping.
type whWebhook struct {
	ID            string
	Name          string
	URL           string
	ProfileID     *string
	CreatedAt     string
	EventTypes    []string
	Status        string
	Mode          string
	WebhookSecret string
}

func fromCreateWebhook(w *components.CreateWebhook) whWebhook {
	return whWebhook{
		ID:            w.GetID(),
		Name:          w.GetName(),
		URL:           w.GetURL(),
		ProfileID:     w.GetProfileID(),
		CreatedAt:     w.GetCreatedAt(),
		EventTypes:    eventTypesToStrings(w.GetEventTypes()),
		Status:        string(w.GetStatus()),
		Mode:          string(w.GetMode()),
		WebhookSecret: w.GetWebhookSecret(),
	}
}

func fromEntityWebhook(w *components.EntityWebhook) whWebhook {
	return whWebhook{
		ID:         w.GetID(),
		Name:       w.GetName(),
		URL:        w.GetURL(),
		ProfileID:  w.GetProfileID(),
		CreatedAt:  w.GetCreatedAt(),
		EventTypes: eventTypesToStrings(w.GetEventTypes()),
		Status:     string(w.GetStatus()),
		Mode:       string(w.GetMode()),
	}
}

func fromListEntityWebhook(w components.ListEntityWebhook) whWebhook {
	return whWebhook{
		ID:         w.GetID(),
		Name:       w.GetName(),
		URL:        w.GetURL(),
		ProfileID:  w.GetProfileID(),
		CreatedAt:  w.GetCreatedAt(),
		EventTypes: eventTypesToStrings(w.GetEventTypes()),
		Status:     string(w.GetStatus()),
		Mode:       string(w.GetMode()),
	}
}

func eventTypesToStrings(types []components.WebhookEventTypes) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = string(t)
	}
	return out
}

// ── flag value holders ────────────────────────────────────────────────────────

var (
	// create flags
	whCreateName       string
	whCreateURL        string
	whCreateEventTypes string

	// list flags
	whListLimit      int64
	whListFrom       string
	whListSort       string
	whListEventTypes string

	// update flags
	whUpdateName       string
	whUpdateURL        string
	whUpdateEventTypes string

	// delete flag
	whDeleteConfirm bool
)

// ── command tree ──────────────────────────────────────────────────────────────

var webhooksCmd = &cobra.Command{
	Use:   "webhooks",
	Short: "Manage Mollie webhook subscriptions",
}

var webhooksCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new webhook subscription",
	RunE:  runWebhooksCreate,
}

var webhooksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhook subscriptions",
	RunE:  runWebhooksList,
}

var webhooksGetCmd = &cobra.Command{
	Use:   "get <webhook-id>",
	Short: "Get a webhook subscription",
	Args:  cobra.ExactArgs(1),
	RunE:  runWebhooksGet,
}

var webhooksUpdateCmd = &cobra.Command{
	Use:   "update <webhook-id>",
	Short: "Update a webhook subscription",
	Args:  cobra.ExactArgs(1),
	RunE:  runWebhooksUpdate,
}

var webhooksDeleteCmd = &cobra.Command{
	Use:   "delete <webhook-id>",
	Short: "Delete a webhook subscription",
	Args:  cobra.ExactArgs(1),
	RunE:  runWebhooksDelete,
}

var webhooksPingCmd = &cobra.Command{
	Use:   "ping <webhook-id>",
	Short: "Send a test event to a webhook endpoint",
	Args:  cobra.ExactArgs(1),
	RunE:  runWebhooksPing,
}

var webhooksEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Inspect webhook events",
}

var webhooksEventsGetCmd = &cobra.Command{
	Use:   "get <event-id>",
	Short: "Get a webhook event",
	Args:  cobra.ExactArgs(1),
	RunE:  runWebhooksEventsGet,
}

func init() {
	// create
	webhooksCreateCmd.Flags().StringVar(&whCreateName, "name", "", "Name for the webhook subscription (required)")
	webhooksCreateCmd.Flags().StringVar(&whCreateURL, "url", "", "Destination URL for webhook events (required)")
	webhooksCreateCmd.Flags().StringVar(&whCreateEventTypes, "event-types", "", `Comma-separated event types, e.g. "payment.paid,refund.refunded" or "*" (required)`)

	// list
	webhooksListCmd.Flags().Int64Var(&whListLimit, "limit", 50, "Maximum number of results to return")
	webhooksListCmd.Flags().StringVar(&whListFrom, "from", "", "Return results starting from this webhook ID (cursor pagination)")
	webhooksListCmd.Flags().StringVar(&whListSort, "sort", "", "Sort direction: asc or desc (default: desc)")
	webhooksListCmd.Flags().StringVar(&whListEventTypes, "event-types", "", "Filter results by a single event type")

	// update
	webhooksUpdateCmd.Flags().StringVar(&whUpdateName, "name", "", "New name for the webhook subscription")
	webhooksUpdateCmd.Flags().StringVar(&whUpdateURL, "url", "", "New destination URL")
	webhooksUpdateCmd.Flags().StringVar(&whUpdateEventTypes, "event-types", "", "New comma-separated list of event types")

	// delete
	webhooksDeleteCmd.Flags().BoolVar(&whDeleteConfirm, "confirm", false, "Skip the confirmation prompt")

	webhooksEventsCmd.AddCommand(webhooksEventsGetCmd)

	webhooksCmd.AddCommand(webhooksCreateCmd)
	webhooksCmd.AddCommand(webhooksListCmd)
	webhooksCmd.AddCommand(webhooksGetCmd)
	webhooksCmd.AddCommand(webhooksUpdateCmd)
	webhooksCmd.AddCommand(webhooksDeleteCmd)
	webhooksCmd.AddCommand(webhooksPingCmd)
	webhooksCmd.AddCommand(webhooksEventsCmd)

	rootCmd.AddCommand(webhooksCmd)
}

// ── event-type conversion helpers ────────────────────────────────────────────

// parseWebhookEventTypes splits a comma-separated string into individual event types.
func parseWebhookEventTypes(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// toCreateEventTypes converts parsed event-type strings into the SDK's
// create-webhook union type: a bare wildcard is sent as a scalar value,
// anything else as an array.
func toCreateEventTypes(types []string) operations.CreateWebhookEventTypes {
	if len(types) == 1 && types[0] == string(components.WebhookEventTypesWildcard) {
		return operations.CreateCreateWebhookEventTypesWebhookEventTypes(components.WebhookEventTypesWildcard)
	}
	arr := make([]components.WebhookEventTypes, len(types))
	for i, t := range types {
		arr[i] = components.WebhookEventTypes(t)
	}
	return operations.CreateCreateWebhookEventTypesArrayOfWebhookEventTypes(arr)
}

// toUpdateEventTypes is the update-webhook equivalent of toCreateEventTypes.
func toUpdateEventTypes(types []string) operations.UpdateWebhookEventTypes {
	if len(types) == 1 && types[0] == string(components.WebhookEventTypesWildcard) {
		return operations.CreateUpdateWebhookEventTypesWebhookEventTypes(components.WebhookEventTypesWildcard)
	}
	arr := make([]components.WebhookEventTypes, len(types))
	for i, t := range types {
		arr[i] = components.WebhookEventTypes(t)
	}
	return operations.CreateUpdateWebhookEventTypesArrayOfWebhookEventTypes(arr)
}

// ── handlers ──────────────────────────────────────────────────────────────────

func runWebhooksCreate(_ *cobra.Command, _ []string) error {
	switch {
	case whCreateName == "":
		return fmt.Errorf("required flag \"name\" not set")
	case whCreateURL == "":
		return fmt.Errorf("required flag \"url\" not set")
	case whCreateEventTypes == "":
		return fmt.Errorf("required flag \"event-types\" not set")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	body := &operations.CreateWebhookRequestBody{
		Name:       whCreateName,
		URL:        whCreateURL,
		EventTypes: toCreateEventTypes(parseWebhookEventTypes(whCreateEventTypes)),
	}

	resp, err := client.Webhooks.Create(context.Background(), nil, body)
	if err != nil {
		return fmt.Errorf("creating webhook: %w", err)
	}
	wh := resp.GetCreateWebhook()
	if wh == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(wh)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			webhookDetailRows(fromCreateWebhook(wh), true),
			!flagLive,
		)
	}
	return nil
}

func runWebhooksList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	req := operations.ListWebhooksRequest{
		Limit: &whListLimit,
	}
	if whListFrom != "" {
		req.From = &whListFrom
	}
	if whListSort != "" {
		sort := components.Sorting(whListSort)
		req.Sort = &sort
	}
	if whListEventTypes != "" {
		et := components.WebhookEventTypes(whListEventTypes)
		req.EventTypes = &et
	}

	resp, err := client.Webhooks.List(context.Background(), req)
	if err != nil {
		return fmt.Errorf("listing webhooks: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		embedded := resp.Object.GetEmbedded()
		webhooks := embedded.GetWebhooks()
		rows := make([][]string, 0, len(webhooks))
		for _, w := range webhooks {
			wh := fromListEntityWebhook(w)
			rows = append(rows, []string{
				wh.ID,
				wh.Name,
				truncateURL(wh.URL, 40),
				wh.Status,
				wh.Mode,
				summarizeEventTypes(wh.EventTypes),
				wh.CreatedAt,
			})
		}
		output.PrintTable(
			[]string{"ID", "NAME", "URL", "STATUS", "MODE", "EVENT TYPES", "CREATED AT"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runWebhooksGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	resp, err := client.Webhooks.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting webhook: %w", err)
	}
	wh := resp.GetEntityWebhook()
	if wh == nil {
		return fmt.Errorf("webhook not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(wh)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			webhookDetailRows(fromEntityWebhook(wh), false),
			!flagLive,
		)
	}
	return nil
}

func runWebhooksUpdate(cmd *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	body := &operations.UpdateWebhookRequestBody{}
	if cmd.Flags().Changed("name") {
		body.Name = &whUpdateName
	}
	if cmd.Flags().Changed("url") {
		body.URL = &whUpdateURL
	}
	if cmd.Flags().Changed("event-types") {
		et := toUpdateEventTypes(parseWebhookEventTypes(whUpdateEventTypes))
		body.EventTypes = &et
	}

	resp, err := client.Webhooks.Update(context.Background(), args[0], nil, body)
	if err != nil {
		return fmt.Errorf("updating webhook: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.GetEntityWebhook())
	default:
		fmt.Printf("✓ Webhook %s updated\n", args[0])
	}
	return nil
}

func runWebhooksDelete(_ *cobra.Command, args []string) error {
	webhookID := args[0]

	if !whDeleteConfirm && !flagYes {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Delete webhook %s?", webhookID))
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("Cancelled.")
				return nil
			}
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	if _, err := client.Webhooks.Delete(context.Background(), webhookID, nil, nil); err != nil {
		return fmt.Errorf("deleting webhook: %w", err)
	}

	fmt.Printf("✓ Webhook %s deleted\n", webhookID)
	return nil
}

func runWebhooksPing(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	if _, err := client.Webhooks.Test(context.Background(), args[0], nil, nil); err != nil {
		return fmt.Errorf("pinging webhook: %w", err)
	}

	fmt.Printf("✓ Ping sent — webhook %s triggered successfully\n", args[0])
	return nil
}

func runWebhooksEventsGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile, flagVerbose)
	if err != nil {
		return err
	}

	resp, err := client.WebhookEvents.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting webhook event: %w", err)
	}
	ev := resp.GetEntityWebhookEvent()
	if ev == nil {
		return fmt.Errorf("webhook event not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(ev)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", ev.GetID()},
				{"Type", string(ev.GetWebhookEventTypes())},
				{"Entity ID", ev.GetEntityID()},
				{"Created At", ev.GetCreatedAt()},
			},
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// summarizeEventTypes returns a compact label for table cells: a single event
// type is shown verbatim; multiple are shown as "N types".
func summarizeEventTypes(types []string) string {
	switch len(types) {
	case 0:
		return "—"
	case 1:
		return types[0]
	default:
		return fmt.Sprintf("%d types", len(types))
	}
}

// webhookDetailRows builds key-value rows for the detail view of a webhook.
// showSecret should be true only for the create response (the only time the
// webhookSecret is returned by the API).
func webhookDetailRows(wh whWebhook, showSecret bool) [][]string {
	profileID := "—"
	if wh.ProfileID != nil {
		profileID = *wh.ProfileID
	}
	rows := [][]string{
		{"ID", wh.ID},
		{"Name", wh.Name},
		{"URL", wh.URL},
		{"Status", wh.Status},
		{"Mode", wh.Mode},
		{"Profile ID", profileID},
		{"Event Types", strings.Join(wh.EventTypes, ", ")},
		{"Created At", wh.CreatedAt},
	}
	if showSecret {
		rows = append(rows, []string{"Webhook Secret", wh.WebhookSecret})
	}
	return rows
}

// truncateURL shortens s to at most n runes, appending "…" if clipped.
func truncateURL(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
