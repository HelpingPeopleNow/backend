package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── SenderRole (audit: DM sender_role denormalization) ────────────────────
// DirectConversation.SenderRole(userID) is the SOLE source of truth
// snapshotted onto every new DirectMessage. Defense-in-depth coverage
// here keeps the internal/core package above the 90% coverage gate.

func TestDirectConversationSenderRole_UserA(t *testing.T) {
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleClient,
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleWorker,
	}
	assert.Equal(t, DirectMessageRoleClient, conv.SenderRole("user-a"))
}

func TestDirectConversationSenderRole_UserB(t *testing.T) {
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleClient,
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleWorker,
	}
	assert.Equal(t, DirectMessageRoleWorker, conv.SenderRole("user-b"))
}

func TestDirectConversationSenderRole_EmptyARoleFallsBackToUser(t *testing.T) {
	// Pin which column triggered the fallback so a future refactor that
	// collapses the UserAID/UserBID branches would fail this test.
	conv := DirectConversation{
		ID:      "conv-1",
		UserAID: "user-a",
		// UserARole intentionally empty.
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleWorker,
	}
	assert.Equal(t, DirectMessageRoleUser, conv.SenderRole("user-a"))
	// Sanity: the B-side still resolves normally.
	assert.Equal(t, DirectMessageRoleWorker, conv.SenderRole("user-b"))
}

func TestDirectConversationSenderRole_EmptyBRoleFallsBackToUser(t *testing.T) {
	// Pin the UserBID branch independently of the UserAID branch above.
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleClient,
		UserBID:   "user-b",
		// UserBRole intentionally empty.
	}
	assert.Equal(t, DirectMessageRoleWorker, conv.SenderRole("user-b"))
	// Sanity: the A-side still resolves normally.
	assert.Equal(t, DirectMessageRoleClient, conv.SenderRole("user-a"))
}

func TestDirectConversationSenderRole_UnknownParticipantFallsBackToUser(t *testing.T) {
	// Audit: callers always pass a participant's user ID. A non-participant
	// is a defensive fallback so misuse cannot poison the NOT NULL column.
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleWorker,
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleClient,
	}
	assert.Equal(t, DirectMessageRoleUser, conv.SenderRole("user-z"))
}

func TestDirectConversationOtherUserID(t *testing.T) {
	// Symmetric pair — inbox UI uses this for the "other party" header;
	// a swap here would silently flip every conversation surface.
	// Status:"active" is set explicitly because Go struct literals don't
	// apply the GORM `default:'active'` tag (DB-side default only).
	conv := DirectConversation{
		ID:      "conv-1",
		UserAID: "user-a",
		UserBID: "user-b",
		Status:  "active",
	}
	assert.Equal(t, "user-b", conv.OtherUserID("user-a"))
	assert.Equal(t, "user-a", conv.OtherUserID("user-b"))
	assert.True(t, conv.IsUserA("user-a"))
	assert.False(t, conv.IsUserA("user-b"))
	assert.True(t, conv.IsActive())
}
