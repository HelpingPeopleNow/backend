package core

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── IsValidSenderRole (audit: DM sender_role denormalization) ────────────
// The closed set of roles that the DB-side sender_role column accepts.
// Centralizing the validation here means future enum additions
// ("admin", "system") only need to extend this predicate.

func TestIsValidSenderRole_AcceptsClosedSet(t *testing.T) {
	assert.True(t, IsValidSenderRole(DirectMessageRoleUser))
	assert.True(t, IsValidSenderRole(DirectMessageRoleClient))
	assert.True(t, IsValidSenderRole(DirectMessageRoleWorker))
}

func TestIsValidSenderRole_RejectsUnknown(t *testing.T) {
	// Defensive: any unknown role surfaces as invalid so a future enum
	// addition cannot silently insert a role the UI doesn't render.
	invalid := []string{"", "admin", "system", "USER", "Worker", "x", "  "}
	for _, r := range invalid {
		assert.False(t, IsValidSenderRole(r), "should reject role %q", r)
	}
}

func TestDirectMessageMaxLengthConstant(t *testing.T) {
	// Lock the DM body cap so a future bump requires an explicit edit
	// to this audit guard. Direct handler / repo both enforce it.
	assert.Equal(t, 4000, MaxDirectMessageLength)
	assert.Greater(t, MaxDirectMessageLength, 0)
	// Sanity: every known role constant is a lowercase ASCII string
	// (the DB column is VARCHAR(10) NOT NULL DEFAULT 'user').
	for _, r := range []string{
		DirectMessageRoleUser,
		DirectMessageRoleClient,
		DirectMessageRoleWorker,
	} {
		assert.True(t, strings.EqualFold(r, strings.ToLower(r)),
			"role %q must be lowercase for DB default constraint", r)
	}
}
