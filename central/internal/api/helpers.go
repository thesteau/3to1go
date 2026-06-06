package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-playground/validator/v10"
)

var requestValidator = validator.New()

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, detail interface{}) {
	writeJSON(w, status, map[string]interface{}{"detail": detail})
}

func readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func validateStruct(v interface{}) error {
	return requestValidator.Struct(v)
}
