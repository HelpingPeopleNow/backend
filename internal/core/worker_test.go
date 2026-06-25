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
