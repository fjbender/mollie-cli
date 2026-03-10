package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Format represents a supported output format.
type Format string

// Supported output format constants.
const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

// NoColor disables ANSI colour output when set to true.
// Set by the --no-color flag or the NO_COLOR environment variable.
var NoColor = false

// isTTY reports whether stdout is a real terminal.
func isTTY() bool {
	return isatty.IsTerminal(os.Stdout.Fd())
}

// colorEnabled reports whether colour should be rendered on stdout.
func colorEnabled() bool {
	return !NoColor && isTTY()
}

// Styled styles the given string with s only when colour is enabled.
func styled(s lipgloss.Style, text string) string {
	if !colorEnabled() {
		return text
	}
	return s.Render(text)
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	testStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	errorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
)

// PrintTable writes a tab-aligned table to stdout.
// When testMode is true a [TEST] badge is prepended so the active environment
// is always unambiguous.
//
// Styling is applied AFTER tabwriter has flushed so ANSI escape codes do not
// confuse tabwriter's column-width accounting (which would cause misalignment).
func PrintTable(header []string, rows [][]string, testMode bool) {
	// Write plain (unstyled) text into a buffer so tabwriter measures real widths.
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	if testMode {
		_, _ = fmt.Fprintln(w, "[TEST]")
	}
	_, _ = fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	_ = w.Flush()

	// Now apply styles line-by-line to the already-aligned output.
	headerLineIdx := 0
	if testMode {
		headerLineIdx = 1
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, line := range lines {
		switch {
		case testMode && i == 0:
			_, _ = fmt.Fprintln(os.Stdout, styled(testStyle, line))
		case i == headerLineIdx:
			_, _ = fmt.Fprintln(os.Stdout, styled(headerStyle, line))
		default:
			_, _ = fmt.Fprintln(os.Stdout, line)
		}
	}
}

// PrintJSON marshals v and writes pretty-printed JSON to stdout.
func PrintJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON: %w", err)
	}
	fmt.Println(string(b))
	return nil
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := styled(errorStyle, "Error:")
	fmt.Fprintf(os.Stderr, "%s %s\n", prefix, msg)
}
