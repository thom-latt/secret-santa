package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// cleanEnv unsets every variable Load reads so tests are hermetic, then runs
// from an empty directory so a developer's .env is never picked up.
func cleanEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"APP_ENV", "PORT", "APP_BASE_URL", "DB_PATH", "SUPER_ADMIN_EMAIL", "SUPER_ADMIN_PASSWORD_HASH",
		"SESSION_SECRET", "SMTP_HOST", "SMTP_PORT", "SMTP_USER", "SMTP_PASS", "SMTP_FROM", "SMTP_TLS_MODE", "TRUST_PROXY",
		"OPERATOR_NAME", "OPERATOR_EMAIL",
	} {
		old, had := os.LookupEnv(k)
		_ = os.Unsetenv(k)
		if had {
			t.Cleanup(func() { _ = os.Setenv(k, old) })
		}
	}
	t.Chdir(t.TempDir())
}

var (
	strongHashOnce  sync.Once
	strongHashValue string
)

// strongHash is computed once: PBKDF2 at 210k iterations is slow under -race.
func strongHash(t *testing.T) string {
	t.Helper()
	strongHashOnce.Do(func() {
		h, err := HashPassword("a-very-strong-passphrase")
		if err != nil {
			t.Fatal(err)
		}
		strongHashValue = h
	})
	return strongHashValue
}

func prodEnv(t *testing.T) {
	t.Helper()
	cleanEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://santa.example.com/")
	t.Setenv("SUPER_ADMIN_PASSWORD_HASH", strongHash(t))
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SUPER_ADMIN_EMAIL", "owner@santa.example.org")
	t.Setenv("SMTP_FROM", "Secret Santa <santa@santa.example.org>")
	t.Setenv("OPERATOR_NAME", "Jane Doe")
	t.Setenv("OPERATOR_EMAIL", "jane@santa.example.org")
}

func TestLoadDevelopmentDefaults(t *testing.T) {
	cleanEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppEnv != "development" || cfg.Port != "8080" || cfg.AppBaseURL != "http://localhost:8080" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.SMTPPort != 587 || cfg.SMTPTLSMode != "starttls" || cfg.TrustProxy {
		t.Fatalf("unexpected SMTP/proxy defaults: %+v", cfg)
	}
	if cfg.SessionSecret == "" || !VerifyPassword("admin", cfg.SuperAdminPasswordHash) {
		t.Fatal("development must get a random session secret and the default admin password")
	}
	if cfg.IsSecureCookie() {
		t.Fatal("http development must not use Secure cookies")
	}
}

func TestLoadDevelopmentAllowsLoopbackHosts(t *testing.T) {
	for _, u := range []string{"http://localhost:3000", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		cleanEnv(t)
		t.Setenv("APP_BASE_URL", u)
		if _, err := Load(); err != nil {
			t.Fatalf("%s: %v", u, err)
		}
	}
}

func TestLoadDevelopmentCustomPortDerivesBaseURL(t *testing.T) {
	cleanEnv(t)
	t.Setenv("PORT", "9090")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppBaseURL != "http://localhost:9090" {
		t.Fatalf("got %q", cfg.AppBaseURL)
	}
}

func TestLoadProductionValid(t *testing.T) {
	prodEnv(t)
	t.Setenv("TRUST_PROXY", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AppBaseURL != "https://santa.example.com" {
		t.Fatalf("trailing slash must be trimmed, got %q", cfg.AppBaseURL)
	}
	if !cfg.IsSecureCookie() || !cfg.TrustProxy {
		t.Fatalf("expected secure cookies and TrustProxy, got %+v", cfg)
	}
	if cfg.OperatorName != "Jane Doe" || cfg.OperatorEmail != "jane@santa.example.org" {
		t.Fatalf("operator identity not loaded: %q %q", cfg.OperatorName, cfg.OperatorEmail)
	}
}

func TestLoadRejectsInvalidConfigurations(t *testing.T) {
	cases := []struct {
		name    string
		prod    bool
		env     map[string]string
		wantErr string
	}{
		{"bad APP_ENV", false, map[string]string{"APP_ENV": "staging"}, "invalid APP_ENV"},
		{"prod without base url", true, map[string]string{"APP_BASE_URL": ""}, "APP_BASE_URL is required"},
		{"relative base url", true, map[string]string{"APP_BASE_URL": "santa.example.com"}, "invalid APP_BASE_URL"},
		{"ftp base url", true, map[string]string{"APP_BASE_URL": "ftp://santa.example.com"}, "invalid APP_BASE_URL"},
		{"prod without session secret", true, map[string]string{"SESSION_SECRET": ""}, "SESSION_SECRET is required"},
		{"prod short session secret", true, map[string]string{"SESSION_SECRET": "short"}, "at least 32"},
		{"prod placeholder session secret", true, map[string]string{"SESSION_SECRET": "change-me-32-bytes-minimum-secret-key"}, "SESSION_SECRET"},
		{"prod without smtp host", true, map[string]string{"SMTP_HOST": ""}, "SMTP_HOST is required"},
		{"prod default admin password", true, map[string]string{"SUPER_ADMIN_PASSWORD_HASH": ""}, "SUPER_ADMIN_PASSWORD_HASH"},
		{"prod placeholder hash", true, map[string]string{"SUPER_ADMIN_PASSWORD_HASH": placeholderPasswordHash}, "SUPER_ADMIN_PASSWORD_HASH"},
		{"malformed hash", true, map[string]string{"SUPER_ADMIN_PASSWORD_HASH": "plaintext"}, "valid pbkdf2_sha256"},
		{"smtp port not a number", false, map[string]string{"SMTP_PORT": "abc"}, "invalid SMTP_PORT"},
		{"smtp port zero", false, map[string]string{"SMTP_PORT": "0"}, "invalid SMTP_PORT"},
		{"smtp port negative", false, map[string]string{"SMTP_PORT": "-5"}, "invalid SMTP_PORT"},
		{"bad tls mode", false, map[string]string{"SMTP_TLS_MODE": "ssl"}, "invalid SMTP_TLS_MODE"},
		{"dev on public host", false, map[string]string{"APP_BASE_URL": "http://santa.example.com"}, "development mode"},
		{"prod http base url", true, map[string]string{"APP_BASE_URL": "http://santa.example.com"}, "must use https"},
		{"prod default admin email", true, map[string]string{"SUPER_ADMIN_EMAIL": "admin@example.com"}, "SUPER_ADMIN_EMAIL"},
		{"prod default smtp from", true, map[string]string{"SMTP_FROM": "Secret Santa <noreply@example.com>"}, "SMTP_FROM"},
		{"prod unparsable smtp from", true, map[string]string{"SMTP_FROM": "not an address"}, "SMTP_FROM"},
		{"prod without operator name", true, map[string]string{"OPERATOR_NAME": " "}, "OPERATOR_NAME"},
		{"prod without operator email", true, map[string]string{"OPERATOR_EMAIL": ""}, "OPERATOR_EMAIL"},
		{"prod invalid operator email", true, map[string]string{"OPERATOR_EMAIL": "not an address"}, "OPERATOR_EMAIL"},
		{"bad TRUST_PROXY", false, map[string]string{"TRUST_PROXY": "maybe"}, "invalid TRUST_PROXY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prod {
				prodEnv(t)
			} else {
				cleanEnv(t)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLoadProductionRejectsWeakKnownPasswords(t *testing.T) {
	for _, weak := range []string{"admin", "change-me-in-production"} {
		prodEnv(t)
		h, err := HashPassword(weak)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("SUPER_ADMIN_PASSWORD_HASH", h)
		if _, err := Load(); err == nil {
			t.Fatalf("production must refuse the hash of %q", weak)
		}
	}
}

func TestParseBoolEnv(t *testing.T) {
	for in, want := range map[string]bool{"1": true, "TRUE": true, " yes ": true, "on": true, "0": false, "False": false, "no": false, "off": false, "": false} {
		t.Setenv("X_BOOL", in)
		got, err := parseBoolEnv("X_BOOL", false)
		if err != nil || got != want {
			t.Fatalf("parseBoolEnv(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
}

func TestLoadReadsDotEnvWithoutOverridingRealEnvironment(t *testing.T) {
	cleanEnv(t)
	content := "# comment\n\nPORT=\"7070\"\nSMTP_FROM='Santa <s@example.com>'\nnot a pair\nSUPER_ADMIN_EMAIL = boss@example.com\n"
	if err := os.WriteFile(".env", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SUPER_ADMIN_EMAIL", "real@example.com") // real env wins over .env
	t.Cleanup(func() { _ = os.Unsetenv("PORT"); _ = os.Unsetenv("SMTP_FROM") })

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "7070" || cfg.SMTPFrom != "Santa <s@example.com>" {
		t.Fatalf("expected values from .env, got port=%q from=%q", cfg.Port, cfg.SMTPFrom)
	}
	if cfg.SuperAdminEmail != "real@example.com" {
		t.Fatalf("real environment must win over .env, got %q", cfg.SuperAdminEmail)
	}
}

func TestPasswordHashing(t *testing.T) {
	h1, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := HashPassword("correct horse battery")
	if h1 == h2 {
		t.Fatal("hashes must be salted")
	}
	if !IsValidPasswordHash(h1) || !VerifyPassword("correct horse battery", h1) {
		t.Fatal("valid hash must verify")
	}
	if VerifyPassword("wrong", h1) || VerifyPassword("", h1) {
		t.Fatal("wrong password must not verify")
	}
	for _, bad := range []string{
		"", "plain", "pbkdf2_sha256:210000:00:00", "md5:210000:" + strings.Repeat("00", 16) + ":" + strings.Repeat("00", 32),
		"pbkdf2_sha256:5:" + strings.Repeat("00", 16) + ":" + strings.Repeat("00", 32),        // too few iterations
		"pbkdf2_sha256:99999999:" + strings.Repeat("00", 16) + ":" + strings.Repeat("00", 32), // DoS-sized iterations
		"pbkdf2_sha256:210000:zz:" + strings.Repeat("00", 32),
		"pbkdf2_sha256:210000:" + strings.Repeat("00", 8) + ":" + strings.Repeat("00", 32),  // short salt
		"pbkdf2_sha256:210000:" + strings.Repeat("00", 16) + ":" + strings.Repeat("00", 16), // short key
	} {
		if IsValidPasswordHash(bad) || VerifyPassword("x", bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestSetEnvPasswordHash(t *testing.T) {
	hash := strongHash(t)
	path := filepath.Join(t.TempDir(), ".env")

	if err := SetEnvPasswordHash(path, "bogus"); err == nil {
		t.Fatal("invalid hash must be refused")
	}

	// replaces any existing entries (including the legacy plaintext key) exactly once
	orig := "PORT=1\nSUPER_ADMIN_PASSWORD=oldplain\n# SUPER_ADMIN_PASSWORD_HASH=commented\nSUPER_ADMIN_PASSWORD_HASH=old\nSMTP_HOST=x\n"
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SetEnvPasswordHash(path, hash); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	out := string(raw)
	if strings.Count(out, "SUPER_ADMIN_PASSWORD_HASH="+hash) != 1 || strings.Contains(out, "oldplain") || strings.Contains(out, "=old\n") {
		t.Fatalf("unexpected .env:\n%s", out)
	}
	if !strings.Contains(out, "PORT=1") || !strings.Contains(out, "SMTP_HOST=x") || !strings.Contains(out, "# SUPER_ADMIN_PASSWORD_HASH=commented") {
		t.Fatalf("unrelated lines must be preserved:\n%s", out)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %v", st.Mode().Perm())
	}

	// appends when absent; creates the file when missing
	path2 := filepath.Join(t.TempDir(), ".env")
	t.Chdir(t.TempDir()) // no .env.example to copy
	if err := SetEnvPasswordHash(path2, hash); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path2)
	if !strings.Contains(string(raw), "SUPER_ADMIN_PASSWORD_HASH="+hash) {
		t.Fatalf("hash not written: %s", raw)
	}
}
