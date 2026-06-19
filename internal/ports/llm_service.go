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

type LLMService interface {
	Ask(ctx context.Context, systemPrompt string, userMessage string, history []MessagePair, provider string) (*LLMResponse, error)
	Health(ctx context.Context) error
}
