package identity

import (
	"errors"
	"strings"
	"testing"
)

func TestHashPassword_RejectsShortPasswords(t *testing.T) {
	for _, pw := range []string{"", "a", "short"} {
		if _, err := HashPassword(pw); !errors.Is(err, ErrPasswordTooShort) {
			t.Errorf("HashPassword(%q) = %v, want ErrPasswordTooShort", pw, err)
		}
	}
}

func TestHashPassword_RejectsOverlongPasswords(t *testing.T) {
	if _, err := HashPassword(strings.Repeat("x", 73)); !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("HashPassword(73 bytes) = %v, want ErrPasswordTooLong", err)
	}

	if _, err := HashPassword(strings.Repeat("x", 72)); err != nil {
		t.Errorf("HashPassword(72 bytes) = %v, want it accepted", err)
	}
}

func TestHashPassword_RoundTrip(t *testing.T) {
	const password = "correct-horse-battery"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == password {
		t.Fatal("HashPassword returned the plaintext")
	}
	if !VerifyPassword(hash, password) {
		t.Error("VerifyPassword rejected the correct password")
	}
	if VerifyPassword(hash, password+"x") {
		t.Error("VerifyPassword accepted a wrong password")
	}
}

func TestHashPassword_IsSalted(t *testing.T) {
	first, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Error("two hashes of the same password are identical; the salt is not random")
	}
}

func TestVerifyPassword_RejectsMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-bcrypt-hash", "$2a$"} {
		if VerifyPassword(hash, "anything") {
			t.Errorf("VerifyPassword accepted the malformed hash %q", hash)
		}
	}
}

func TestIsPasswordPolicyError(t *testing.T) {
	if !IsPasswordPolicyError(ErrPasswordTooShort) || !IsPasswordPolicyError(ErrPasswordTooLong) {
		t.Error("IsPasswordPolicyError did not recognise a policy rejection")
	}
	if IsPasswordPolicyError(errors.New("database down")) {
		t.Error("IsPasswordPolicyError misclassified an internal error as a policy rejection")
	}
}

func TestBurnPasswordComparison(t *testing.T) {
	BurnPasswordComparison("anything")
	BurnPasswordComparison("")
}

func TestNormalizeEmail(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "lowercased", in: "Ada@Example.COM", want: "ada@example.com"},
		{name: "trimmed", in: "  ada@example.com  ", want: "ada@example.com"},
		{name: "plus addressing preserved", in: "ada+tag@example.com", want: "ada+tag@example.com"},
		{name: "no at sign", in: "ada", wantErr: true},
		{name: "empty local part", in: "@example.com", wantErr: true},
		{name: "empty domain", in: "ada@", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "embedded whitespace", in: "a da@example.com", wantErr: true},
		{name: "embedded newline", in: "ada@example.com\nbcc:x@y.z", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeEmail(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidEmail) {
					t.Fatalf("NormalizeEmail(%q) = %q, %v; want ErrInvalidEmail", tc.in, got, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeEmail(%q) = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeEmail_CaseVariantsCollapse(t *testing.T) {
	variants := []string{"ada@example.com", "Ada@Example.com", "ADA@EXAMPLE.COM"}

	var first string
	for i, v := range variants {
		got, err := NormalizeEmail(v)
		if err != nil {
			t.Fatalf("NormalizeEmail(%q): %v", v, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", v, got, first)
		}
	}
}

func TestEmailDomain(t *testing.T) {
	cases := map[string]string{
		"ada@example.com":       "example.com",
		"ada@mail.example.com":  "mail.example.com",
		"ada+tag@example.co.uk": "example.co.uk",
		"nodomain":              "nodomain",
	}
	for in, want := range cases {
		if got := emailDomain(in); got != want {
			t.Errorf("emailDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChooseMembership(t *testing.T) {
	memberships := []OrgMembership{
		{MembershipID: 1, OrganizationID: 10},
		{MembershipID: 2, OrganizationID: 20},
	}

	t.Run("explicit organization is honoured", func(t *testing.T) {
		got, err := chooseMembership(memberships, 20)
		if err != nil {
			t.Fatalf("chooseMembership: %v", err)
		}
		if got.MembershipID != 2 {
			t.Errorf("MembershipID = %d, want 2", got.MembershipID)
		}
	})

	t.Run("non-member organization is refused", func(t *testing.T) {
		if _, err := chooseMembership(memberships, 999); !errors.Is(err, ErrNotAMember) {
			t.Errorf("chooseMembership = %v, want ErrNotAMember", err)
		}
	})

	t.Run("a sole membership is selected automatically", func(t *testing.T) {
		got, err := chooseMembership(memberships[:1], 0)
		if err != nil {
			t.Fatalf("chooseMembership: %v", err)
		}
		if got == nil || got.MembershipID != 1 {
			t.Errorf("chooseMembership = %v, want the single membership", got)
		}
	})

	t.Run("ambiguity asks the caller to choose", func(t *testing.T) {
		got, err := chooseMembership(memberships, 0)
		if err != nil {
			t.Fatalf("chooseMembership: %v", err)
		}
		if got != nil {
			t.Errorf("chooseMembership = %v, want nil so the caller is prompted", got)
		}
	})
}

func BenchmarkHashPassword(b *testing.B) {
	for b.Loop() {
		if _, err := HashPassword("a-representative-password"); err != nil {
			b.Fatal(err)
		}
	}
}
