package auth

import (
	"context"
	"errors"
	"sync"
)

// UserRepository is the database-independent account lookup and update contract.
type UserRepository interface {
	GetUserByUsername(context.Context, string) (*User, error)
	GetUserByID(context.Context, int) (*User, error)
	UpdateUser(context.Context, int, *string, *string, *bool, *bool) (*User, error)
}

func Authenticate(s UserRepository, ctx context.Context, username, password string) (*User, error) {
	user, err := s.GetUserByUsername(ctx, username)
	if err != nil || user == nil {
		return nil, nil
	}
	if !VerifyPassword(password, user.PasswordHash) {
		return nil, nil
	}
	user, err = WithDefaultPasswordChangeRequired(s, ctx, user)
	if err != nil {
		return nil, err
	}
	return PublicUser(user), nil
}

func ChangePassword(s UserRepository, ctx context.Context, userID int, currentPassword, newPassword string) (*User, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return nil, errors.New("user not found")
	}
	if !VerifyPassword(currentPassword, user.PasswordHash) {
		return nil, errors.New("current password is incorrect")
	}
	return s.UpdateUser(ctx, userID, nil, &newPassword, nil, new(false))
}

// Checking a hash against the default password is deliberately slow (about
// 175 ms, far more on a Raspberry Pi), and every signed-in request runs this
// check. The answer only depends on the stored hash, so it is worked out once
// per hash. A changed password has a new hash and is checked again.
var defaultPasswordHashes sync.Map // password hash -> bool

func isDefaultPassword(hash string) bool {
	if known, ok := defaultPasswordHashes.Load(hash); ok {
		return known.(bool)
	}
	isDefault := VerifyPassword(DefaultAdminPassword, hash)
	defaultPasswordHashes.Store(hash, isDefault)
	return isDefault
}

func WithDefaultPasswordChangeRequired(s UserRepository, ctx context.Context, user *User) (*User, error) {
	if user.MustChangePassword || !isDefaultPassword(user.PasswordHash) {
		return user, nil
	}
	updated, err := s.UpdateUser(ctx, user.ID, nil, nil, nil, new(true))
	if err != nil {
		return user, nil
	}
	updated.PasswordHash = user.PasswordHash
	return updated, nil
}
