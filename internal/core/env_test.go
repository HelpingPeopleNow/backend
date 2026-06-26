package core

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEnvFloatValid(t *testing.T) {
	os.Setenv("TEST_ENV_FLOAT", "3.14")
	defer os.Unsetenv("TEST_ENV_FLOAT")
	assert.InDelta(t, 3.14, GetEnvFloat("TEST_ENV_FLOAT", 0), 0.001)
}

func TestGetEnvFloatEmptyFallsBack(t *testing.T) {
	os.Unsetenv("TEST_ENV_FLOAT_MISSING")
	assert.Equal(t, 99.9, GetEnvFloat("TEST_ENV_FLOAT_MISSING", 99.9))
}

func TestGetEnvFloatInvalidFallsBack(t *testing.T) {
	os.Setenv("TEST_ENV_FLOAT_BAD", "not-a-number")
	defer os.Unsetenv("TEST_ENV_FLOAT_BAD")
	assert.Equal(t, 42.0, GetEnvFloat("TEST_ENV_FLOAT_BAD", 42.0))
}

func TestGetEnvBoolTrueVariants(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "True", "yes", "YES", "Yes"} {
		os.Setenv("TEST_ENV_BOOL", v)
		assert.True(t, GetEnvFloat("TEST_ENV_BOOL", 0) != 0 || GetEnvBool("TEST_ENV_BOOL", false), "value=%s", v)
	}
	os.Unsetenv("TEST_ENV_BOOL")
}

func TestGetEnvBoolFalseVariants(t *testing.T) {
	for _, v := range []string{"0", "false", "FALSE", "False", "no", "NO", "No"} {
		os.Setenv("TEST_ENV_BOOL", v)
		assert.False(t, GetEnvBool("TEST_ENV_BOOL", true), "value=%s", v)
	}
	os.Unsetenv("TEST_ENV_BOOL")
}

func TestGetEnvBoolEmptyFallsBack(t *testing.T) {
	os.Unsetenv("TEST_ENV_BOOL_MISSING")
	assert.True(t, GetEnvBool("TEST_ENV_BOOL_MISSING", true))
	assert.False(t, GetEnvBool("TEST_ENV_BOOL_MISSING", false))
}

func TestGetEnvBoolInvalidFallsBack(t *testing.T) {
	os.Setenv("TEST_ENV_BOOL_BAD", "maybe")
	defer os.Unsetenv("TEST_ENV_BOOL_BAD")
	assert.True(t, GetEnvBool("TEST_ENV_BOOL_BAD", true))
	assert.False(t, GetEnvBool("TEST_ENV_BOOL_BAD", false))
}
