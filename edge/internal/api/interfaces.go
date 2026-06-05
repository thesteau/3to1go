package api

import (
	"context"

	"github.com/3to1go/edge/internal/store"
)

type userStorer interface {
	EnsureSchema(ctx context.Context) error
	EnsureDefaultAdmin(ctx context.Context) error
	Authenticate(ctx context.Context, username, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int) (string, error)
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionsForUser(ctx context.Context, userID int) error
	UserForSession(ctx context.Context, token string) (*store.User, error)
	ListUsers(ctx context.Context) ([]*store.User, error)
	GetUserByID(ctx context.Context, id int) (*store.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*store.User, error)
	UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*store.User, error)
	DeleteUser(ctx context.Context, userID int) error
	ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*store.User, error)
}
