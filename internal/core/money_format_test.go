package core

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFmtMoneyWholeAmount(t *testing.T) {
	assert.Equal(t, "€50", fmtMoney(50.0))
	assert.Equal(t, "€100", fmtMoney(100.0))
	assert.Equal(t, "€0", fmtMoney(0.0))
	assert.Equal(t, "€-25", fmtMoney(-25.0))
}

func TestFmtMoneyDecimalAmount(t *testing.T) {
	assert.Equal(t, "€50.50", fmtMoney(50.50))
	assert.Equal(t, "€99.99", fmtMoney(99.99))
	assert.Equal(t, "€0.01", fmtMoney(0.01))
	assert.Equal(t, "€-10.75", fmtMoney(-10.75))
}

func TestFmtMoneyEdgeCases(t *testing.T) {
	assert.Equal(t, "€+Inf", fmtMoney(math.Inf(1)))
	assert.Equal(t, "€-Inf", fmtMoney(math.Inf(-1)))
	assert.Equal(t, "€NaN", fmtMoney(math.NaN()))
}
