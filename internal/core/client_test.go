package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClientProfileMergeFields(t *testing.T) {
	cp := &ClientProfile{}
	fields := map[string]interface{}{
		"full_name":         "Alvaro",
		"phone":             "+34600123456",
		"city":              "Madrid",
		"address":           "Calle Mayor 1",
		"bio":               "Need a plumber",
		"preferred_contact": "phone",
		"property_type":     "apartment",
		"notes":             "Urgent",
	}
	cp.MergeFields(fields)

	assert.Equal(t, "Alvaro", cp.FullName)
	assert.Equal(t, "+34600123456", cp.Phone)
	assert.Equal(t, "Madrid", cp.City)
	assert.Equal(t, "Calle Mayor 1", cp.Address)
	assert.Equal(t, "Need a plumber", cp.Bio)
	assert.Equal(t, "phone", cp.PreferredContact)
	assert.Equal(t, "apartment", cp.PropertyType)
	assert.Equal(t, "Urgent", cp.Notes)
}

func TestClientProfileMergeFieldsEmpty(t *testing.T) {
	cp := &ClientProfile{FullName: "existing"}
	cp.MergeFields(map[string]interface{}{})
	assert.Equal(t, "existing", cp.FullName)
}

func TestClientProfileToDTO(t *testing.T) {
	cp := &ClientProfile{ID: "c1", FullName: "Alvaro"}
	dto := cp.ToDTO()
	assert.Equal(t, "c1", dto.ID)
	assert.Equal(t, "Alvaro", dto.FullName)
}
