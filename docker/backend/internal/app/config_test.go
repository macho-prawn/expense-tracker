package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigReadsSecretFiles(t *testing.T) {
	t.Setenv("DB_HOST", "expense-tracker-db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("SESSION_TTL_HOURS", "1")
	t.Setenv("DB_NAME_FILE", writeSecretFile(t, "db_name", "sharetab\n"))
	t.Setenv("DB_USER_FILE", writeSecretFile(t, "db_user", "sharetab-user\n"))
	t.Setenv("DB_PASSWORD_FILE", writeSecretFile(t, "db_password", "sharetab-password\n"))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.DBName != "sharetab" {
		t.Fatalf("DBName = %q, want %q", cfg.DBName, "sharetab")
	}
	if cfg.DBUser != "sharetab-user" {
		t.Fatalf("DBUser = %q, want %q", cfg.DBUser, "sharetab-user")
	}
	if cfg.DBPassword != "sharetab-password" {
		t.Fatalf("DBPassword = %q, want %q", cfg.DBPassword, "sharetab-password")
	}
}

func TestLoadConfigPrefersSecretFilesOverEnv(t *testing.T) {
	t.Setenv("DB_HOST", "expense-tracker-db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("SESSION_TTL_HOURS", "1")
	t.Setenv("DB_NAME", "env-name")
	t.Setenv("DB_USER", "env-user")
	t.Setenv("DB_PASSWORD", "env-password")
	t.Setenv("DB_NAME_FILE", writeSecretFile(t, "db_name", "file-name"))
	t.Setenv("DB_USER_FILE", writeSecretFile(t, "db_user", "file-user"))
	t.Setenv("DB_PASSWORD_FILE", writeSecretFile(t, "db_password", "file-password"))

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.DBName != "file-name" {
		t.Fatalf("DBName = %q, want %q", cfg.DBName, "file-name")
	}
	if cfg.DBUser != "file-user" {
		t.Fatalf("DBUser = %q, want %q", cfg.DBUser, "file-user")
	}
	if cfg.DBPassword != "file-password" {
		t.Fatalf("DBPassword = %q, want %q", cfg.DBPassword, "file-password")
	}
}

func TestLoadConfigReturnsErrorForMissingSecretFile(t *testing.T) {
	t.Setenv("DB_HOST", "expense-tracker-db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("SESSION_TTL_HOURS", "1")
	t.Setenv("DB_NAME_FILE", filepath.Join(t.TempDir(), "missing"))

	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig() error = nil, want non-nil")
	}
}

func TestLoadConfigDerivesSessionSecureFromAppOrigin(t *testing.T) {
	t.Setenv("DB_HOST", "expense-tracker-db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("SESSION_TTL_HOURS", "1")
	t.Setenv("APP_ORIGIN", "https://sharetab.example")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if !cfg.SessionSecure {
		t.Fatal("SessionSecure = false, want true")
	}
}

func TestLoadConfigUsesInsecureCookiesForHttpOrigin(t *testing.T) {
	t.Setenv("DB_HOST", "expense-tracker-db")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("SESSION_TTL_HOURS", "1")
	t.Setenv("APP_ORIGIN", "http://localhost:8082")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if cfg.SessionSecure {
		t.Fatal("SessionSecure = true, want false")
	}
}

func writeSecretFile(t *testing.T, name, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}

	return path
}
