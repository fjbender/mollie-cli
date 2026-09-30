package cmd

import "testing"

func TestFormatVersion(t *testing.T) {
	tests := []struct {
		name                                   string
		version, commit, date, buildInfo, want string
	}{
		{"release build", "0.9.0", "abc123", "2026-10-01", "", "0.9.0 (commit abc123, built 2026-10-01)"},
		{"go install build", "dev", "none", "unknown", "v0.9.0", "v0.9.0"},
		{"local devel build", "dev", "none", "unknown", "(devel)", "dev"},
		{"no build info", "dev", "none", "unknown", "", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatVersion(tt.version, tt.commit, tt.date, tt.buildInfo); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
