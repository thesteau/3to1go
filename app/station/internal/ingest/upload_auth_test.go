package ingest

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/3to1go/station/internal/store"
)

func TestUploadOwnershipAndLegacySessions(t *testing.T) {
	owner := "owner-hash"
	for _, tc := range []struct {
		name     string
		saved    *string
		reg      *store.ScoutRegistration
		supplied string
		want     int
	}{
		{"owner", &owner, nil, owner, 0},
		{"other token", &owner, nil, "another-hash", 403},
		{"empty token", &owner, nil, "", 403},
		{"legacy owner", nil, &store.ScoutRegistration{CredentialHash: &owner}, owner, 0},
		{"legacy other", nil, &store.ScoutRegistration{CredentialHash: &owner}, "another-hash", 403},
		{"legacy unbound", nil, nil, owner, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newServiceWithIndex(t, &mockIndex{getScoutRegistrationResult: tc.reg})
			if err := svc.saveSession(&UploadSession{UploadID: "test-upload", ScoutID: "scout", ScoutInstanceID: "instance", CredentialHash: tc.saved}); err != nil {
				t.Fatal(err)
			}
			err := svc.AuthorizeUpload(context.Background(), "test-upload", tc.supplied)
			if tc.want == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var he *HTTPError
			if !errors.As(err, &he) || he.Code != tc.want {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestStartUploadPersistsOwnerAndRejectsCrossInstanceIdempotency(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})
	ctx := context.Background()
	owner := "owner-hash"
	req := UploadInitRequest{ScoutID: "scout", ScoutInstanceID: "instance", JobName: "job", ArchiveSizeBytes: 10, ArchiveSHA256: "same-sha", IdempotencyKey: "same-key"}
	response, err := svc.StartUpload(ctx, req, nil, &owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AuthorizeUpload(ctx, response.UploadID, owner); err != nil {
		t.Fatalf("owner not persisted: %v", err)
	}
	req.ScoutInstanceID = "different-instance"
	_, err = svc.StartUpload(ctx, req, nil, &owner, false)
	var he *HTTPError
	if !errors.As(err, &he) || he.Code != http.StatusConflict {
		t.Fatalf("cross-instance idempotency: %v", err)
	}
}
