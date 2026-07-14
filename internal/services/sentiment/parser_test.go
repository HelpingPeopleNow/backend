package sentiment

import (
	"strings"
	"testing"
)

func TestParseScore(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantScore  int16
		wantReason string
		wantErr    bool
	}{
		{
			name:       "happy path",
			input:      `{"score": 7, "reason": "Professional and respectful tone"}`,
			wantScore:  7,
			wantReason: "Professional and respectful tone",
		},
		{
			name:       "markdown fence",
			input:      "```json\n{\"score\": 8, \"reason\": \"Great\"}\n```",
			wantScore:  8,
			wantReason: "Great",
		},
		{
			name:       "zero score",
			input:      `{"score": 0, "reason": "Angry"}`,
			wantScore:  0,
			wantReason: "Angry",
		},
		{
			name:       "ten score",
			input:      `{"score": 10, "reason": "Perfect"}`,
			wantScore:  10,
			wantReason: "Perfect",
		},
		{
			name:    "out of range high",
			input:   `{"score": 11, "reason": "Too high"}`,
			wantErr: true,
		},
		{
			name:    "out of range low",
			input:   `{"score": -1, "reason": "Too low"}`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `not json`,
			wantErr: true,
		},
		{
			name:    "missing score",
			input:   `{"reason": "no score"}`,
			wantErr: true,
		},
		{
			name:       "reason truncation",
			input:      `{"score": 5, "reason": "` + strings.Repeat("a", 200) + `"}`,
			wantScore:  5,
			wantReason: strings.Repeat("a", 120),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			score, reason, err := ParseScore(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got score=%d reason=%q", score, reason)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if score != tc.wantScore {
				t.Errorf("score: got %d, want %d", score, tc.wantScore)
			}
			if reason != tc.wantReason {
				t.Errorf("reason: got %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
