package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/store"
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

func (a *App) authorizeCredentialForInstance(r *http.Request, cred *store.CredentialRecord, edgeID, instID string, allowBinding bool) (int, string) {
	if cred == nil || cred.TokenHash == "" {
		return http.StatusUnauthorized, "unauthorized"
	}
	reg, err := a.snapIndex.GetEdgeRegistration(r.Context(), edgeID, instID)
	if err != nil {
		return http.StatusInternalServerError, "failed to inspect credential scope"
	}
	if reg != nil && reg.CredentialHash != nil && *reg.CredentialHash != "" {
		if *reg.CredentialHash == cred.TokenHash {
			return 0, ""
		}
		return http.StatusForbidden, "credential is not bound to this edge instance"
	}
	if !allowBinding {
		return http.StatusForbidden, "credential has not been bound to this edge instance"
	}

	allRegs, err := a.snapIndex.ListEdgeRegistrations(r.Context(), nil)
	if err != nil {
		return http.StatusInternalServerError, "failed to inspect credential users"
	}
	used := 0
	for _, r2 := range allRegs {
		if r2.CredentialHash != nil && *r2.CredentialHash == cred.TokenHash {
			used++
		}
	}
	limit := max(cred.MaxRegistrations, 1)
	if !cred.Shared {
		limit = 1
	}
	if used >= limit {
		if cred.Shared {
			return http.StatusForbidden, "shared credential registration limit reached"
		}
		return http.StatusForbidden, "single-use credential is already bound to another edge instance"
	}
	return 0, ""
}

func (a *App) handleInitiateUpload(w http.ResponseWriter, r *http.Request) {
	cred, err := a.authorizeBearer(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var body ingest.UploadInitRequest
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	metadata := struct {
		EdgeID           string `validate:"required"`
		EdgeInstanceID   string `validate:"required"`
		JobName          string `validate:"required"`
		Fingerprint      string `validate:"required"`
		Timestamp        string `validate:"required"`
		ArchiveSizeBytes int64  `validate:"min=1"`
		IdempotencyKey   string `validate:"required"`
	}{
		EdgeID:           body.EdgeID,
		EdgeInstanceID:   body.EdgeInstanceID,
		JobName:          body.JobName,
		Fingerprint:      body.Fingerprint,
		Timestamp:        body.Timestamp,
		ArchiveSizeBytes: body.ArchiveSizeBytes,
		IdempotencyKey:   body.IdempotencyKey,
	}
	if err := validateStruct(&metadata); err != nil {
		writeError(w, http.StatusBadRequest, "invalid upload metadata")
		return
	}

	// Validate namespace components
	edgeID, err := ingest.ValidateNamespaceComponent(body.EdgeID, "edge_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	instID, err := ingest.ValidateNamespaceComponent(body.EdgeInstanceID, "edge_instance_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	jobName, err := ingest.ValidateNamespaceComponent(body.JobName, "job_name")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	body.EdgeID = edgeID
	body.EdgeInstanceID = instID
	body.JobName = jobName

	if status, detail := a.authorizeCredentialForInstance(r, cred, edgeID, instID, true); status != 0 {
		writeError(w, status, detail)
		return
	}

	if body.ArchiveFormat != "tar.zst" {
		writeError(w, http.StatusBadRequest, "archive_format must be tar.zst")
		return
	}
	if len(body.ArchiveSHA256) != 64 {
		writeError(w, http.StatusBadRequest, "archive_sha256 must be a 64-character lowercase hex digest")
		return
	}

	credHash := cred.TokenHash
	srcAddr := ingest.SourceAddress(r)

	resp, err := a.ingest.StartUpload(r.Context(), body, srcAddr, &credHash)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleAppendChunk(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authorizeBearer(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	uploadID := r.PathValue("upload_id")
	offsetStr := r.URL.Query().Get("offset")
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil || offset < 0 {
		writeError(w, http.StatusBadRequest, "invalid offset parameter")
		return
	}
	resp, err := a.ingest.AppendChunk(r.Context(), uploadID, offset, r.Body)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleFinalizeUpload(w http.ResponseWriter, r *http.Request) {
	if _, err := a.authorizeBearer(r); err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	uploadID := r.PathValue("upload_id")
	resp, err := a.ingest.FinalizeUpload(r.Context(), uploadID)
	if err != nil {
		writeHTTPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeHTTPError(w http.ResponseWriter, err error) {
	if he, ok := errors.AsType[*ingest.HTTPError](err); ok {
		writeJSON(w, he.Code, map[string]any{"detail": he.Message})
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
