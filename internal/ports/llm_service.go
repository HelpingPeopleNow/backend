package ports

import "context"

type MessagePair struct {
	Role    string
	Content string
}

type LLMResponse struct {
	Answer string
	Role   string
}

// LLMService is the outbound port for everything the backend needs from the
// helper's LLM adapter layer. Embed is added per VECTOR_SEARCH_PLAN §8.3.
type LLMService interface {
	Ask(ctx context.Context, systemPrompt string, userMessage string, history []MessagePair, provider string) (*LLMResponse, error)
	Health(ctx context.Context) error

	// Embed returns a 768-dim []float32 representation of text via
	// helper's Ollama embedding adapter (granite-embedding:278m). The
	// error channel distinguishes plane-network failures from
	// dimension-mismatch errors (the latter MUST be treated as a hard
	// failure — do not persist, do not retry silently).
	Embed(ctx context.Context, text string) ([]float32, error)
}
