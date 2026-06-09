package core

import (
	"encoding/json"
	"time"
)

// Conversation holds a persisted chat session (main, worker, or client).
type Conversation struct {
	ID        string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    string          `gorm:"type:text;not null;index:idx_conv_user_type"`
	Type      string          `gorm:"type:text;not null;default:'main';index:idx_conv_user_type"`
	Title     string          `gorm:"type:text"`
	Messages  json.RawMessage `gorm:"type:jsonb;not null;default:'[]'"`
	Metadata  json.RawMessage `gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time       `gorm:"autoCreateTime"`
	UpdatedAt time.Time       `gorm:"autoUpdateTime"`
}
