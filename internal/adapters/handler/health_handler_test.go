package handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// fakeLLMHealth implements llmHealthChecker for testing
type fakeLLMHealth struct {
	err error
}

func (f *fakeLLMHealth) Health(_ context.Context) error { return f.err }

func TestNewHealthHandler(t *testing.T) {
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(&gorm.DB{}, llm)
	assert.NotNil(t, h)
	assert.Equal(t, llm, h.llm)
}

func TestNewHealthHandlerNilDB(t *testing.T) {
	llm := &fakeLLMHealth{err: nil}
	h := NewHealthHandler(nil, llm)
	assert.NotNil(t, h)
	assert.Nil(t, h.db)
}
