// Package secrets manages the per-installation master key and derives purpose
// specific subkeys. The master key never leaves the data directory and is
// never written to the database or logs.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const keySize = 32

// Manager owns the master secret and provides derived keys.
type Manager struct {
	master []byte
	dir    string
}

// LoadOrCreate reads the master key, creating it atomically on first start.
func LoadOrCreate(dataDir string) (*Manager, error) {
	path := filepath.Join(dataDir, "secret.key")
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != keySize {
			return nil, fmt.Errorf("secret key %s has invalid length %d", path, len(key))
		}
		return &Manager{master: key, dir: dataDir}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read secret key: %w", err)
	}
	key = make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate secret key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		// A concurrent start may have won the race.
		if existing, rerr := os.ReadFile(path); rerr == nil && len(existing) == keySize {
			return &Manager{master: existing, dir: dataDir}, nil
		}
		return nil, fmt.Errorf("create secret key: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, fmt.Errorf("write secret key: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return nil, fmt.Errorf("sync secret key: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close secret key: %w", err)
	}
	return &Manager{master: key, dir: dataDir}, nil
}

// Derive returns a 32-byte subkey bound to the given purpose.
func (m *Manager) Derive(purpose string) []byte {
	k, err := hkdf.Key(sha256.New, m.master, nil, "texshare/"+purpose+"/v1", keySize)
	if err != nil {
		panic("hkdf: " + err.Error())
	}
	return k
}

// Seal encrypts plaintext with AES-256-GCM using a purpose-bound key.
func (m *Manager) Seal(purpose string, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(m.Derive(purpose))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, plaintext, []byte("texshare:"+purpose))
	return "v1:" + base64.RawStdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal.
func (m *Manager) Open(purpose, encoded string) ([]byte, error) {
	if !strings.HasPrefix(encoded, "v1:") {
		return nil, errors.New("unsupported secret format")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(m.Derive(purpose))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, []byte("texshare:"+purpose))
}

// BackupKey returns the automatic-backup key. It is read from the configured
// file or created in the data directory. The key is deliberately not stored in
// SQLite so a stolen database cannot decrypt automatic backups.
func (m *Manager) BackupKey(explicitPath string) ([]byte, error) {
	path := explicitPath
	if path == "" {
		path = filepath.Join(m.dir, "backup.key")
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != keySize {
			return nil, fmt.Errorf("backup key %s has invalid length", path)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if existing, rerr := os.ReadFile(path); rerr == nil && len(existing) == keySize {
			return existing, nil
		}
		return nil, err
	}
	if _, err := f.Write(key); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return key, nil
}

// HMACKey is a derived key for tamper-evident records and hashed tokens.
func (m *Manager) HMACKey(purpose string) []byte {
	return m.Derive("hmac/" + purpose)
}
