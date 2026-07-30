package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadSecretEnv covers the docker-secret file fallback in
// readSecretEnv. Behaviour: TELEGRAM_BOT_TOKEN_FILE takes precedence
// over TELEGRAM_BOT_TOKEN. This is how the production / dev compose
// files hand the Telegram bot credential to the backend without ever
// putting the value into the .env file or rendered compose.
func TestReadSecretEnv(t *testing.T) {
	dir := t.TempDir()

	// Both unset → empty string.
	t.Run("both unset returns empty", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "")
		assert.Equal(t, "", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})

	// Only direct env → direct env value.
	t.Run("only direct env returns direct", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "direct-token")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "")
		assert.Equal(t, "direct-token", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})

	// File wins over direct env (the security model).
	t.Run("file takes precedence over direct env", func(t *testing.T) {
		path := filepath.Join(dir, "telegram_bot_token")
		require.NoError(t, os.WriteFile(path, []byte("file-token\n"), 0o600))
		t.Setenv("TELEGRAM_BOT_TOKEN", "direct-token")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", path)
		assert.Equal(t, "file-token", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})

	// Whitespace-only file path falls back to direct env.
	t.Run("blank file env falls back to direct", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "direct-token")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", "   ")
		assert.Equal(t, "direct-token", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})

	// Unreadable file path falls back to direct env (defence-in-depth:
	// a missing or mis-mounted secret should not crash the process).
	t.Run("unreadable file falls back to direct", func(t *testing.T) {
		t.Setenv("TELEGRAM_BOT_TOKEN", "direct-token")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", filepath.Join(dir, "does-not-exist"))
		assert.Equal(t, "direct-token", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})

	// File contents are trimmed (mounting tools frequently add a trailing
	// newline; that would break the Telegram bot URL).
	t.Run("file contents are trimmed", func(t *testing.T) {
		path := filepath.Join(dir, "trim")
		require.NoError(t, os.WriteFile(path, []byte("  token-with-padding \n\n"), 0o600))
		t.Setenv("TELEGRAM_BOT_TOKEN", "direct")
		t.Setenv("TELEGRAM_BOT_TOKEN_FILE", path)
		assert.Equal(t, "token-with-padding", readSecretEnv("TELEGRAM_BOT_TOKEN", "TELEGRAM_BOT_TOKEN_FILE"))
	})
}
