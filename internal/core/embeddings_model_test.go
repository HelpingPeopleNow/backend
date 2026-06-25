package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Conversation ────────────────────────────────────────────────────

func TestConversationIsNew(t *testing.T) {
	c := Conversation{}
	assert.True(t, c.IsNew())
	c.ID = "some-id"
	assert.False(t, c.IsNew())
}

// ── DirectConversation ──────────────────────────────────────────────

func TestDirectConversationTableName(t *testing.T) {
	dc := DirectConversation{}
	assert.Equal(t, "direct_conversations", dc.TableName())
}

func TestDirectConversationIsActive(t *testing.T) {
	assert.True(t, DirectConversation{Status: "active"}.IsActive())
	assert.False(t, DirectConversation{Status: "blocked"}.IsActive())
}

func TestDirectConversationIsBlocked(t *testing.T) {
	assert.True(t, DirectConversation{Status: "blocked"}.IsBlocked())
	assert.False(t, DirectConversation{Status: "active"}.IsBlocked())
}

// ── DirectMessage ───────────────────────────────────────────────────

func TestDirectMessageTableName(t *testing.T) {
	dm := DirectMessage{}
	assert.Equal(t, "direct_messages", dm.TableName())
}

// ── PhoneNumber ─────────────────────────────────────────────────────

func TestPhoneNumberNew(t *testing.T) {
	p := NewPhoneNumber("  +34600123456  ")
	assert.Equal(t, "+34600123456", p.String())
}

func TestPhoneNumberIsEmpty(t *testing.T) {
	assert.True(t, NewPhoneNumber("").IsEmpty())
	assert.False(t, NewPhoneNumber("+34600123456").IsEmpty())
}

// ── Money ───────────────────────────────────────────────────────────

func TestMoneyAmount(t *testing.T) {
	assert.InDelta(t, 25.5, NewMoney(25.5).Amount(), 0.001)
}

func TestMoneyString(t *testing.T) {
	assert.NotEmpty(t, NewMoney(25.5).String())
}

func TestMoneyStringZero(t *testing.T) {
	assert.Equal(t, "—", NewMoney(0).String())
}

// ── HashField ───────────────────────────────────────────────────────

func TestHashFieldDeterministic(t *testing.T) {
	h1 := HashField("hello")
	h2 := HashField("hello")
	require.Len(t, h1, 64)
	assert.Equal(t, h1, h2)
}

// ── joinJSONArray ───────────────────────────────────────────────────

func TestJoinJSONArrayValid(t *testing.T) {
	assert.Equal(t, "GAS SAFE, NICEIC", joinJSONArray(`["GAS SAFE","NICEIC"]`))
}

func TestJoinJSONArrayEmpty(t *testing.T) {
	assert.Equal(t, "", joinJSONArray(""))
}

func TestJoinJSONArrayInvalid(t *testing.T) {
	assert.Equal(t, "not json", joinJSONArray("not json"))
}

// ── BuildFieldTexts ─────────────────────────────────────────────────

func TestBuildFieldTextsFull(t *testing.T) {
	wp := &WorkerProfile{
		Profession:     "plumber",
		Bio:            "10 years",
		Certifications: `["GAS SAFE"]`,
		City:           "Madrid",
		Languages:      `["Spanish"]`,
		BusinessName:   "Bob's",
	}
	texts := BuildFieldTexts(wp)
	assert.Equal(t, "Plumber", texts["profession"])
	assert.Equal(t, "plumber", texts["profession_raw"])
	assert.Equal(t, "10 years", texts["bio"])
	assert.Equal(t, "GAS SAFE", texts["certifications"])
	assert.Equal(t, "Madrid", texts["city"])
	assert.Equal(t, "Spanish", texts["languages"])
	assert.Equal(t, "Bob's", texts["business_name"])
}

func TestBuildFieldTextsEmpty(t *testing.T) {
	assert.Empty(t, BuildFieldTexts(&WorkerProfile{}))
}

func TestBuildFieldTextsCanonicalProfession(t *testing.T) {
	texts := BuildFieldTexts(&WorkerProfile{Profession: "Painter"})
	assert.Equal(t, "Painter", texts["profession"])
	_, hasRaw := texts["profession_raw"]
	assert.False(t, hasRaw)
}

// ── normalizeProfessionForEmbedding ─────────────────────────────────

func TestNormalizeProfessionForEmbedding(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"electricista", "Electrician"},
		{"plumber", "Plumber"},
		{"fontanero", "Plumber"},
		{"cleaner", "Cleaner"},
		{"handyman", "Handyman"},
		{"carpenter", "Carpenter"},
		{"painter", "Painter"},
		{"landscaper", "Landscaper"},
		{"roofer", "Roofer"},
		{"hvac", "HVAC Technician"},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, normalizeProfessionForEmbedding(tc.input))
		})
	}
}

// ── WorkerEmbedding ─────────────────────────────────────────────────

func TestWorkerEmbeddingTableName(t *testing.T) {
	we := WorkerEmbedding{}
	assert.Equal(t, "worker_embeddings", we.TableName())
}

// ── FieldWeights ────────────────────────────────────────────────────

func TestFieldWeightsKeys(t *testing.T) {
	for _, key := range []string{"profession", "bio", "certifications", "city", "languages", "business_name"} {
		_, ok := FieldWeights[key]
		assert.True(t, ok, "missing key: %s", key)
	}
}
