package secrets

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePersistsMasterKey(t *testing.T) {
	dir := t.TempDir()
	m1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("first LoadOrCreate: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("secret.key not created: %v", err)
	}
	if info.Size() != keySize {
		t.Fatalf("secret.key size = %d, want %d", info.Size(), keySize)
	}
	// A second load must return the same master key.
	m2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("second LoadOrCreate: %v", err)
	}
	if !bytes.Equal(m1.Derive("purpose"), m2.Derive("purpose")) {
		t.Fatal("master key changed between loads")
	}
}

func TestDeriveIsPurposeBound(t *testing.T) {
	m, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := m.Derive("alpha")
	b := m.Derive("beta")
	if bytes.Equal(a, b) {
		t.Fatal("different purposes produced the same subkey")
	}
	if len(a) != keySize {
		t.Fatalf("derived key length = %d, want %d", len(a), keySize)
	}
	if !bytes.Equal(a, m.Derive("alpha")) {
		t.Fatal("Derive is not deterministic")
	}
	if !bytes.Equal(m.HMACKey("x"), m.Derive("hmac/x")) {
		t.Fatal("HMACKey does not match Derive")
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	m, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plaintext := []byte("cloudflare-api-token")
	sealed, err := m.Seal("cf_token", plaintext)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(sealed) < 3 || sealed[:3] != "v1:" {
		t.Fatalf("unexpected sealed format: %q", sealed)
	}
	opened, err := m.Open("cf_token", sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatalf("round trip mismatch: %q", opened)
	}
	// Wrong purpose must fail authentication.
	if _, err := m.Open("other", sealed); err == nil {
		t.Fatal("Open with wrong purpose succeeded")
	}
	// Sealing twice yields different ciphertext (random nonce).
	sealed2, _ := m.Seal("cf_token", plaintext)
	if sealed == sealed2 {
		t.Fatal("Seal reused a nonce")
	}
}

func TestBackupKeyCreatesAndReuses(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	k1, err := m.BackupKey("")
	if err != nil {
		t.Fatalf("BackupKey: %v", err)
	}
	if len(k1) != keySize {
		t.Fatalf("backup key length = %d", len(k1))
	}
	if _, err := os.Stat(filepath.Join(dir, "backup.key")); err != nil {
		t.Fatalf("backup.key not created: %v", err)
	}
	k2, err := m.BackupKey("")
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("backup key not stable: %v", err)
	}
	// An explicit path is honoured.
	explicit := filepath.Join(t.TempDir(), "nested", "bk.key")
	k3, err := m.BackupKey(explicit)
	if err != nil {
		t.Fatalf("BackupKey explicit: %v", err)
	}
	if len(k3) != keySize {
		t.Fatalf("explicit backup key length = %d", len(k3))
	}
}
