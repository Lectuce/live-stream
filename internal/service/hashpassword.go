package service

import (
	"log"

	"github.com/alexedwards/argon2id"
)

var params = &argon2id.Params{
	Memory:      19 * 1024,
	Iterations:  2,
	Parallelism: 1,
	SaltLength:  16,
	KeyLength:   32,
}

func HashPassword(password string) (string, error) {

	hash, err := argon2id.CreateHash(password, params)
	if err != nil {
		log.Printf("create hash failed: %v", err)
		return "", err
	}
	return hash, nil

}

func VerifyPassword(password string, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		log.Printf("verify password failed: %v", err)
		return match, err
	}
	return match, nil
}
