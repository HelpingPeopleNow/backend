package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDirectConversationSenderRole_AEmptyFallsBackToUser pins the UserA branch
// of the empty-column fallback. With UserARole intentionally empty, SenderRole()
// must return "user" for UserAID and resolve UserBID normally via UserBRole.
func TestDirectConversationSenderRole_EmptyARoleFallsBackToUser(t *testing.T) {
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

// TestDirectConversationSenderRole_BEmptyFallsBackToUser pins the UserB branch
// independently of the UserA branch above. With UserBRole intentionally empty,
// SenderRole() must return "user" for UserBID and resolve UserAID normally via
// UserARole. Each branch is exercised in isolation so a future refactor that
// collapses both empty-fallback paths into one would fail this test alongside
// its sibling.
func TestDirectConversationSenderRole_EmptyBRoleFallsBackToUser(t *testing.T) {
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleClient,
		UserBID:   "user-b",
		// UserBRole intentionally empty.
	}
	// Probing as UserBID with empty UserBRole → fallback to "user".
	assert.Equal(t, DirectMessageRoleUser, conv.SenderRole("user-b"))
	// Sanity: the A-side still resolves normally.
	assert.Equal(t, DirectMessageRoleClient, conv.SenderRole("user-a"))
}

// TestDirectConversationSenderRole_UnknownParticipantFallsBackToUser:
// callers always pass a participant's user ID. A non-participant is a
// defensive fallback so misuse cannot poison the NOT NULL column.
func TestDirectConversationSenderRole_UnknownParticipantFallsBackToUser(t *testing.T) {
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleWorker,
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleClient,
	}
	assert.Equal(t, DirectMessageRoleUser, conv.SenderRole("user-z"))
}

// TestDirectConversationSymmetricRoleResolution guards against swapping the
// UserARole/UserBRole branches in SenderRole(). For a fully-populated conv,
// probing as UserA must yield UserARole and probing as UserB must yield
// UserBRole. Without this, a future "code golf" could swap the branches and
// every existing DM conversation would silently flip sender_role at send time.
func TestDirectConversationSymmetricRoleResolution(t *testing.T) {
	conv := DirectConversation{
		ID:        "conv-1",
		UserAID:   "user-a",
		UserARole: DirectMessageRoleClient,
		UserBID:   "user-b",
		UserBRole: DirectMessageRoleWorker,
	}
	assert.Equal(t, DirectMessageRoleClient, conv.SenderRole("user-a"))
	assert.Equal(t, DirectMessageRoleWorker, conv.SenderRole("user-b"))
}

// TestDirectConversationOtherUserID: symmetric pair — inbox UI uses this
// for the "other party" header; a swap here would silently flip every
// conversation surface.
//
// Status:"active" is set explicitly because Go struct literals don't honor
// the GORM `default:'active'` tag (DB-side default only).
func TestDirectConversationOtherUserID(t *testing.T) {
	conv := DirectConversation{
		ID:      "conv-1",
		UserAID: "user-a",
		UserBID: "user-b",
		Status:  "active",
	}
	assert.Equal(t, "user-b", conv.OtherUserID("user-a"))
	assert.Equal(t, "user-a", conv.OtherUserID("user-b"))
}
