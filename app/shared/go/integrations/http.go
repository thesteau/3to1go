package integrations

import (
	"errors"
	"net/http"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/httpx"
	"github.com/go-chi/chi/v5"
)

// Register shares the same admin-only API in both apps.
func Register(router chi.Router, store Store, app string) {
	guard := func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Cache-Control", "no-store")
		user := auth.RequireAdmin(w, r)
		if user == nil {
			return false
		}
		if user.MustChangePassword {
			httpx.WriteError(w, http.StatusForbidden, "change your password before managing integrations")
			return false
		}
		if store == nil {
			httpx.WriteError(w, http.StatusServiceUnavailable, "integrations unavailable")
			return false
		}
		return true
	}
	router.Get("/api/integrations", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"destinations": store.Snapshot(), "events": eventsFor(app)})
	})
	router.Post("/api/integrations", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		var update Update
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		if err := httpx.ReadJSON(r, &update); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid integration request")
			return
		}
		destination, err := store.Save(update)
		if err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, errVault) {
				code = http.StatusInternalServerError
			}
			if errors.Is(err, ErrNotFound) {
				code = http.StatusNotFound
			}
			httpx.WriteError(w, code, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"destination": destination})
	})
	router.Delete("/api/integrations/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		if err := store.Delete(chi.URLParam(r, "id")); err != nil {
			writeFailure(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	})
	router.Post("/api/integrations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		if !guard(w, r) {
			return
		}
		if err := store.Test(r.Context(), chi.URLParam(r, "id")); err != nil {
			writeFailure(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
}

func writeFailure(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	if errors.Is(err, ErrNotFound) {
		code = http.StatusNotFound
	}
	if errors.Is(err, errVault) {
		code = http.StatusInternalServerError
	}
	httpx.WriteError(w, code, err.Error())
}
