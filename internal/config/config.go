package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	AppEnv             string
	Port               string
	AppBaseURL         string
	DBPath             string
	SuperAdminEmail    string
	SuperAdminPassword string
	SessionSecret      string
	SMTPHost           string
	SMTPPort           int
	SMTPUser           string
	SMTPPass           string
	SMTPFrom           string
	SMTPTLSMode        string
}

func Load() (*Config, error) {
	_ = loadDotEnv(".env")

	appEnv := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "development")))
	if appEnv != "development" && appEnv != "production" {
		return nil, fmt.Errorf("invalid APP_ENV %q: must be 'development' or 'production'", appEnv)
	}

	port := strings.TrimSpace(getEnv("PORT", "8080"))
	baseURL := strings.TrimRight(strings.TrimSpace(getEnv("APP_BASE_URL", "")), "/")
	if baseURL == "" || (appEnv == "development" && baseURL == "http://localhost:8080" && port != "8080") {
		if appEnv == "production" && baseURL == "" {
			return nil, errors.New("APP_BASE_URL is required in production")
		}
		baseURL = "http://localhost:" + port
	}
	parsedURL, err := url.Parse(baseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid APP_BASE_URL %q: must be an absolute http(s) URL", baseURL)
	}

	dbPath := strings.TrimSpace(getEnv("DB_PATH", "./data/secret-santa.db"))
	superEmail := strings.TrimSpace(getEnv("SUPER_ADMIN_EMAIL", "admin@example.com"))
	superPass := getEnv("SUPER_ADMIN_PASSWORD", "admin")

	sessionSecret := getEnv("SESSION_SECRET", "")
	if sessionSecret == "" {
		if appEnv == "production" {
			return nil, errors.New("SESSION_SECRET is required in production")
		}
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		sessionSecret = hex.EncodeToString(b)
	}

	smtpPort, err := strconv.Atoi(strings.TrimSpace(getEnv("SMTP_PORT", "587")))
	if err != nil || smtpPort <= 0 {
		return nil, fmt.Errorf("invalid SMTP_PORT: %w", err)
	}

	tlsMode := strings.ToLower(strings.TrimSpace(getEnv("SMTP_TLS_MODE", "starttls")))
	if tlsMode != "starttls" && tlsMode != "tls" && tlsMode != "none" {
		return nil, fmt.Errorf("invalid SMTP_TLS_MODE %q: must be 'starttls', 'tls', or 'none'", tlsMode)
	}

	cfg := &Config{
		AppEnv:             appEnv,
		Port:               port,
		AppBaseURL:         baseURL,
		DBPath:             dbPath,
		SuperAdminEmail:    superEmail,
		SuperAdminPassword: superPass,
		SessionSecret:      sessionSecret,
		SMTPHost:           strings.TrimSpace(getEnv("SMTP_HOST", "")),
		SMTPPort:           smtpPort,
		SMTPUser:           strings.TrimSpace(getEnv("SMTP_USER", "")),
		SMTPPass:           getEnv("SMTP_PASS", ""),
		SMTPFrom:           strings.TrimSpace(getEnv("SMTP_FROM", "Secret Santa <noreply@example.com>")),
		SMTPTLSMode:        tlsMode,
	}

	if appEnv == "production" {
		if len(cfg.SuperAdminPassword) < 12 || cfg.SuperAdminPassword == "admin" || cfg.SuperAdminPassword == "change-me-in-production" {
			return nil, errors.New("SUPER_ADMIN_PASSWORD must be changed and at least 12 characters in production")
		}
		if len(cfg.SessionSecret) < 32 || cfg.SessionSecret == "change-me-32-bytes-minimum-secret-key" {
			return nil, errors.New("SESSION_SECRET must be changed and at least 32 characters in production")
		}
		if cfg.SMTPHost == "" {
			return nil, errors.New("SMTP_HOST is required in production")
		}
	}

	return cfg, nil
}

func (c *Config) IsSecureCookie() bool {
	return c.AppEnv == "production" || strings.HasPrefix(strings.ToLower(c.AppBaseURL), "https://")
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			_ = os.Setenv(k, v)
		}
	}
	return scanner.Err()
}
