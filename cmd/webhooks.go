package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/config"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/fjbender/mollie-cli/internal/verbose"
	"github.com/spf13/cobra"
)

const mollieAPIV2 = "https://api.mollie.com/v2"

// Internal response types use plain strings for eventTypes so we aren't
// bound to the SDK's incomplete WebhookEventTypes enum.

type whWebhook struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	ProfileID     *string  `json:"profileId"`
	CreatedAt     string   `json:"createdAt"`
	EventTypes    []string `json:"eventTypes"`
	Status        string   `json:"status"`
	Mode          string   `json:"mode"`
	WebhookSecret string   `json:"webhookSecret,omitempty"`
}

type whWebhookList struct {
	Count    int64 `json:"count"`
	Embedded struct {
		Webhooks []whWebhook `json:"webhooks"`
	} `json:"_embedded"`
}

type whEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	EntityID  string `json:"entityId"`
	CreatedAt string `json:"createdAt"`
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

// ── HTTP client ───────────────────────────────────────────────────────────────

// whClient makes authenticated Mollie API calls with our own response types,
// sidestepping the SDK's incomplete WebhookEventTypes enum.
type whClient struct {
	http     *http.Client
	apiKey   string
	isAPIKey bool // true for test_/live_ keys; false for access tokens
}

func newWhClient() *whClient {
	key := cfg.APIKey
	if flagAPIKey != "" {
		key = flagAPIKey
	}

	var transport http.RoundTripper = http.DefaultTransport
	if flagVerbose > 0 {
		transport = &verbose.LoggingTransport{Level: flagVerbose, Inner: transport}
	}

	return &whClient{
		http:     &http.Client{Transport: transport},
		apiKey:   key,
		isAPIKey: config.IsAPIKey(key),
	}
}

// needsTestmode reports whether testmode must be explicitly requested.
// Only relevant for access tokens — API keys encode mode in their prefix.
func (c *whClient) needsTestmode() bool {
	return !c.isAPIKey && !flagLive
}

// testmodeBody returns a body containing testmode=true for access-token test
// calls, or nil (no body) otherwise.
func (c *whClient) testmodeBody() *whTestmodeBody {
	if c.needsTestmode() {
		t := true
		return &whTestmodeBody{Testmode: &t}
	}
	return nil
}

// get fetches a resource and decodes the JSON response into result.
func (c *whClient) get(ctx context.Context, path string, query url.Values, result any) error {
	if c.needsTestmode() {
		if query == nil {
			query = url.Values{}
		}
		query.Set("testmode", "true")
	}
	return c.do(ctx, http.MethodGet, path, query, nil, result)
}

// mutate sends a POST/PATCH/DELETE request with an optional JSON body.
func (c *whClient) mutate(ctx context.Context, method, path string, body, result any) error {
	return c.do(ctx, method, path, nil, body, result)
}

func (c *whClient) do(ctx context.Context, method, path string, query url.Values, body, result any) error {
	u, err := url.Parse(mollieAPIV2 + path)
	if err != nil {
		return err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", "Mollie-CLI/1.0.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !c.isAPIKey && flagProfile != "" {
		req.Header.Set("X-Profile-Id", flagProfile)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Status int    `json:"status"`
			Title  string `json:"title"`
			Detail string `json:"detail"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&apiErr)
		msg := apiErr.Title
		if apiErr.Detail != "" {
			msg += " — " + apiErr.Detail
		}
		return fmt.Errorf("API error %d: %s", resp.StatusCode, msg)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

// ── request body types ────────────────────────────────────────────────────────

type whCreateBody struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"eventTypes"`
	Testmode   *bool    `json:"testmode,omitempty"`
}

type whUpdateBody struct {
	Name       *string  `json:"name,omitempty"`
	URL        *string  `json:"url,omitempty"`
	EventTypes []string `json:"eventTypes,omitempty"`
	Testmode   *bool    `json:"testmode,omitempty"`
}

type whTestmodeBody struct {
	Testmode *bool `json:"testmode,omitempty"`
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

	c := newWhClient()

	body := whCreateBody{
		Name:       whCreateName,
		URL:        whCreateURL,
		EventTypes: parseWebhookEventTypes(whCreateEventTypes),
	}
	if c.needsTestmode() {
		t := true
		body.Testmode = &t
	}

	var wh whWebhook
	if err := c.mutate(context.Background(), http.MethodPost, "/webhooks", body, &wh); err != nil {
		return fmt.Errorf("creating webhook: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(wh)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			webhookDetailRows(wh, true),
			!flagLive,
		)
	}
	return nil
}

func runWebhooksList(_ *cobra.Command, _ []string) error {
	c := newWhClient()

	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", whListLimit))
	if whListFrom != "" {
		q.Set("from", whListFrom)
	}
	if whListSort != "" {
		q.Set("sort", whListSort)
	}
	if whListEventTypes != "" {
		q.Set("eventTypes", whListEventTypes)
	}

	var list whWebhookList
	if err := c.get(context.Background(), "/webhooks", q, &list); err != nil {
		return fmt.Errorf("listing webhooks: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(list)
	default:
		rows := make([][]string, 0, len(list.Embedded.Webhooks))
		for _, wh := range list.Embedded.Webhooks {
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
	c := newWhClient()

	var wh whWebhook
	if err := c.get(context.Background(), "/webhooks/"+args[0], nil, &wh); err != nil {
		return fmt.Errorf("getting webhook: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(wh)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			webhookDetailRows(wh, false),
			!flagLive,
		)
	}
	return nil
}

func runWebhooksUpdate(cmd *cobra.Command, args []string) error {
	c := newWhClient()

	body := whUpdateBody{}
	if cmd.Flags().Changed("name") {
		body.Name = &whUpdateName
	}
	if cmd.Flags().Changed("url") {
		body.URL = &whUpdateURL
	}
	if cmd.Flags().Changed("event-types") {
		body.EventTypes = parseWebhookEventTypes(whUpdateEventTypes)
	}
	if c.needsTestmode() {
		t := true
		body.Testmode = &t
	}

	var wh whWebhook
	if err := c.mutate(context.Background(), http.MethodPatch, "/webhooks/"+args[0], body, &wh); err != nil {
		return fmt.Errorf("updating webhook: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(wh)
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

	c := newWhClient()
	if err := c.mutate(context.Background(), http.MethodDelete, "/webhooks/"+webhookID, c.testmodeBody(), nil); err != nil {
		return fmt.Errorf("deleting webhook: %w", err)
	}

	fmt.Printf("✓ Webhook %s deleted\n", webhookID)
	return nil
}

func runWebhooksPing(_ *cobra.Command, args []string) error {
	c := newWhClient()
	if err := c.mutate(context.Background(), http.MethodPost, "/webhooks/"+args[0]+"/ping", c.testmodeBody(), nil); err != nil {
		return fmt.Errorf("pinging webhook: %w", err)
	}

	fmt.Printf("✓ Ping sent — webhook %s triggered successfully\n", args[0])
	return nil
}

func runWebhooksEventsGet(_ *cobra.Command, args []string) error {
	c := newWhClient()

	var ev whEvent
	if err := c.get(context.Background(), "/events/"+args[0], nil, &ev); err != nil {
		return fmt.Errorf("getting webhook event: %w", err)
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(ev)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			[][]string{
				{"ID", ev.ID},
				{"Type", ev.Type},
				{"Entity ID", ev.EntityID},
				{"Created At", ev.CreatedAt},
			},
			!flagLive,
		)
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

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
