package core

import (
	"testing"
)

func TestNewMoney(t *testing.T) {
	m := NewMoney(42.5)
	if m.Amount() != 42.5 {
		t.Errorf("NewMoney(42.5).Amount() = %v, want 42.5", m.Amount())
	}
}

func TestNewMoney_Zero(t *testing.T) {
	m := NewMoney(0)
	if !m.IsZero() {
		t.Error("NewMoney(0).IsZero() = false, want true")
	}
}

func TestNewMoney_Negative(t *testing.T) {
	m := NewMoney(-10)
	if !m.IsZero() {
		t.Error("NewMoney(-10).IsZero() = false, want true")
	}
}

func TestMoney_PerHour(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		expected string
	}{
		{"zero returns dash", 0, "—"},
		{"negative returns dash", -5, "—"},
		{"whole number", 50, "€50/hr"},
		{"fractional", 50.50, "€50.50/hr"},
		{"large number", 1000, "€1000/hr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMoney(tt.amount)
			got := m.PerHour()
			if got != tt.expected {
				t.Errorf("NewMoney(%v).PerHour() = %q, want %q", tt.amount, got, tt.expected)
			}
		})
	}
}

func TestMoney_String(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		expected string
	}{
		{"zero returns dash", 0, "—"},
		{"negative returns dash", -5, "—"},
		{"whole number", 50, "€50"},
		{"fractional", 50.50, "€50.50"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMoney(tt.amount)
			got := m.String()
			if got != tt.expected {
				t.Errorf("NewMoney(%v).String() = %q, want %q", tt.amount, got, tt.expected)
			}
		})
	}
}

func TestMoney_Amount(t *testing.T) {
	m := NewMoney(99.99)
	if m.Amount() != 99.99 {
		t.Errorf("Amount() = %v, want 99.99", m.Amount())
	}
}

func TestMoney_IsZero_PositiveNonZero(t *testing.T) {
	m := NewMoney(0.01)
	if m.IsZero() {
		t.Error("NewMoney(0.01).IsZero() = true, want false")
	}
}
