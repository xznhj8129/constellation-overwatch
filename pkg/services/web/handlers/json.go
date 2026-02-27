package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// webResponse is the standard JSON envelope for web UI AJAX endpoints.
type webResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *webError   `json:"error,omitempty"`
}

type webError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// sendSuccess writes a JSON success response with the given data.
func sendSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(webResponse{Success: true, Data: data}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"error":{"code":"ENCODING_ERROR","message":"Failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	buf.WriteTo(w)
}

// sendError writes a JSON error response.
func sendError(w http.ResponseWriter, statusCode int, code, message string) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(webResponse{
		Success: false,
		Error:   &webError{Code: code, Message: message},
	}); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"error":{"code":"ENCODING_ERROR","message":"Failed to encode response"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	buf.WriteTo(w)
}
