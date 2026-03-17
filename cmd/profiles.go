package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/fjbender/mollie-cli/internal/input"
	"github.com/fjbender/mollie-cli/internal/mollieclient"
	"github.com/fjbender/mollie-cli/internal/output"
	"github.com/fjbender/mollie-cli/internal/prompt"
	"github.com/mollie/mollie-api-golang/models/components"
	"github.com/mollie/mollie-api-golang/models/operations"
	"github.com/spf13/cobra"
)

// ── flag value holders ───────────────────────────────────────────────────────

var (
	// list flags
	profListLimit int64
	profListFrom  string

	// create flags
	profCreateName     string
	profCreateWebsite  string
	profCreateEmail    string
	profCreatePhone    string
	profCreateCategory string

	// update flags
	profUpdateName        string
	profUpdateWebsite     string
	profUpdateEmail       string
	profUpdatePhone       string
	profUpdateDescription string
	profUpdateCategory    string

	// delete flag
	profConfirm bool
)

// ── command tree ─────────────────────────────────────────────────────────────

var profilesCmd = &cobra.Command{
	Use:   "profiles",
	Short: "Manage Mollie profiles",
}

var profilesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List profiles",
	RunE:  runProfilesList,
}

var profilesGetCmd = &cobra.Command{
	Use:   "get <profile-id>",
	Short: "Get a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfilesGet,
}

var profilesCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Get the profile linked to the current API key",
	RunE:  runProfilesCurrent,
}

var profilesCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a profile",
	RunE:  runProfilesCreate,
}

var profilesUpdateCmd = &cobra.Command{
	Use:   "update <profile-id>",
	Short: "Update a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfilesUpdate,
}

var profilesDeleteCmd = &cobra.Command{
	Use:   "delete <profile-id>",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfilesDelete,
}

func init() {
	// list flags
	profilesListCmd.Flags().Int64Var(&profListLimit, "limit", 50, "Maximum number of results to return")
	profilesListCmd.Flags().StringVar(&profListFrom, "from", "", "Return results starting from this profile ID (cursor pagination)")

	// create flags
	profilesCreateCmd.Flags().StringVar(&profCreateName, "name", "", "Name of the profile (required)")
	profilesCreateCmd.Flags().StringVar(&profCreateWebsite, "website", "", "URL of the profile's website (required)")
	profilesCreateCmd.Flags().StringVar(&profCreateEmail, "email", "", "Email address associated with the profile (required)")
	profilesCreateCmd.Flags().StringVar(&profCreatePhone, "phone", "", "Phone number associated with the profile (required)")
	profilesCreateCmd.Flags().StringVar(&profCreateCategory, "category", "", "Merchant category code (e.g. 5399)")
	// Required fields are validated in runProfilesCreate so that JSON stdin can
	// supply them without triggering Cobra's pre-RunE flag validation.

	// update flags
	profilesUpdateCmd.Flags().StringVar(&profUpdateName, "name", "", "New profile name")
	profilesUpdateCmd.Flags().StringVar(&profUpdateWebsite, "website", "", "New website URL")
	profilesUpdateCmd.Flags().StringVar(&profUpdateEmail, "email", "", "New email address")
	profilesUpdateCmd.Flags().StringVar(&profUpdatePhone, "phone", "", "New phone number")
	profilesUpdateCmd.Flags().StringVar(&profUpdateDescription, "description", "", "New description")
	profilesUpdateCmd.Flags().StringVar(&profUpdateCategory, "category", "", "New merchant category code")

	// delete flag
	profilesDeleteCmd.Flags().BoolVar(&profConfirm, "confirm", false, "Skip the confirmation prompt")

	profilesCmd.AddCommand(profilesListCmd)
	profilesCmd.AddCommand(profilesGetCmd)
	profilesCmd.AddCommand(profilesCurrentCmd)
	profilesCmd.AddCommand(profilesCreateCmd)
	profilesCmd.AddCommand(profilesUpdateCmd)
	profilesCmd.AddCommand(profilesDeleteCmd)
	rootCmd.AddCommand(profilesCmd)
}

// ── handlers ─────────────────────────────────────────────────────────────────

func runProfilesList(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	var from *string
	if profListFrom != "" {
		from = &profListFrom
	}
	limit := profListLimit

	resp, err := client.Profiles.List(context.Background(), from, &limit, nil)
	if err != nil {
		return fmt.Errorf("listing profiles: %w", err)
	}
	if resp.Object == nil {
		return nil
	}

	embedded := resp.Object.GetEmbedded()
	profiles := embedded.GetProfiles()

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(resp.Object)
	default:
		rows := make([][]string, 0, len(profiles))
		for _, p := range profiles {
			rows = append(rows, []string{
				p.GetID(),
				p.GetName(),
				string(p.GetStatus()),
				string(p.GetMode()),
				p.GetWebsite(),
			})
		}
		output.PrintTable(
			[]string{"ID", "NAME", "STATUS", "MODE", "WEBSITE"},
			rows,
			!flagLive,
		)
	}
	return nil
}

func runProfilesGet(_ *cobra.Command, args []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Profiles.Get(context.Background(), args[0], nil, nil)
	if err != nil {
		return fmt.Errorf("getting profile: %w", err)
	}
	p := resp.GetProfileResponse()
	if p == nil {
		return fmt.Errorf("profile not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(p)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			profileDetailRows(p),
			!flagLive,
		)
	}
	return nil
}

func runProfilesCurrent(_ *cobra.Command, _ []string) error {
	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	resp, err := client.Profiles.GetCurrent(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("getting current profile: %w", err)
	}
	p := resp.GetProfileResponse()
	if p == nil {
		return fmt.Errorf("profile not found")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(p)
	default:
		output.PrintTable(
			[]string{"FIELD", "VALUE"},
			profileDetailRows(p),
			!flagLive,
		)
	}
	return nil
}

func runProfilesCreate(cmd *cobra.Command, _ []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "name"); ok && !cmd.Flags().Changed("name") {
			profCreateName = v
		}
		if v, ok := input.Str(jsonInput, "website"); ok && !cmd.Flags().Changed("website") {
			profCreateWebsite = v
		}
		if v, ok := input.Str(jsonInput, "email"); ok && !cmd.Flags().Changed("email") {
			profCreateEmail = v
		}
		if v, ok := input.Str(jsonInput, "phone"); ok && !cmd.Flags().Changed("phone") {
			profCreatePhone = v
		}
		if v, ok := input.Str(jsonInput, "businessCategory"); ok && !cmd.Flags().Changed("category") {
			profCreateCategory = v
		}
	}

	// Validate required fields (may have been supplied via JSON stdin).
	switch {
	case profCreateName == "":
		return fmt.Errorf("required flag \"name\" not set")
	case profCreateWebsite == "":
		return fmt.Errorf("required flag \"website\" not set")
	case profCreateEmail == "":
		return fmt.Errorf("required flag \"email\" not set")
	case profCreatePhone == "":
		return fmt.Errorf("required flag \"phone\" not set")
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	req := components.ProfileRequest{
		Name:    profCreateName,
		Website: profCreateWebsite,
		Email:   profCreateEmail,
		Phone:   profCreatePhone,
	}
	if profCreateCategory != "" {
		req.BusinessCategory = &profCreateCategory
	}

	resp, err := client.Profiles.Create(context.Background(), req, nil)
	if err != nil {
		return fmt.Errorf("creating profile: %w", err)
	}
	p := resp.GetProfileResponse()
	if p == nil {
		return fmt.Errorf("unexpected empty response from API")
	}

	switch resolvedOutput() {
	case output.FormatJSON:
		return output.PrintJSON(p)
	default:
		output.PrintTable(
			[]string{"ID", "NAME", "STATUS", "MODE", "WEBSITE"},
			[][]string{{
				p.GetID(),
				p.GetName(),
				string(p.GetStatus()),
				string(p.GetMode()),
				p.GetWebsite(),
			}},
			!flagLive,
		)
	}
	return nil
}

func runProfilesUpdate(cmd *cobra.Command, args []string) error {
	// ── JSON stdin input ─────────────────────────────────────────────────────
	jsonInput, err := input.ReadStdin()
	if err != nil {
		return err
	}
	if jsonInput != nil {
		if v, ok := input.Str(jsonInput, "name"); ok && !cmd.Flags().Changed("name") {
			profUpdateName = v
		}
		if v, ok := input.Str(jsonInput, "website"); ok && !cmd.Flags().Changed("website") {
			profUpdateWebsite = v
		}
		if v, ok := input.Str(jsonInput, "email"); ok && !cmd.Flags().Changed("email") {
			profUpdateEmail = v
		}
		if v, ok := input.Str(jsonInput, "phone"); ok && !cmd.Flags().Changed("phone") {
			profUpdatePhone = v
		}
		if v, ok := input.Str(jsonInput, "description"); ok && !cmd.Flags().Changed("description") {
			profUpdateDescription = v
		}
		if v, ok := input.Str(jsonInput, "businessCategory"); ok && !cmd.Flags().Changed("category") {
			profUpdateCategory = v
		}
	}

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	body := operations.UpdateProfileRequestBody{}
	if profUpdateName != "" {
		body.Name = &profUpdateName
	}
	if profUpdateWebsite != "" {
		body.Website = &profUpdateWebsite
	}
	if profUpdateEmail != "" {
		body.Email = &profUpdateEmail
	}
	if profUpdatePhone != "" {
		body.Phone = &profUpdatePhone
	}
	if profUpdateDescription != "" {
		body.Description = &profUpdateDescription
	}
	if profUpdateCategory != "" {
		body.BusinessCategory = &profUpdateCategory
	}

	if _, err := client.Profiles.Update(context.Background(), args[0], body, nil); err != nil {
		return fmt.Errorf("updating profile: %w", err)
	}

	fmt.Printf("\u2713 Profile %s updated\n", args[0])
	return nil
}

func runProfilesDelete(_ *cobra.Command, args []string) error {
	profileID := args[0]

	if !profConfirm && !flagYes {
		confirmed, err := prompt.Confirm(fmt.Sprintf("Delete profile %s?", profileID))
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

	client, err := mollieclient.New(cfg, flagAPIKey, flagLive, flagProfile)
	if err != nil {
		return err
	}

	if _, err := client.Profiles.Delete(context.Background(), profileID, nil); err != nil {
		return fmt.Errorf("deleting profile: %w", err)
	}

	fmt.Printf("\u2713 Profile %s deleted\n", profileID)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func profileDetailRows(p *components.ProfileResponse) [][]string {
	row := func(k, v string) []string { return []string{k, v} }
	return [][]string{
		row("ID", p.GetID()),
		row("Mode", string(p.GetMode())),
		row("Name", p.GetName()),
		row("Website", p.GetWebsite()),
		row("Email", p.GetEmail()),
		row("Phone", p.GetPhone()),
		row("Business Category", p.GetBusinessCategory()),
		row("Status", string(p.GetStatus())),
		row("Created At", p.GetCreatedAt()),
	}
}
