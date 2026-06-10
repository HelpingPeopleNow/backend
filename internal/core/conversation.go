package core

import (
	"encoding/json"
	"time"
)

// Conversation holds a persisted chat session (main, worker, or client).
type Conversation struct {
	ID        string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    string          `gorm:"type:text;not null;index:idx_conv_user_type" json:"user_id"`
	Type      string          `gorm:"type:text;not null;default:'main';index:idx_conv_user_type" json:"type"`
	Title     string          `gorm:"type:text" json:"title"`
	Messages  json.RawMessage `gorm:"type:jsonb;not null;default:'[]'" json:"messages"`
	Metadata  json.RawMessage `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time       `gorm:"autoUpdateTime" json:"updated_at"`
}
