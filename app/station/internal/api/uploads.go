package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/shared/protocol"
	"github.com/3to1go/station/internal/ingest"
	"github.com/3to1go/station/internal/store"
)

func (a *App) authorizeBearer(r *http.Request) (*store.CredentialRecord, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, errors.New("unauthorized")
	}
	token := auth[len("Bearer "):]
	s := a.Settings()
	rec, err := a.credStore.Verify(r.Context(), token, s.IssuerPublicKey)
	if err != nil {
		return nil, errors.New("unauthorized")
	}
	return rec, nil
}

func (a *App) authorizeCredentialForInstance(r *http.Request, cred *store.CredentialRecord, scoutID, instID string, allowBinding bool) (int, string) {
	if cred == nil || cred.TokenHash == "" {
		return http.StatusUnauthorized, "unauthorized"
	}
	if allowBinding {
		err := a.credStore.Bind(r.Context(), cred.TokenHash, scoutID, instID)
		switch {
		case err == nil:
			return 0, ""
		case errors.Is(err, store.ErrCredentialUnavailable):
			return http.StatusUnauthorized, err.Error()
		case errors.Is(err, store.ErrCredentialBinding), errors.Is(err, store.ErrCredentialLimit):
			return http.StatusForbidden, err.Error()
		default:
			return http.StatusInternalServerError, "failed to bind Station token"
		}
	}
	reg, err := a.snapIndex.GetScoutRegistration(r.Context(), scoutID, instID)
	if err != nil {
		return http.StatusInternalServerError, "failed to inspect Station token scope"
	}
	if reg != nil && reg.CredentialHash != nil && *reg.CredentialHash == cred.TokenHash {
		return 0, ""
	}
	return http.StatusForbidden, "Station token is not bound to this Scout instance"
}

func (a *App) handleInitiateUpload(w http.ResponseWriter, r *http.Request) {
	if a.Settings().UploadsPaused {
		httpx.WriteError(w, http.StatusServiceUnavailable, "uploads are paused")
		return
	}
	cred, err := a.authorizeBearer(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body ingest.UploadInitRequest
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	metadata := struct {
		ScoutID          string `validate:"required"`
		ScoutInstanceID  string `validate:"required"`
		JobName          string `validate:"required"`
		Fingerprint      string `validate:"required"`
		Timestamp        string `validate:"required"`
		ArchiveSizeBytes int64  `validate:"min=1"`
		IdempotencyKey   string `validate:"required"`
	}{
		ScoutID:          body.ScoutID,
		ScoutInstanceID:  body.ScoutInstanceID,
		JobName:          body.JobName,
		Fingerprint:      body.Fingerprint,
		Timestamp:        body.Timestamp,
		ArchiveSizeBytes: body.ArchiveSizeBytes,
		IdempotencyKey:   body.IdempotencyKey,
	}
	if err := httpx.ValidateStruct(&metadata); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid upload metadata")
		return
	}

	// Validate namespace components
	scoutID, err := ingest.ValidateNamespaceComponent(body.ScoutID, "scout_id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	instID, err := ingest.ValidateNamespaceComponent(body.ScoutInstanceID, "scout_instance_id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	jobName, err := ingest.ValidateNamespaceComponent(body.JobName, "job_name")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.ScoutID = scoutID
	body.ScoutInstanceID = instID
	body.JobName = jobName
	if _, err := time.Parse("2006-01-02T15:04:05Z", body.Timestamp); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "timestamp must be a UTC timestamp in YYYY-MM-DDTHH:MM:SSZ format")
		return
	}
	if !fingerprintQueryRE.MatchString(body.Fingerprint) {
		httpx.WriteError(w, http.StatusBadRequest, "fingerprint must be an 8- or 64-character lowercase hex digest")
		return
	}

	if body.ArchiveFormat != protocol.ArchiveFormatTarZst {
		httpx.WriteError(w, http.StatusBadRequest, "archive_format must be tar.zst")
		return
	}
	if len(body.ArchiveSHA256) != 64 || !fingerprintQueryRE.MatchString(body.ArchiveSHA256) {
		httpx.WriteError(w, http.StatusBadRequest, "archive_sha256 must be a 64-character lowercase hex digest")
		return
	}

	if status, detail := a.authorizeCredentialForInstance(r, cred, scoutID, instID, true); status != 0 {
		httpx.WriteError(w, status, detail)
		return
	}

	credHash := cred.TokenHash
	srcAddr := ingest.SourceAddress(r)
	srcTLS := r.TLS != nil

	resp, err := a.ingest.StartUpload(r.Context(), body, srcAddr, &credHash, srcTLS)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (a *App) handleAppendChunk(w http.ResponseWriter, r *http.Request) {
	cred, err := a.authorizeBearer(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	uploadID := r.PathValue("upload_id")
	if err := a.ingest.AuthorizeUpload(r.Context(), uploadID, cred.TokenHash); err != nil {
		writeHTTPError(w, err)
		return
	}
	offsetStr := r.URL.Query().Get("offset")
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		httpx.WriteError(w, http.StatusBadRequest, "invalid offset parameter")
		return
	}
	resp, err := a.ingest.AppendChunk(r.Context(), uploadID, offset, r.Body)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (a *App) handleFinalizeUpload(w http.ResponseWriter, r *http.Request) {
	cred, err := a.authorizeBearer(r)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	uploadID := r.PathValue("upload_id")
	if err := a.ingest.AuthorizeUpload(r.Context(), uploadID, cred.TokenHash); err != nil {
		writeHTTPError(w, err)
		return
	}
	resp, err := a.ingest.FinalizeUpload(r.Context(), uploadID)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func writeHTTPError(w http.ResponseWriter, err error) {
	if he, ok := errors.AsType[*ingest.HTTPError](err); ok {
		httpx.WriteJSON(w, he.Code, map[string]any{"detail": he.Message})
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, err.Error())
}
