package secrets

import (
	"errors"
	"strings"
	"testing"
)

func newSealer(t *testing.T, material string) *AESSealer {
	t.Helper()
	s, err := NewAESSealer(DeriveKey(material))
	if err != nil {
		t.Fatalf("NewAESSealer: %v", err)
	}
	return s
}

func TestNewAESSealer_RejectsWrongKeyLength(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		if _, err := NewAESSealer(make([]byte, size)); err == nil {
			t.Errorf("NewAESSealer accepted a %d-byte key; only %d is valid", size, KeySize)
		}
	}
}

func TestAESSealer_RoundTrip(t *testing.T) {
	sealer := newSealer(t, "test-key")

	cases := []struct {
		name      string
		plaintext string
	}{
		{"empty", ""},
		{"short", "s3cret"},
		{"unicode", "pässwörd-日本語"},
		{"long", strings.Repeat("x", 8192)},
		{"newlines", "line1\nline2\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := sealer.Seal(tc.plaintext)
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			opened, err := sealer.Open(sealed)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if opened != tc.plaintext {
				t.Errorf("round trip = %q, want %q", opened, tc.plaintext)
			}
		})
	}
}

func TestAESSealer_EmptyPassesThrough(t *testing.T) {
	sealer := newSealer(t, "test-key")

	sealed, err := sealer.Seal("")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed != "" {
		t.Errorf("Seal(\"\") = %q, want empty", sealed)
	}
}

func TestAESSealer_SealIsNonDeterministic(t *testing.T) {
	sealer := newSealer(t, "test-key")

	seen := make(map[string]struct{}, 100)
	for range 100 {
		sealed, err := sealer.Seal("same-plaintext")
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		if _, dup := seen[sealed]; dup {
			t.Fatal("Seal produced a repeated ciphertext; nonce is not unique per call")
		}
		seen[sealed] = struct{}{}
	}
}

func TestAESSealer_OpenRejectsForeignKey(t *testing.T) {
	sealed, err := newSealer(t, "key-a").Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := newSealer(t, "key-b").Open(sealed); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("Open with the wrong key = %v, want ErrInvalidCiphertext", err)
	}
}

func TestAESSealer_OpenDetectsTampering(t *testing.T) {
	sealer := newSealer(t, "test-key")
	sealed, err := sealer.Seal("secret-value")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	tampered := []byte(sealed)

	idx := len(tampered) / 2
	if tampered[idx] == 'A' {
		tampered[idx] = 'B'
	} else {
		tampered[idx] = 'A'
	}

	if _, err := sealer.Open(string(tampered)); !errors.Is(err, ErrInvalidCiphertext) {
		t.Errorf("Open of tampered ciphertext = %v, want ErrInvalidCiphertext", err)
	}
}

func TestAESSealer_OpenRejectsMalformed(t *testing.T) {
	sealer := newSealer(t, "test-key")

	for _, input := range []string{"not-base64!!!", "AAAA", "c2hvcnQ="} {
		if _, err := sealer.Open(input); !errors.Is(err, ErrInvalidCiphertext) {
			t.Errorf("Open(%q) = %v, want ErrInvalidCiphertext", input, err)
		}
	}
}

func TestAESSealer_ConcurrentUse(t *testing.T) {
	sealer := newSealer(t, "test-key")

	done := make(chan struct{})
	for i := range 16 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 50 {
				sealed, err := sealer.Seal("concurrent")
				if err != nil {
					t.Errorf("Seal: %v", err)
					return
				}
				if opened, err := sealer.Open(sealed); err != nil || opened != "concurrent" {
					t.Errorf("goroutine %d: Open = %q, %v", i, opened, err)
					return
				}
			}
		}()
	}
	for range 16 {
		<-done
	}
}

func BenchmarkAESSealer_Seal(b *testing.B) {
	sealer, err := NewAESSealer(DeriveKey("bench"))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := sealer.Seal("a-typical-oauth-client-secret-value"); err != nil {
			b.Fatal(err)
		}
	}
}
