package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/HelpingPeopleNow/backend/internal/core"
	"github.com/HelpingPeopleNow/backend/internal/ports"
	"gorm.io/gorm"
)

type GormChatRepository struct {
	db *gorm.DB
}

func NewGormChatRepository(db *gorm.DB) ports.ChatRepository {
	return &GormChatRepository{db: db}
}

func (r *GormChatRepository) SaveMessages(
	ctx context.Context,
	userID string,
	convType string,
	userMessage string,
	assistantResponse string,
	conversationID string,
	fields json.RawMessage,
	metadata map[string]interface{},
) (string, error) {
	if conversationID != "" {
		var existing core.Conversation
		if err := r.db.WithContext(ctx).First(&existing, "id = ? AND user_id = ?", conversationID, userID).Error; err != nil {
			conversationID = ""
		} else {
			messages := []core.Message{
				{ConversationID: conversationID, Role: "user", Content: userMessage},
				{ConversationID: conversationID, Role: "assistant", Content: assistantResponse},
			}
			for _, msg := range messages {
				if err := r.db.WithContext(ctx).Create(&msg).Error; err != nil {
					slog.Error("failed to save message", "conversation_id", conversationID, "role", msg.Role, "error", err)
					return "", err
				}
			}

			updates := map[string]interface{}{
				"updated_at": time.Now(),
			}
			if fields != nil || len(metadata) > 0 {
				meta := map[string]interface{}{}
				if existing.Metadata != nil {
					_ = json.Unmarshal(existing.Metadata, &meta)
				}
				if fields != nil {
					meta["extracted_fields"] = fields
				}
				for k, v := range metadata {
					meta[k] = v
				}
				metaJSON, _ := json.Marshal(meta)
				updates["metadata"] = metaJSON
			}

			if err := r.db.WithContext(ctx).Model(&core.Conversation{}).Where("id = ?", conversationID).Updates(updates).Error; err != nil {
				slog.Error("failed to update conversation metadata", "conversation_id", conversationID, "error", err)
				return "", err
			}
			return conversationID, nil
		}
	}

	meta := map[string]interface{}{}
	if fields != nil {
		meta["extracted_fields"] = fields
	}
	for k, v := range metadata {
		meta[k] = v
	}
	if convType == "worker" || convType == "client" {
		meta["type"] = "profile_intake"
		meta["completed"] = false
	}
	metaJSON, _ := json.Marshal(meta)

	conv := core.Conversation{
		UserID:   userID,
		Type:     convType,
		Metadata: metaJSON,
	}
	if err := r.db.WithContext(ctx).Create(&conv).Error; err != nil {
		slog.Error("failed to create conversation", "user_id", userID, "type", convType, "error", err)
		return "", err
	}

	messages := []core.Message{
		{ConversationID: conv.ID, Role: "user", Content: userMessage},
		{ConversationID: conv.ID, Role: "assistant", Content: assistantResponse},
	}
	for _, msg := range messages {
		if err := r.db.WithContext(ctx).Create(&msg).Error; err != nil {
			slog.Error("failed to save initial message", "conversation_id", conv.ID, "role", msg.Role, "error", err)
			return "", err
		}
	}

	return conv.ID, nil
}

func (r *GormChatRepository) LoadConversation(ctx context.Context, userID string, convType string) (*core.Conversation, error) {
	var conv core.Conversation
	err := r.db.WithContext(ctx).Where("user_id = ? AND type = ?", userID, convType).Order("updated_at DESC").First(&conv).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		slog.Error("failed to load conversation", "user_id", userID, "type", convType, "error", err)
		return nil, err
	}
	return &conv, nil
}

func (r *GormChatRepository) ListConversations(ctx context.Context, userID string, convType string, limit, offset int) ([]core.Conversation, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	query := r.db.WithContext(ctx).Model(&core.Conversation{}).Where("user_id = ?", userID)
	if convType != "" {
		query = query.Where("type = ?", convType)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		slog.Error("failed to count conversations", "user_id", userID, "convType", convType, "error", err)
		return nil, 0, fmt.Errorf("count conversations: %w", err)
	}

	var convs []core.Conversation
	if err := query.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&convs).Error; err != nil {
		slog.Error("failed to list conversations", "user_id", userID, "convType", convType, "error", err)
		return nil, 0, fmt.Errorf("list conversations: %w", err)
	}
	return convs, total, nil
}

func (r *GormChatRepository) GetConversation(ctx context.Context, userID, conversationID string) (*core.Conversation, error) {
	var conv core.Conversation
	err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", conversationID, userID).First(&conv).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		slog.Error("failed to get conversation", "conversation_id", conversationID, "user_id", userID, "error", err)
		return nil, err
	}
	return &conv, nil
}

func (r *GormChatRepository) GetMessages(ctx context.Context, conversationID string) ([]core.Message, error) {
	var msgs []core.Message
	err := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID).Order("created_at ASC").Find(&msgs).Error
	if err != nil {
		slog.Error("failed to get messages", "conversation_id", conversationID, "error", err)
		return nil, err
	}
	return msgs, nil
}
