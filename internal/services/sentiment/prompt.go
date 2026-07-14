package sentiment

import (
	"fmt"
	"strings"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

// SystemPrompt is the fixed system prompt used for sentiment scoring.
const SystemPrompt = `You are a conversation tone analyst. Analyze the following conversation between a client and a tradesperson (plumber, electrician, cleaner, etc.) on a home-services platform.

Score the overall tone from 0 to 10:
- 0 = extremely angry, hostile, threatening
- 1-2 = very frustrated, rude, aggressive
- 3-4 = somewhat frustrated, tense
- 5 = neutral, professional
- 6-7 = friendly, cooperative
- 8-9 = very positive, warm
- 10 = extremely happy, enthusiastic

Respond with JSON ONLY, no other text:
{"score": <integer 0-10>, "reason": "<≤120 char explanation>"}`

// FormatTranscript renders the last messages as a transcript where each
// line is "<LABEL>: <body>". Labels are derived from sender_role:
//
//	worker -> TRADER
//	client -> CLIENT
//	other  -> USER
func FormatTranscript(messages []core.DirectMessage) string {
	if len(messages) == 0 {
		return ""
	}
	var b strings.Builder
	for i, m := range messages {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(labelForRole(m.SenderRole))
		b.WriteString(": ")
		b.WriteString(m.Body)
	}
	return b.String()
}

func labelForRole(role string) string {
	switch role {
	case core.DirectMessageRoleWorker:
		return "TRADER"
	case core.DirectMessageRoleClient:
		return "CLIENT"
	default:
		return "USER"
	}
}

// FormatUserMessage wraps the transcript into the user message sent to the LLM.
func FormatUserMessage(transcript string) string {
	return fmt.Sprintf("Analyze this conversation:\n\n%s", transcript)
}
