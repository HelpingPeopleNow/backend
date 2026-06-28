package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerProfileMergeFields(t *testing.T) {
	wp := &WorkerProfile{}
	fields := map[string]interface{}{
		"profession":        "plumber",
		"bio":               "10 years experience",
		"city":              "Madrid",
		"business_name":     "Bob's Plumbing",
		"hourly_rate":       "25.5",
		"phone":             "+34600123456",
		"years_experience":  "10",
		"emergency_service": "true",
		"has_insurance":     "true",
		"certifications":    `["GAS SAFE"]`,
		"languages":         `["Spanish","English"]`,
		"social_links":      `[{"platform":"instagram","url":"ig.com/bob"}]`,
	}
	wp.MergeFields(fields)

	assert.Equal(t, "plumber", wp.Profession)
	assert.Equal(t, "10 years experience", wp.Bio)
	assert.Equal(t, "Madrid", wp.City)
	assert.Equal(t, "Bob's Plumbing", wp.BusinessName)
	assert.Equal(t, "+34600123456", wp.Phone)
	assert.True(t, wp.EmergencyService)
	assert.True(t, wp.HasInsurance)
}

func TestWorkerProfileMergeFieldsEmpty(t *testing.T) {
	wp := &WorkerProfile{Profession: "existing"}
	wp.MergeFields(map[string]interface{}{})
	assert.Equal(t, "existing", wp.Profession)
}

func TestWorkerProfileBadges(t *testing.T) {
	wp := &WorkerProfile{
		HasInsurance:     true,
		EmergencyService: true,
		FreeEstimate:     true,
		BusinessName:     "Co.",
	}
	badges := wp.Badges()
	require.Len(t, badges, 3)
}

func TestWorkerProfileFormattedRate(t *testing.T) {
	wp := &WorkerProfile{HourlyRate: 25.5}
	assert.Contains(t, wp.FormattedRate(), "25")
}

func TestWorkerProfileToDTO(t *testing.T) {
	wp := &WorkerProfile{ID: "w1", Profession: "plumber"}
	dto := wp.ToDTO()
	assert.Equal(t, "w1", dto.ID)
	assert.Equal(t, "plumber", dto.Profession)
}

func TestWorkerProfileSearchSummary(t *testing.T) {
	wp := &WorkerProfile{ID: "w1", Profession: "plumber", City: "Madrid"}
	s := wp.SearchSummary(1)
	assert.Contains(t, s, "plumber")
	assert.Contains(t, s, "Madrid")
}

func TestFindTraderCards(t *testing.T) {
	workers := []WorkerProfile{
		{ID: "w1", Profession: "plumber"},
		{ID: "w2", Profession: "electrician"},
	}
	cards := FindTraderCards(workers)
	require.Len(t, cards, 2)
}

func TestWorkerProfileToFindTraderCard(t *testing.T) {
	wp := &WorkerProfile{ID: "w1", Profession: "plumber", City: "Madrid"}
	card := wp.ToFindTraderCard()
	assert.Equal(t, "w1", card.ID)
}

// ── Slug tests ─────────────────────────────────────────────────────

func TestGenerateSlug(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Acme Plumbing", "acme-plumbing"},
		{"  Bob's   Plumbing  ", "bobs-plumbing"},
		{"John & Mary's LLC", "john-marys-llc"},
		{"HELPING PEOPLE NOW", "helping-people-now"},
		{"special!@#$%^chars", "specialchars"},
	}
	for _, tt := range tests {
		got := GenerateSlug(tt.input)
		assert.Equal(t, tt.want, got, "GenerateSlug(%q)", tt.input)
	}
}

func TestGenerateSlugTruncation(t *testing.T) {
	long := "A Very Long Business Name That Should Definitely Be Truncated Because It Exceeds Fifty Characters"
	slug := GenerateSlug(long)
	assert.LessOrEqual(t, len(slug), 50, "slug should be truncated to 50 chars")
	assert.Equal(t, "a-very-long-business-name-that-should-definitely-b", slug)
}

func TestGenerateSlugStripsLeadingTrailingHyphens(t *testing.T) {
	slug := GenerateSlug("  -hello world-   ")
	assert.Equal(t, "hello-world", slug)
}

func TestValidateSlug(t *testing.T) {
	assert.True(t, ValidateSlug("acme-plumbing"))
	assert.True(t, ValidateSlug("hello"))
	assert.True(t, ValidateSlug("a-b-c"))
	assert.False(t, ValidateSlug(""))
	assert.False(t, ValidateSlug("INVALID"))
	assert.False(t, ValidateSlug("with spaces"))
	assert.False(t, ValidateSlug("-leading-hyphen"))
	assert.False(t, ValidateSlug("trailing-hyphen-"))
	assert.False(t, ValidateSlug("double--hyphen"))
	assert.False(t, ValidateSlug("special!chars"))
}
