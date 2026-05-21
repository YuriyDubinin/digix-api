package response

import (
	"encoding/json"
	"net/http"
)

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorPayload struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []FieldError `json:"details,omitempty"`
}

type ErrorResponse struct {
	Error ErrorPayload `json:"error"`
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, status int, code, message string, details ...FieldError) {
	WriteJSON(w, status, ErrorResponse{
		Error: ErrorPayload{Code: code, Message: message, Details: details},
	})
}

func WriteValidationError(w http.ResponseWriter, message string, details ...FieldError) {
	WriteError(w, http.StatusUnprocessableEntity, "validation_error", message, details...)
}

func WriteNotFound(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusNotFound, "not_found", message)
}

func WriteInternalError(w http.ResponseWriter) {
	WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func WriteBadRequest(w http.ResponseWriter, message string) {
	WriteError(w, http.StatusBadRequest, "bad_request", message)
}
