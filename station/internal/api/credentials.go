package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/station/internal/ingest"
	"github.com/3to1go/station/internal/signing"
)

func (a *App) handleMintCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	ttlDays := 365
	shared := false
	maxRegistrations := 1
	if v := r.URL.Query().Get("ttl_days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 3650 {
			httpx.WriteError(w, http.StatusBadRequest, "ttl_days must be between 1 and 3650")
			return
		}
		ttlDays = n
	}
	if r.Header.Get("Content-Type") != "" {
		var body struct {
			TTLDays          int  `json:"ttl_days" validate:"omitempty,min=1,max=3650"`
			Shared           bool `json:"shared"`
			MaxRegistrations int  `json:"max_registrations" validate:"omitempty,min=1,max=10000"`
		}
		if err := httpx.ReadJSON(r, &body); err == nil {
			if err := httpx.ValidateStruct(&body); err != nil {
				httpx.WriteError(w, http.StatusBadRequest, "invalid credential options")
				return
			}
			if body.TTLDays != 0 {
				ttlDays = body.TTLDays
			}
			shared = body.Shared
			if body.MaxRegistrations != 0 {
				maxRegistrations = body.MaxRegistrations
			}
		} else if strings.TrimSpace(r.URL.Query().Get("ttl_days")) == "" {
			httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if shared {
		if maxRegistrations < 2 || maxRegistrations > 10000 {
			httpx.WriteError(w, http.StatusBadRequest, "max_registrations must be between 2 and 10000 for shared credentials")
			return
		}
	} else {
		maxRegistrations = 1
	}

	s := a.Settings()
	priv, _, err := signing.LoadOrCreateIssuerKeypair(s.IssuerKeyPath)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load issuer key")
		return
	}
	scope := signing.CredentialScope{Shared: shared, MaxRegistrations: maxRegistrations}
	credential, err := a.credStore.Mint(r.Context(), priv, ttlDays, scope)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to mint credential")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"credential":        credential,
		"ttl_days":          ttlDays,
		"shared":            shared,
		"max_registrations": maxRegistrations,
		"message":           "This token can be revoked from Station after a Scout instance reports in with it, or it can expire naturally.",
	})
}

func (a *App) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	scoutID, err := ingest.ValidateNamespaceComponent(r.PathValue("scout_id"), "scout_id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	instID, err := ingest.ValidateNamespaceComponent(r.PathValue("scout_instance_id"), "scout_instance_id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	reg, err := a.snapIndex.GetScoutRegistration(r.Context(), scoutID, instID)
	if err != nil || reg == nil {
		httpx.WriteError(w, http.StatusNotFound, "instance not found")
		return
	}
	if reg.CredentialHash == nil || *reg.CredentialHash == "" {
		httpx.WriteError(w, http.StatusConflict, "instance has not used a database-backed credential yet")
		return
	}
	tokenHash := *reg.CredentialHash

	// Find all instances using this credential
	allRegs, err := a.snapIndex.ListScoutRegistrations(r.Context(), nil)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list credential users")
		return
	}
	var affected []map[string]string
	for _, r2 := range allRegs {
		if r2.CredentialHash != nil && *r2.CredentialHash == tokenHash {
			affected = append(affected, map[string]string{
				"scout_id":          r2.ScoutID,
				"scout_instance_id": r2.ScoutInstanceID,
			})
		}
	}

	revoked, err := a.credStore.Revoke(r.Context(), tokenHash)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke credential")
		return
	}

	// Clear credential_hash on affected registrations
	for _, r2 := range allRegs {
		if r2.CredentialHash != nil && *r2.CredentialHash == tokenHash {
			copy := r2
			copy.CredentialHash = nil
			if err := a.snapIndex.UpsertScoutRegistration(r.Context(), &copy); err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "failed to update credential registrations")
				return
			}
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"status":             "revoked",
		"revoked_rows":       revoked,
		"affected_instances": affected,
	})
}
