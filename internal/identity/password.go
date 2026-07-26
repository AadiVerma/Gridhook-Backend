package identity

import "golang.org/x/crypto/bcrypt"

// HashPassword is deliberately slow and one-way — never reversible, and
// categorically separate from the Sealer used for connector credentials.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
