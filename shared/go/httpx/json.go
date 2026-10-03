package httpx

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
)

var requestValidator = validator.New()

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent, so a failed write has no one left to report to.
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, detail any) {
	WriteJSON(w, status, map[string]any{"detail": detail})
}

func ReadJSON(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(v)
}

func ValidateStruct(v any) error {
	return requestValidator.Struct(v)
}

func SessionCookieSecure() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SESSION_COOKIE_SECURE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
