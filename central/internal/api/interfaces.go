package api

import (
	"context"
	"crypto/ed25519"
	"io"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/store"
)

type userStorer interface {
	UserForSession(ctx context.Context, token string) (*store.User, error)
	Authenticate(ctx context.Context, username, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int) (string, error)
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionsForUser(ctx context.Context, userID int) error
	ListUsers(ctx context.Context) ([]*store.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*store.User, error)
	UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*store.User, error)
	DeleteUser(ctx context.Context, userID int) error
	ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*store.User, error)
}

type credStorer interface {
	Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*store.CredentialRecord, error)
	Mint(ctx context.Context, priv ed25519.PrivateKey, ttlDays int) (string, error)
	Revoke(ctx context.Context, tokenHash string) (int64, error)
	CleanupExpired(ctx context.Context) (int64, error)
}

type settingsStorer interface {
	Save(ctx context.Context, p *config.SettingsPayload) error
}

type snapIndexer interface {
	GetEdgeRegistration(ctx context.Context, edgeID, instID string) (*store.EdgeRegistration, error)
	DeleteEdgeRegistration(ctx context.Context, edgeID, instID string) error
	HasNamespaceEntries(ctx context.Context, edgeID, instID string) (bool, error)
	UpsertEdgeRegistration(ctx context.Context, r *store.EdgeRegistration) error
	ListEdgeRegistrations(ctx context.Context, edgeIDFilter *string) ([]store.EdgeRegistration, error)
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
}

type ingestSvc interface {
	StartUpload(ctx context.Context, req ingest.UploadInitRequest, sourceAddr, credHash *string) (*ingest.SessionResponse, error)
	AppendChunk(ctx context.Context, uploadID string, offset int64, body io.Reader) (*ingest.ChunkResponse, error)
	FinalizeUpload(ctx context.Context, uploadID string) (*ingest.FinalizeResponse, error)
	ReconcileNamespace(ctx context.Context, namespace string)
	CleanupLoop(ctx context.Context, intervalSeconds int)
	UpdateSettings(settings *config.Settings)
}
