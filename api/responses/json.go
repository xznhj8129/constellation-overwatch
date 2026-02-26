package responses

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/services/logger"
	"github.com/Constellation-Overwatch/constellation-overwatch/pkg/shared"
)

// SendSuccess sends a success response with data
func SendSuccess(w http.ResponseWriter, statusCode int, data interface{}) {
	response := shared.Response{
		Success: true,
		Data:    data,
	}

	// Encode to buffer first to catch encoding errors before writing headers
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		logger.Errorf("Failed to encode success response: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(shared.Response{
			Success: false,
			Error: &shared.Error{
				Code:    "ENCODING_ERROR",
				Message: "Failed to encode response",
			},
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	buf.WriteTo(w)
}

// SendError sends an error response
func SendError(w http.ResponseWriter, statusCode int, code, message string) {
	response := shared.Response{
		Success: false,
		Error: &shared.Error{
			Code:    code,
			Message: message,
		},
	}

	// Encode to buffer first to catch encoding errors before writing headers
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(response); err != nil {
		logger.Errorf("Failed to encode error response: %v", err)
		// Fallback to a minimal error response that we know can be encoded
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"success":false,"error":{"code":"ENCODING_ERROR","message":"Failed to encode response"}}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	buf.WriteTo(w)
}
