package core

import (
	"encoding/json"
	"time"
)

type Conversation struct {
	ID        string          `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	UserID    string          `gorm:"type:text;not null;index:idx_conv_user_type" json:"user_id"`
	Type      string          `gorm:"type:text;not null;default:'main';index:idx_conv_user_type" json:"type"`
	Metadata  json.RawMessage `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedAt time.Time       `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time       `gorm:"autoUpdateTime" json:"updated_at"`
}

type Message struct {
	ID             string    `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ConversationID string    `gorm:"type:uuid;not null;index" json:"conversation_id"`
	Role           string    `gorm:"type:text;not null" json:"role"`
	Content        string    `gorm:"type:text;not null" json:"content"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (c *Conversation) IsNew() bool {
	return c.ID == ""
}
