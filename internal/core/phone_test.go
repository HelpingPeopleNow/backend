package core

import (
	"testing"
)

func TestNewPhoneNumber(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain number", "+34612345678", "+34612345678"},
		{"with spaces", "  +34 612 345 678  ", "+34 612 345 678"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPhoneNumber(tt.input)
			if p.String() != tt.expected {
				t.Errorf("NewPhoneNumber(%q).String() = %q, want %q", tt.input, p.String(), tt.expected)
			}
		})
	}
}

func TestPhoneNumber_IsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
	}{
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"valid number", "+34612345678", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPhoneNumber(tt.input)
			if p.IsEmpty() != tt.wantNil {
				t.Errorf("NewPhoneNumber(%q).IsEmpty() = %v, want %v", tt.input, p.IsEmpty(), tt.wantNil)
			}
		})
	}
}

func TestPhoneNumber_String(t *testing.T) {
	p := NewPhoneNumber("+1234567890")
	if p.String() != "+1234567890" {
		t.Errorf("String() = %q, want %q", p.String(), "+1234567890")
	}
}

func TestPhoneNumber_TrimsSpace(t *testing.T) {
	p := NewPhoneNumber("  +1234567890  ")
	if p.String() != "+1234567890" {
		t.Errorf("String() = %q, want %q (should be trimmed)", p.String(), "+1234567890")
	}
}
