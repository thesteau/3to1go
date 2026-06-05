package api

import (
	"net/http"
	"strconv"

	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/signing"
)

func (a *App) handleMintCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	ttlDays := 365
	if v := r.URL.Query().Get("ttl_days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			writeError(w, http.StatusBadRequest, "ttl_days must be between 1 and 3650")
			return
		}
		ttlDays = n
	}

	s := a.Settings()
	priv, _, err := signing.LoadOrCreateIssuerKeypair(s.IssuerKeyPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load issuer key")
		return
	}
	credential, err := a.credStore.Mint(r.Context(), priv, ttlDays)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint credential")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"credential": credential,
		"ttl_days":   ttlDays,
	})
}

func (a *App) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	edgeID, err := ingest.ValidateNamespaceComponent(r.PathValue("edge_id"), "edge_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	instID, err := ingest.ValidateNamespaceComponent(r.PathValue("edge_instance_id"), "edge_instance_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	reg, err := a.snapIndex.GetEdgeRegistration(r.Context(), edgeID, instID)
	if err != nil || reg == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}
	if reg.CredentialHash == nil || *reg.CredentialHash == "" {
		writeError(w, http.StatusConflict, "instance has not used a database-backed credential yet")
		return
	}
	tokenHash := *reg.CredentialHash

	// Find all instances using this credential
	allRegs, err := a.snapIndex.ListEdgeRegistrations(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list credential users")
		return
	}
	var affected []map[string]string
	for _, r2 := range allRegs {
		if r2.CredentialHash != nil && *r2.CredentialHash == tokenHash {
			affected = append(affected, map[string]string{
				"edge_id":          r2.EdgeID,
				"edge_instance_id": r2.EdgeInstanceID,
			})
		}
	}

	revoked, err := a.credStore.Revoke(r.Context(), tokenHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke credential")
		return
	}

	// Clear credential_hash on affected registrations
	for _, r2 := range allRegs {
		if r2.CredentialHash != nil && *r2.CredentialHash == tokenHash {
			copy := r2
			copy.CredentialHash = nil
			if err := a.snapIndex.UpsertEdgeRegistration(r.Context(), &copy); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update credential registrations")
				return
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":             "revoked",
		"revoked_rows":       revoked,
		"affected_instances": affected,
	})
}
