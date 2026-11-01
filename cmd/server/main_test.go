package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"secret-santa/internal/config"
)

func TestRunSetPasswordWritesVerifiableHash(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("PORT=8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runSetPassword([]string{"-env", env, "a-long-enough-password"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(env)
	var hash string
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "SUPER_ADMIN_PASSWORD_HASH="); ok {
			hash = v
		}
	}
	if hash == "" || !config.VerifyPassword("a-long-enough-password", hash) {
		t.Fatalf("hash missing or not matching the password:\n%s", raw)
	}
	if strings.Contains(string(raw), "a-long-enough-password") {
		t.Fatal("the plaintext password must never be written")
	}
}

func TestRunSetPasswordRejectsShortPasswordAndLeavesFileUntouched(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(env, []byte("PORT=8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runSetPassword([]string{"-env", env, "short"})
	if err == nil || !strings.Contains(err.Error(), "at least 12") {
		t.Fatalf("expected a length error, got %v", err)
	}
	if raw, _ := os.ReadFile(env); string(raw) != "PORT=8080\n" {
		t.Fatalf(".env must be untouched, got %q", raw)
	}
}

func TestRunSetPasswordReadsFromStdin(t *testing.T) {
	env := filepath.Join(t.TempDir(), ".env")
	r, w, _ := os.Pipe()
	_, _ = w.WriteString("password-from-stdin-123\n")
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = orig }()

	t.Chdir(t.TempDir())
	if err := runSetPassword([]string{"-env", env}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(env)
	if !strings.Contains(string(raw), "SUPER_ADMIN_PASSWORD_HASH=pbkdf2_sha256:") {
		t.Fatalf("hash not written:\n%s", raw)
	}
}

func TestRunSetPasswordRejectsUnknownFlag(t *testing.T) {
	if err := runSetPassword([]string{"-nope"}); err == nil {
		t.Fatal("expected a flag parsing error")
	}
}

func TestRunBackupWritesCopy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DB_PATH", filepath.Join(dir, "live.db"))
	dst := filepath.Join(dir, "out", "b.db")
	if err := runBackup([]string{dst}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
	if err := runBackup(nil); err == nil {
		t.Fatal("missing destination must fail")
	}
}
