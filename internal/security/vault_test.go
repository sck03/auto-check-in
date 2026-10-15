package security

import (
	"auto-check-in/internal/domain"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestVaultAuthenticationAndAccountBinding(t *testing.T) {
	v, _ := NewVault(bytes.Repeat([]byte{7}, 32))
	plain := []byte("private session cookie")
	a, err := v.Seal(plain, "account:a")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := v.Seal(plain, "account:a")
	if bytes.Equal(a, b) || bytes.Contains(a, plain) {
		t.Fatal("nonce reuse or plaintext")
	}
	got, err := v.Open(a, "account:a")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal(err)
	}
	if _, err = v.Open(a, "account:b"); err == nil {
		t.Fatal("account substitution accepted")
	}
	a[len(a)-1] ^= 1
	if _, err = v.Open(a, "account:a"); err == nil {
		t.Fatal("tamper accepted")
	}
}
func TestEncryptedBackupWrongPassword(t *testing.T) {
	plain := []byte(`{"cookie":"very-secret-value"}`)
	b, err := EncryptBackup(plain, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("very-secret-value")) {
		t.Fatal("plaintext backup")
	}
	got, err := DecryptBackup(b, "correct horse battery")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal(err)
	}
	if _, err = DecryptBackup(b, "wrong password"); err == nil {
		t.Fatal("wrong password accepted")
	}
}
func TestMissingKeyDoesNotReplaceDatabaseKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "checkin.db"), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadKey(dir, ""); err == nil {
		t.Fatal("silently replaced lost key")
	}
}
func TestRedaction(t *testing.T) {
	c := domain.Credentials{Password: "secret-password", Token: "bearer-secret", CookieHeader: "session=cookie-secret"}
	got := Redact("secret-password bearer-secret cookie-secret code=oauth-secret Bearer abc123", c)
	for _, value := range []string{"secret-password", "bearer-secret", "cookie-secret", "oauth-secret", "abc123"} {
		if bytes.Contains([]byte(got), []byte(value)) {
			t.Fatal(got)
		}
	}
}
func TestDirectoryLock(t *testing.T) {
	dir := t.TempDir()
	release, err := LockDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if other, err := LockDirectory(dir); err == nil {
		other()
		t.Fatal("second process lock accepted")
	}
}
