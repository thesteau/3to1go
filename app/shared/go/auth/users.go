package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

const (
	DefaultAdminUsername = "admin"
	DefaultAdminPassword = "admin"
	BootstrapAdminID     = 1
	bcryptCost           = 12
)

type User struct {
	ID                 int    `json:"id"`
	Username           string `json:"username"`
	PasswordHash       string `json:"-"`
	IsAdmin            bool   `json:"is_admin"`
	IsBootstrapAdmin   bool   `json:"is_bootstrap_admin"`
	MustChangePassword bool   `json:"must_change_password"`
	CreatedAt          string `json:"created_at"`
}

func PublicUser(u *User) *User {
	return &User{
		ID:                 u.ID,
		Username:           u.Username,
		PasswordHash:       u.PasswordHash,
		IsAdmin:            u.IsAdmin,
		IsBootstrapAdmin:   u.ID == BootstrapAdminID,
		MustChangePassword: u.MustChangePassword,
		CreatedAt:          u.CreatedAt,
	}
}

func NormalizeUsername(username string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	if len(normalized) < 3 {
		return "", errors.New("username must be at least 3 characters")
	}
	if len(normalized) > 64 {
		return "", errors.New("username must be at most 64 characters")
	}
	for _, ch := range normalized {
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '-' && ch != '.' {
			return "", errors.New("username can only contain letters, numbers, dots, dashes, and underscores")
		}
	}
	return normalized, nil
}

func HashPassword(password string) (string, error) {
	if len(password) < 5 {
		return "", errors.New("password must be at least 5 characters")
	}
	if strings.TrimSpace(password) == "" {
		return "", errors.New("password must contain at least one non-space character")
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(digest), nil
}

func VerifyPassword(password, encoded string) bool {
	if strings.HasPrefix(encoded, "$2a$") || strings.HasPrefix(encoded, "$2b$") || strings.HasPrefix(encoded, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) == nil
	}
	return VerifyPBKDF2Password(password, encoded)
}

func VerifyPBKDF2Password(password, encoded string) bool {
	parts := strings.SplitN(encoded, "$", 4)
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iterations, err := parseInt(parts[1])
	if err != nil {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	digest := pbkdf2.Key([]byte(password), salt, iterations, sha256.Size, sha256.New)
	return hmac.Equal(digest, expected)
}

func parseInt(s string) (int, error) {
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("not an integer")
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random session token bytes: %w", err)
	}
	return hex.EncodeToString(b), nil
}
