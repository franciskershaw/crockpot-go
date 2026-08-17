package auth

import (
	"testing"
)

func TestGenerateConfirmationCode_IsSixDigits(t *testing.T) {
	code, err := GenerateConfirmationCode()
	if err != nil {
		t.Fatalf("GenerateConfirmationCode failed: %v", err)
	}

	if len(code) != 6 {
		t.Fatalf("expected 6-character code, got %q (len %d)", code, len(code))
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("expected all-digit code, got %q", code)
		}
	}
}

func TestGenerateConfirmationCode_IsRandom(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		code, err := GenerateConfirmationCode()
		if err != nil {
			t.Fatalf("GenerateConfirmationCode failed: %v", err)
		}
		seen[code] = true
	}

	if len(seen) < 2 {
		t.Fatalf("expected varied codes across 20 generations, got %d distinct value(s)", len(seen))
	}
}

func TestGenerateResetToken_Is64CharHex(t *testing.T) {
	token, err := GenerateResetToken()
	if err != nil {
		t.Fatalf("GenerateResetToken failed: %v", err)
	}

	if len(token) != 64 {
		t.Fatalf("expected 64-char hex-encoded token (32 bytes), got %q (len %d)", token, len(token))
	}
	for _, r := range token {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("expected lowercase hex output, got %q", token)
		}
	}
}

func TestGenerateResetToken_IsRandom(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		token, err := GenerateResetToken()
		if err != nil {
			t.Fatalf("GenerateResetToken failed: %v", err)
		}
		seen[token] = true
	}

	if len(seen) < 20 {
		t.Fatalf("expected 20 distinct tokens across 20 generations, got %d distinct value(s)", len(seen))
	}
}

func TestHashToken_IsDeterministic(t *testing.T) {
	first := HashToken("abc123")
	second := HashToken("abc123")
	if first != second {
		t.Error("expected HashToken to return the same hash for the same input")
	}
}

func TestHashToken_DiffersForDifferentInput(t *testing.T) {
	if HashToken("abc123") == HashToken("xyz789") {
		t.Error("expected HashToken to return different hashes for different input")
	}
}

func TestHashToken_IsHexSHA256(t *testing.T) {
	got := HashToken("abc123")
	if len(got) != 64 {
		t.Fatalf("expected 64-char hex-encoded sha256 digest, got %q (len %d)", got, len(got))
	}
	for _, r := range got {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("expected lowercase hex output, got %q", got)
		}
	}
}
