package core

import "testing"

func TestMergeFieldsClientAllFields(t *testing.T) {
	fields := map[string]interface{}{
		"full_name":         "Alvaro",
		"phone":             "+34600123456",
		"city":              "Madrid",
		"address":           "Calle Mayor 1",
		"bio":               "Homeowner",
		"preferred_contact": "WhatsApp",
		"property_type":     "apartment",
		"notes":             "Needs weekend availability",
	}
	c := &ClientProfile{}
	c.MergeFields(fields)

	if c.FullName != "Alvaro" {
		t.Fatalf("full_name: got %q", c.FullName)
	}
	if c.Phone != "+34600123456" {
		t.Fatalf("phone: got %q", c.Phone)
	}
	if c.City != "Madrid" {
		t.Fatalf("city: got %q", c.City)
	}
	if c.Address != "Calle Mayor 1" {
		t.Fatalf("address: got %q", c.Address)
	}
	if c.Bio != "Homeowner" {
		t.Fatalf("bio: got %q", c.Bio)
	}
	if c.PreferredContact != "WhatsApp" {
		t.Fatalf("preferred_contact: got %q", c.PreferredContact)
	}
	if c.PropertyType != "apartment" {
		t.Fatalf("property_type: got %q", c.PropertyType)
	}
	if c.Notes != "Needs weekend availability" {
		t.Fatalf("notes: got %q", c.Notes)
	}
}

func TestMergeFieldsClientPartialUpdate(t *testing.T) {
	c := &ClientProfile{
		FullName: "Alvaro",
		Phone:    "+34600123456",
		City:     "Madrid",
	}
	fields := map[string]interface{}{
		"full_name": "Alvaro T",
	}
	c.MergeFields(fields)

	if c.FullName != "Alvaro T" {
		t.Fatalf("full_name: got %q", c.FullName)
	}
	if c.Phone != "+34600123456" {
		t.Fatalf("phone should be preserved, got %q", c.Phone)
	}
	if c.City != "Madrid" {
		t.Fatalf("city should be preserved, got %q", c.City)
	}
}

func TestMergeFieldsClientNullClears(t *testing.T) {
	c := &ClientProfile{
		FullName: "Alvaro",
		Phone:    "+34600123456",
	}
	fields := map[string]interface{}{
		"phone": nil,
	}
	c.MergeFields(fields)

	if c.Phone != "" {
		t.Fatalf("phone should be cleared, got %q", c.Phone)
	}
	if c.FullName != "Alvaro" {
		t.Fatalf("full_name should be preserved, got %q", c.FullName)
	}
}

func TestMergeFieldsClientEmptyFields(t *testing.T) {
	c := &ClientProfile{FullName: "Alvaro"}
	fields := map[string]interface{}{}
	c.MergeFields(fields)

	if c.FullName != "Alvaro" {
		t.Fatalf("full_name should be preserved, got %q", c.FullName)
	}
}

func TestClientToDTO(t *testing.T) {
	c := ClientProfile{
		ID:               "c1",
		UserID:           "user-1",
		FullName:         "Alvaro",
		Phone:            "+34600123456",
		City:             "Madrid",
		Address:          "Calle Mayor 1",
		Bio:              "Homeowner",
		PreferredContact: "WhatsApp",
		PropertyType:     "apartment",
		Notes:            "Weekend only",
	}
	dto := c.ToDTO()
	if dto.ID != "c1" {
		t.Fatalf("id: got %q", dto.ID)
	}
	if dto.FullName != "Alvaro" {
		t.Fatalf("full_name: got %q", dto.FullName)
	}
	if dto.Phone != "+34600123456" {
		t.Fatalf("phone: got %q", dto.Phone)
	}
	if dto.PreferredContact != "WhatsApp" {
		t.Fatalf("preferred_contact: got %q", dto.PreferredContact)
	}
}
