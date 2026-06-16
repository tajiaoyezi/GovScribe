package auth

import "golang.org/x/crypto/bcrypt"

const PasswordHashAlgorithmBcrypt = "bcrypt"
const DefaultBcryptCost = 12

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
	Algorithm() string
}

type BcryptHasher struct {
	Cost int
}

func (h BcryptHasher) Hash(password string) (string, error) {
	cost := h.Cost
	if cost == 0 {
		cost = DefaultBcryptCost
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(hashed), nil
}

func (h BcryptHasher) Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (h BcryptHasher) Algorithm() string {
	return PasswordHashAlgorithmBcrypt
}
