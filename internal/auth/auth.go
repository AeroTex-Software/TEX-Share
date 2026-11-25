// Package auth implements password hashing, TOTP and user-agent parsing.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. 32 MiB keeps login verification affordable while
// remaining well above the OWASP minimum.
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 32 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	saltLen             = 16
)

var ErrInvalidHash = errors.New("invalid password hash")

// HashPassword returns a PHC-formatted Argon2id hash.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a PHC hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	if m < 1024 || m > 1024*1024 || t < 1 || t > 20 || p < 1 || p > 32 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) < 16 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// TOTPConfig is the standard configuration accepted by common authenticator
// apps (SHA-1, six digits, 30 second period).
func TOTPConfig(issuer, account string) (*otp.Key, error) {
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
}

// ValidateTOTP verifies a code and returns the time step. lastStep is used to
// reject replay of an already consumed code.
func ValidateTOTP(secret, code string, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return 0, false
	}
	step := time.Now().Unix() / 30
	for _, candidate := range []int64{step, step - 1, step + 1} {
		if candidate <= lastStep {
			continue
		}
		ok, err := totp.ValidateCustom(code, secret, time.Unix(candidate*30, 0), totp.ValidateOpts{
			Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1,
		})
		if err == nil && ok {
			return candidate, true
		}
	}
	return 0, false
}

// RecoveryCodeCount is the number of one-time recovery codes generated.
const RecoveryCodeCount = 10

const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// GenerateRecoveryCodes creates human-friendly one-time codes and their hashes.
func GenerateRecoveryCodes(hmacKey []byte) ([]string, []string, error) {
	codes := make([]string, 0, RecoveryCodeCount)
	hashes := make([]string, 0, RecoveryCodeCount)
	for i := 0; i < RecoveryCodeCount; i++ {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, nil, err
		}
		var b strings.Builder
		for j, v := range raw {
			if j == 5 {
				b.WriteByte('-')
			}
			b.WriteByte(recoveryAlphabet[int(v)%len(recoveryAlphabet)])
		}
		code := b.String()
		codes = append(codes, code)
		hashes = append(hashes, HashRecoveryCode(hmacKey, code))
	}
	return codes, hashes, nil
}

// HashRecoveryCode returns a keyed hash for storage.
func HashRecoveryCode(key []byte, code string) string {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("recovery:" + normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateTempPassword creates a strong temporary password.
func GenerateTempPassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%"
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		panic("crypto/rand unavailable")
	}
	var b strings.Builder
	for _, v := range raw {
		b.WriteByte(alphabet[int(v)%len(alphabet)])
	}
	return b.String()
}

// ParseUAParses a user agent into a short browser and OS label without any
// external dependency.
func ParseUA(ua string) (browser, osName string) {
	lower := strings.ToLower(ua)
	switch {
	case strings.Contains(lower, "edg/"):
		browser = "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox/"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome/") || strings.Contains(lower, "crios/"):
		browser = "Chrome"
	case strings.Contains(lower, "safari/"):
		browser = "Safari"
	case strings.Contains(lower, "curl/"):
		browser = "curl"
	default:
		browser = "Unknown"
	}
	switch {
	case strings.Contains(lower, "windows"):
		osName = "Windows"
	case strings.Contains(lower, "android"):
		osName = "Android"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ios"):
		osName = "iOS"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macintosh"):
		osName = "macOS"
	case strings.Contains(lower, "linux"):
		osName = "Linux"
	default:
		osName = "Unknown"
	}
	return browser, osName
}

// ValidatePasswordPolicy enforces length and basic composition. The minimum is
// configurable by administrators.
func ValidatePasswordPolicy(password string, minLen int) error {
	if minLen < 8 {
		minLen = 8
	}
	if len(password) < minLen {
		return fmt.Errorf("password must be at least %d characters", minLen)
	}
	if len(password) > 256 {
		return errors.New("password too long")
	}
	var classes int
	if strings.IndexFunc(password, func(r rune) bool { return r >= 'a' && r <= 'z' }) >= 0 {
		classes++
	}
	if strings.IndexFunc(password, func(r rune) bool { return r >= 'A' && r <= 'Z' }) >= 0 {
		classes++
	}
	if strings.IndexFunc(password, func(r rune) bool { return r >= '0' && r <= '9' }) >= 0 {
		classes++
	}
	if strings.IndexFunc(password, func(r rune) bool { return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) }) >= 0 {
		classes++
	}
	if classes < 3 {
		return errors.New("password must use at least three character classes")
	}
	return nil
}

// ParseInt64 is a small helper used by callers parsing form values.
func ParseInt64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// Base32Secret encodes a raw TOTP secret the way authenticator apps expect.
func Base32Secret(secret string) string {
	return strings.ToUpper(strings.TrimSpace(secret))
}

// DecodeBase32Secret is used by tests.
func DecodeBase32Secret(secret string) ([]byte, error) {
	return base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
}
