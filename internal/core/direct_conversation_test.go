package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirectConversationOtherUserID(t *testing.T) {
	c := DirectConversation{UserAID: "alice", UserBID: "bob"}

	assert.Equal(t, "bob", c.OtherUserID("alice"))
	assert.Equal(t, "alice", c.OtherUserID("bob"))
	assert.Equal(t, "alice", c.OtherUserID("unknown"))
}

func TestDirectConversationIsUserA(t *testing.T) {
	c := DirectConversation{UserAID: "alice", UserBID: "bob"}

	assert.True(t, c.IsUserA("alice"))
	assert.False(t, c.IsUserA("bob"))
}

func TestDirectMessageReportTableName(t *testing.T) {
	r := DirectMessageReport{}
	assert.Equal(t, "direct_message_reports", r.TableName())
}

func TestDirectConversationStatus(t *testing.T) {
	active := DirectConversation{Status: "active"}
	blocked := DirectConversation{Status: "blocked"}

	assert.True(t, active.IsActive())
	assert.False(t, active.IsBlocked())
	assert.True(t, blocked.IsBlocked())
	assert.False(t, blocked.IsActive())
}
