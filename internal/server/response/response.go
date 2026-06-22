package response

import (
	"encoding/json"
	"net/http"
)

type envelope struct {
	Data any `json:"data"`
}

func Data(w http.ResponseWriter, status int, data any) error {
	return writeJSON(w, status, envelope{Data: data})
}

func writeJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(body)
}
