package ingest

import (
	"context"
	"net/http"
)

// AuthorizeUpload checks ownership for every chunk and finalization. Legacy
// sessions have no saved hash, so use their existing instance binding instead.
func (s *Service) AuthorizeUpload(ctx context.Context, uploadID, hash string) error {
	session, err := s.loadSessionContext(ctx, uploadID)
	if err != nil {
		return err
	}
	owner := session.CredentialHash
	if owner == nil || *owner == "" {
		reg, err := s.index.GetScoutRegistration(ctx, session.ScoutID, session.ScoutInstanceID)
		if err != nil {
			return httpError(http.StatusInternalServerError, "failed to inspect upload owner")
		}
		if reg != nil {
			owner = reg.CredentialHash
		}
	}
	if hash == "" || owner == nil || *owner != hash {
		return httpError(http.StatusForbidden, "Station token does not own this upload")
	}
	return nil
}
