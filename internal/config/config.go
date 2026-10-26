package config

import (
	"bufio"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	pbkdf2Prefix            = "pbkdf2_sha256"
	pbkdf2DefaultIterations = 210000
	placeholderPasswordHash = "pbkdf2_sha256:210000:00000000000000000000000000000000:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

type Config struct {
	AppEnv                 string
	Port                   string
	AppBaseURL             string
	DBPath                 string
	SuperAdminEmail        string
	SuperAdminPasswordHash string
	SessionSecret          string
	SMTPHost               string
	SMTPPort               int
	SMTPUser               string
	SMTPPass               string
	SMTPFrom               string
	SMTPTLSMode            string
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
	superPassHash := strings.TrimSpace(getEnv("SUPER_ADMIN_PASSWORD_HASH", ""))
	if (superPassHash == "" || superPassHash == placeholderPasswordHash) && appEnv == "development" {
		superPassHash, _ = HashPassword("admin")
	}

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

	if !IsValidPasswordHash(superPassHash) {
		return nil, errors.New("SUPER_ADMIN_PASSWORD_HASH must be a valid pbkdf2_sha256 hash (run './secret-santa set-password' or 'make set-password')")
	}

	cfg := &Config{
		AppEnv:                 appEnv,
		Port:                   port,
		AppBaseURL:             baseURL,
		DBPath:                 dbPath,
		SuperAdminEmail:        superEmail,
		SuperAdminPasswordHash: superPassHash,
		SessionSecret:          sessionSecret,
		SMTPHost:               strings.TrimSpace(getEnv("SMTP_HOST", "")),
		SMTPPort:               smtpPort,
		SMTPUser:               strings.TrimSpace(getEnv("SMTP_USER", "")),
		SMTPPass:               getEnv("SMTP_PASS", ""),
		SMTPFrom:               strings.TrimSpace(getEnv("SMTP_FROM", "Secret Santa <noreply@example.com>")),
		SMTPTLSMode:            tlsMode,
	}

	if appEnv == "production" {
		if cfg.SuperAdminPasswordHash == placeholderPasswordHash || VerifyPassword("admin", cfg.SuperAdminPasswordHash) || VerifyPassword("change-me-in-production", cfg.SuperAdminPasswordHash) {
			return nil, errors.New("SUPER_ADMIN_PASSWORD_HASH must be set to a strong password hash in production (run './secret-santa set-password' or 'make set-password')")
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

func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2DefaultIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d:%x:%x", pbkdf2Prefix, pbkdf2DefaultIterations, salt, dk), nil
}

func parsePasswordHash(encoded string) (int, []byte, []byte, bool) {
	parts := strings.Split(strings.TrimSpace(encoded), ":")
	if len(parts) != 4 || parts[0] != pbkdf2Prefix {
		return 0, nil, nil, false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 10000 || iter > 5000000 {
		return 0, nil, nil, false
	}
	salt, err1 := hex.DecodeString(parts[2])
	dk, err2 := hex.DecodeString(parts[3])
	if err1 != nil || err2 != nil || len(salt) < 16 || len(dk) != 32 {
		return 0, nil, nil, false
	}
	return iter, salt, dk, true
}

func IsValidPasswordHash(encoded string) bool {
	_, _, _, ok := parsePasswordHash(encoded)
	return ok
}

func VerifyPassword(password, encodedHash string) bool {
	iter, salt, expectedDK, ok := parsePasswordHash(encodedHash)
	if !ok {
		return false
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, iter, len(expectedDK))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, expectedDK) == 1
}

func SetEnvPasswordHash(path, hash string) error {
	if !IsValidPasswordHash(hash) {
		return errors.New("invalid password hash")
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw, _ = os.ReadFile(".env.example")
	} else if err != nil {
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	replaced := false
	out := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if k, _, ok := strings.Cut(trimmed, "="); ok {
				key := strings.TrimSpace(k)
				if key == "SUPER_ADMIN_PASSWORD_HASH" || key == "SUPER_ADMIN_PASSWORD" {
					if !replaced {
						out = append(out, "SUPER_ADMIN_PASSWORD_HASH="+hash)
						replaced = true
					}
					continue
				}
			}
		}
		out = append(out, line)
	}
	if !replaced {
		if len(out) > 0 && out[len(out)-1] == "" {
			out[len(out)-1] = "SUPER_ADMIN_PASSWORD_HASH=" + hash
			out = append(out, "")
		} else {
			out = append(out, "SUPER_ADMIN_PASSWORD_HASH="+hash, "")
		}
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o600)
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
