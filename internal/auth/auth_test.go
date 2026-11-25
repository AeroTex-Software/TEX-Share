package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("Sup3rSecret!Pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected hash format: %q", hash)
	}
	if !VerifyPassword("Sup3rSecret!Pass", hash) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password accepted")
	}
	// Malformed hashes must not panic or verify.
	for _, bad := range []string{"", "plaintext", "$argon2id$v=19$m=0,t=0,p=0$a$b", "$argon2id$v=19$m=32768,t=3,p=2$short$short"} {
		if VerifyPassword("Sup3rSecret!Pass", bad) {
			t.Errorf("malformed hash %q verified", bad)
		}
	}
}

func TestPasswordHashIsSalted(t *testing.T) {
	h1, _ := HashPassword("same-password-123")
	h2, _ := HashPassword("same-password-123")
	if h1 == h2 {
		t.Fatal("identical passwords produced identical hashes (no salt)")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	if err := ValidatePasswordPolicy("Sup3rSecret!Pass", 8); err != nil {
		t.Errorf("strong password rejected: %v", err)
	}
	cases := []struct {
		name string
		pw   string
		min  int
	}{
		{"too short", "Ab1!", 8},
		{"too few classes", "alllowercase", 8},
		{"only digits", "12345678", 8},
		{"too long", strings.Repeat("Aa1!", 100), 8},
	}
	for _, c := range cases {
		if err := ValidatePasswordPolicy(c.pw, c.min); err == nil {
			t.Errorf("%s: expected rejection", c.name)
		}
	}
	// Minimum is clamped to 8.
	if err := ValidatePasswordPolicy("Ab1!", 1); err == nil {
		t.Error("min length below 8 was not clamped")
	}
}

func TestTOTPValidateAndReplay(t *testing.T) {
	key, err := TOTPConfig("TEX Share", "admin")
	if err != nil {
		t.Fatalf("TOTPConfig: %v", err)
	}
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	step, ok := ValidateTOTP(key.Secret(), code, 0)
	if !ok {
		t.Fatal("valid TOTP code rejected")
	}
	// Replaying the same step must fail.
	if _, ok := ValidateTOTP(key.Secret(), code, step); ok {
		t.Fatal("replayed TOTP code accepted")
	}
	// Wrong codes are rejected.
	if _, ok := ValidateTOTP(key.Secret(), "000000", 0); ok {
		t.Fatal("wrong TOTP code accepted")
	}
	if _, ok := ValidateTOTP(key.Secret(), "12345", 0); ok {
		t.Fatal("short TOTP code accepted")
	}
}

func TestRecoveryCodes(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	codes, hashes, err := GenerateRecoveryCodes(key)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("count = %d/%d, want %d", len(codes), len(hashes), RecoveryCodeCount)
	}
	if !strings.Contains(codes[0], "-") {
		t.Errorf("recovery code not grouped: %q", codes[0])
	}
	// The stored hash must verify and normalisation must ignore case/dashes.
	if HashRecoveryCode(key, codes[0]) != hashes[0] {
		t.Fatal("recovery hash mismatch")
	}
	normalised := strings.ToLower(strings.ReplaceAll(codes[0], "-", ""))
	if HashRecoveryCode(key, normalised) != hashes[0] {
		t.Fatal("recovery hash is not normalisation-insensitive")
	}
}

func TestGenerateTempPassword(t *testing.T) {
	pw := GenerateTempPassword()
	if len(pw) < 12 {
		t.Fatalf("temp password too short: %d", len(pw))
	}
	if GenerateTempPassword() == pw {
		t.Fatal("temp password repeated")
	}
}

func TestParseUA(t *testing.T) {
	browser, osName := ParseUA("Mozilla/5.0 (Windows NT 10.0) Gecko/20100101 Firefox/120.0")
	if browser != "Firefox" || osName != "Windows" {
		t.Fatalf("ParseUA = %q/%q", browser, osName)
	}
	browser, osName = ParseUA("curl/8.4.0")
	if browser != "curl" {
		t.Fatalf("curl browser = %q", browser)
	}
	_ = osName
}
