package core

import "time"

// DirectMessageReport records a user report for a direct conversation.
type DirectMessageReport struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID string    `gorm:"type:uuid;not null;index" json:"conversation_id"`
	ReportedBy     string    `gorm:"type:text;not null" json:"reported_by"`
	Reason         string    `gorm:"type:text;not null;default:''" json:"reason"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (DirectMessageReport) TableName() string { return "direct_message_reports" }
