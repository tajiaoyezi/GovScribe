package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestBcryptHasherSaltsAndVerifiesPassword(t *testing.T) {
	hasher := BcryptHasher{Cost: bcrypt.MinCost}
	first, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	second, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if first == second {
		t.Fatal("same plaintext must produce different salted hashes")
	}
	if !hasher.Verify(first, "correct horse battery staple") {
		t.Fatal("correct password did not verify")
	}
	if hasher.Verify(first, "wrong") {
		t.Fatal("wrong password verified")
	}
}
