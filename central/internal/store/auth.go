package store

import "github.com/3to1go/shared/auth"

func publicUser(u *User) *User { return auth.PublicUser(u) }

func normalizeUsername(s string) (string, error) { return auth.NormalizeUsername(s) }

func hashPassword(s string) (string, error) { return auth.HashPassword(s) }

func verifyPassword(password, encoded string) bool { return auth.VerifyPassword(password, encoded) }

func randomToken() (string, error) { return auth.RandomToken() }
