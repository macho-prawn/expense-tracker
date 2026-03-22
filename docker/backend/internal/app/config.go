package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port              string
	DBHost            string
	DBPort            string
	DBName            string
	DBUser            string
	DBPassword        string
	SessionCookieName string
	SessionTTL        time.Duration
	SessionSecure     bool
	AppOrigin         string
	FXAPIBaseURL      string
}

func LoadConfig() (Config, error) {
	dbName, err := getEnvOrFile("DB_NAME", "DB_NAME_FILE", "sharetab")
	if err != nil {
		return Config{}, err
	}

	dbUser, err := getEnvOrFile("DB_USER", "DB_USER_FILE", "sharetab")
	if err != nil {
		return Config{}, err
	}

	dbPassword, err := getEnvOrFile("DB_PASSWORD", "DB_PASSWORD_FILE", "sharetab")
	if err != nil {
		return Config{}, err
	}

	ttlHours, err := strconv.Atoi(getEnv("SESSION_TTL_HOURS", "720"))
	if err != nil || ttlHours <= 0 {
		return Config{}, fmt.Errorf("SESSION_TTL_HOURS must be a positive integer")
	}

	appOrigin := getEnv("APP_ORIGIN", "http://localhost:8082")

	cfg := Config{
		Port:              getEnv("PORT", "3001"),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBName:            dbName,
		DBUser:            dbUser,
		DBPassword:        dbPassword,
		SessionCookieName: getEnv("SESSION_COOKIE_NAME", "sharetab_session"),
		SessionTTL:        time.Duration(ttlHours) * time.Hour,
		SessionSecure:     isSecureOrigin(appOrigin),
		AppOrigin:         appOrigin,
		FXAPIBaseURL:      getEnv("FX_API_BASE_URL", "https://api.frankfurter.app"),
	}

	if cfg.DBHost == "" || cfg.DBPort == "" || cfg.DBName == "" || cfg.DBUser == "" {
		return Config{}, fmt.Errorf("DB_HOST, DB_PORT, DB_NAME, and DB_USER are required")
	}

	return cfg, nil
}

func (c Config) DatabaseDSN() string {
	return (&url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   c.DBHost + ":" + c.DBPort,
		Path:   c.DBName,
		RawQuery: url.Values{
			"sslmode": []string{"disable"},
		}.Encode(),
	}).String()
}

func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getEnvOrFile(key, fileKey, fallback string) (string, error) {
	if path := os.Getenv(fileKey); path != "" {
		value, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", fileKey, err)
		}
		return strings.TrimSpace(string(value)), nil
	}

	return getEnv(key, fallback), nil
}

func isSecureOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "https")
}
