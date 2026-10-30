package helpers_test

import (
	"errors"
	"strings"
	"testing"

	"bonita-backend/helpers"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		wantError bool
		expected  error
	}{
		{
			name:      "ValidStandardPassword",
			password:  "BonitaUmroh2026!",
			wantError: false,
		},
		{
			name:      "EmptyPassword",
			password:  "",
			wantError: false,
		},
		{
			name:      "PasswordWithUnicodeAndSymbols",
			password:  "S@nd!_R4h4s14#🕌",
			wantError: false,
		},
		{
			name:      "MaxBoundary72BytesPassword",
			password:  strings.Repeat("a", 72),
			wantError: false,
		},
		{
			name:      "Exceeds72BytesPasswordReturnsBcryptError",
			password:  strings.Repeat("a", 73),
			wantError: true,
			expected:  bcrypt.ErrPasswordTooLong,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hash, err := helpers.HashPassword(tc.password)
			if tc.wantError {
				if err == nil {
					t.Fatalf("Expected error for password length %d, got nil", len(tc.password))
				}
				if tc.expected != nil && !errors.Is(err, tc.expected) {
					t.Errorf("Expected error %v, got %v", tc.expected, err)
				}
				if hash != "" {
					t.Errorf("Expected empty hash on error, got %q", hash)
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if hash == "" {
				t.Fatal("Expected non-empty hash")
			}
			if hash == tc.password {
				t.Error("Hash must not be equal to plain-text password")
			}
			if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
				t.Errorf("Expected valid bcrypt prefix ($2a$ or $2b$), got %q", hash)
			}
			if !helpers.CheckPasswordHash(tc.password, hash) {
				t.Error("Expected CheckPasswordHash to return true for matching password")
			}
		})
	}

	t.Run("GeneratesUniqueSaltPerCall", func(t *testing.T) {
		pw := "UniqueSaltCheck123"
		h1, err1 := helpers.HashPassword(pw)
		h2, err2 := helpers.HashPassword(pw)
		if err1 != nil || err2 != nil {
			t.Fatalf("Unexpected errors: %v, %v", err1, err2)
		}
		if h1 == h2 {
			t.Error("Expected two bcrypt hashes of the same password to differ due to random salt")
		}
	})
}

func TestCheckPasswordHash(t *testing.T) {
	validPassword := "RahasiaAdmin123!"
	validHash, err := helpers.HashPassword(validPassword)
	if err != nil {
		t.Fatalf("Failed to generate hash for setup: %v", err)
	}

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{
			name:     "MatchingPasswordAndHash",
			password: validPassword,
			hash:     validHash,
			want:     true,
		},
		{
			name:     "WrongPassword",
			password: "SalahPassword999",
			hash:     validHash,
			want:     false,
		},
		{
			name:     "CaseDifferenceFails",
			password: strings.ToLower(validPassword),
			hash:     validHash,
			want:     false,
		},
		{
			name:     "EmptyPasswordAgainstValidHash",
			password: "",
			hash:     validHash,
			want:     false,
		},
		{
			name:     "MalformedHashString",
			password: validPassword,
			hash:     "invalid-bcrypt-hash",
			want:     false,
		},
		{
			name:     "EmptyHashString",
			password: validPassword,
			hash:     "",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := helpers.CheckPasswordHash(tc.password, tc.hash)
			if got != tc.want {
				t.Errorf("CheckPasswordHash(%q, %q) = %v, want %v", tc.password, tc.hash, got, tc.want)
			}
		})
	}
}
