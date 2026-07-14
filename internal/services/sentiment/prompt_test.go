package sentiment

import (
	"testing"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

func TestFormatTranscript(t *testing.T) {
	cases := []struct {
		name     string
		msgs     []core.DirectMessage
		expected string
	}{
		{
			name:     "empty",
			msgs:     []core.DirectMessage{},
			expected: "",
		},
		{
			name: "single message",
			msgs: []core.DirectMessage{
				{SenderRole: core.DirectMessageRoleClient, Body: "Hello"},
			},
			expected: "CLIENT: Hello",
		},
		{
			name: "mixed roles",
			msgs: []core.DirectMessage{
				{SenderRole: core.DirectMessageRoleClient, Body: "Hi"},
				{SenderRole: core.DirectMessageRoleWorker, Body: "Hello"},
				{SenderRole: core.DirectMessageRoleUser, Body: "Hey"},
				{SenderRole: "", Body: "Legacy"},
			},
			expected: "CLIENT: Hi\nTRADER: Hello\nUSER: Hey\nUSER: Legacy",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTranscript(tc.msgs)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
