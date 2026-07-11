package core

import (
	"strings"
	"testing"
)

func TestFeedbackValidate(t *testing.T) {
	tests := []struct {
		name    string
		fb      Feedback
		wantErr bool
	}{
		{
			name:    "valid general",
			fb:      Feedback{Message: "hello", PageURL: "/chat", Category: "general"},
			wantErr: false,
		},
		{
			name:    "valid bug",
			fb:      Feedback{Message: "broken", PageURL: "/find?q=test", Category: "bug"},
			wantErr: false,
		},
		{
			name:    "valid idea",
			fb:      Feedback{Message: "add dark mode", PageURL: "/chat", Category: "idea"},
			wantErr: false,
		},
		{
			name:    "valid complaint",
			fb:      Feedback{Message: "too slow", PageURL: "/inbox", Category: "complaint"},
			wantErr: false,
		},
		{
			name:    "empty message",
			fb:      Feedback{Message: "", PageURL: "/chat", Category: "general"},
			wantErr: true,
		},
		{
			name:    "message too long",
			fb:      Feedback{Message: strings.Repeat("a", 2001), PageURL: "/chat", Category: "general"},
			wantErr: true,
		},
		{
			name:    "message at max length",
			fb:      Feedback{Message: strings.Repeat("a", 2000), PageURL: "/chat", Category: "general"},
			wantErr: false,
		},
		{
			name:    "invalid category",
			fb:      Feedback{Message: "hi", PageURL: "/chat", Category: "spam"},
			wantErr: true,
		},
		{
			name:    "empty page_url",
			fb:      Feedback{Message: "hi", PageURL: "", Category: "general"},
			wantErr: true,
		},
		{
			name:    "page_url too long",
			fb:      Feedback{Message: "hi", PageURL: strings.Repeat("a", 2049), Category: "general"},
			wantErr: true,
		},
		{
			name:    "page_url at max length",
			fb:      Feedback{Message: "hi", PageURL: strings.Repeat("a", 2048), Category: "general"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fb.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFeedbackTableName(t *testing.T) {
	fb := Feedback{}
	if got := fb.TableName(); got != "feedback" {
		t.Errorf("TableName() = %q, want %q", got, "feedback")
	}
}

func TestValidCategories(t *testing.T) {
	expected := []string{"bug", "idea", "complaint", "general"}
	for _, cat := range expected {
		if !ValidCategories[cat] {
			t.Errorf("ValidCategories[%q] = false, want true", cat)
		}
	}
	// Invalid categories should not be in the map.
	for _, cat := range []string{"spam", "other", ""} {
		if ValidCategories[cat] {
			t.Errorf("ValidCategories[%q] = true, want false", cat)
		}
	}
}

func TestValidStatuses(t *testing.T) {
	expected := []string{"open", "in_progress", "resolved", "dismissed"}
	for _, s := range expected {
		if !ValidStatuses[s] {
			t.Errorf("ValidStatuses[%q] = false, want true", s)
		}
	}
	for _, s := range []string{"pending", "closed", ""} {
		if ValidStatuses[s] {
			t.Errorf("ValidStatuses[%q] = true, want false", s)
		}
	}
}
