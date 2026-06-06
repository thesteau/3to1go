package api

import (
	"context"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/store"
)

type userStorer interface {
	EnsureSchema(ctx context.Context) error
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

type edgeRunner interface {
	CurrentSettings() *config.Settings
	UpdateSettings(s *config.Settings) error
	EncryptionKeyFingerprint() string
	EncryptionKeyBase64() string

	StatusSnapshot() map[string]any
	DirectoriesSnapshot() map[string]any

	NtfySnapshot(cfg *config.Settings) map[string]any
	TestNtfy(ntfyURL, ntfyTopic, messageTemplate string) error

	CertSnapshot() map[string]any
	SaveCertFile(filename string, content []byte) (any, error)
	DeleteCertFile(filename string) error

	HookSnapshot(preCmd, postCmd string) map[string]any
	SaveHookFile(filename string, content []byte) (any, error)
	ReadHookFile(filename string) (string, string, error)
	DeleteHookFile(filename string) error

	SaveJob(relativePath string, cfg map[string]any) (any, error)
	DeleteJob(relativePath string) error

	ForceSendJob(ctx context.Context, jobName string) (map[string]any, error)
	PreviewRecovery(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
	RecoverJob(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
}

type schedulerFacade interface {
	Snapshot() map[string]any
	RequestRunNow() string
	ReloadSettings(cronSchedule string) error
}
