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
	var he *ingest.HTTPError
	if errors.As(err, &he) {
		writeJSON(w, he.Code, map[string]interface{}{"detail": he.Message})
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}
