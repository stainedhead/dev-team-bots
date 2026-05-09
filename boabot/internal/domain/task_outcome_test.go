package domain_test

import (
	"testing"

	"github.com/stainedhead/dev-team-bots/boabot/internal/domain"
)

func TestParseTaskOutcome(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   domain.DirectTaskStatus
	}{
		{
			name:   "no marker returns empty",
			output: "All done, everything worked great.",
			want:   "",
		},
		{
			name:   "blocked marker",
			output: "Could not proceed.\nTASK_OUTCOME: blocked",
			want:   domain.DirectTaskStatusBlocked,
		},
		{
			name:   "errored marker",
			output: "Something failed internally.\nTASK_OUTCOME: errored",
			want:   domain.DirectTaskStatusErrored,
		},
		{
			name:   "marker with leading whitespace",
			output: "  TASK_OUTCOME: blocked  ",
			want:   domain.DirectTaskStatusBlocked,
		},
		{
			name:   "marker mid-output",
			output: "Line one.\nTASK_OUTCOME: errored\nMore text after.",
			want:   domain.DirectTaskStatusErrored,
		},
		{
			name:   "unknown value ignored",
			output: "TASK_OUTCOME: unknown",
			want:   "",
		},
		{
			name:   "empty output",
			output: "",
			want:   "",
		},
		{
			name:   "partial prefix not matched",
			output: "TASK_OUTCOME_EXTRA: blocked",
			want:   "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.ParseTaskOutcome(tc.output)
			if got != tc.want {
				t.Errorf("ParseTaskOutcome(%q) = %q, want %q", tc.output, got, tc.want)
			}
		})
	}
}
