package ports

import (
	"context"
	"encoding/json"

	"github.com/HelpingPeopleNow/backend/internal/core"
)

type ChatRepository interface {
	SaveMessages(ctx context.Context, userID string, convType string, userMessage, assistantResponse string, conversationID string, fields json.RawMessage, metadata map[string]interface{}, workersJSON string) (string, error)

	LoadConversation(ctx context.Context, userID string, convType string) (*core.Conversation, error)

	ListConversations(ctx context.Context, userID string, convType string, limit, offset int) ([]core.Conversation, int64, error)

	GetConversation(ctx context.Context, userID, conversationID string) (*core.Conversation, error)

	GetMessages(ctx context.Context, conversationID string) ([]core.Message, error)
}
