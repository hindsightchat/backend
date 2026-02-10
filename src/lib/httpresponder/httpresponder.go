package httpresponder

import (
	"encoding/json"
	"io"
	"net/http"
)

type ErrorResponse struct {
	Error string `json:"error"`
	Code  int    `json:"code,omitempty"`
}

// ReadDataToString reads all data from an io.ReadCloser and returns it as a byte slice.
func ReadDataToString(data io.ReadCloser) ([]byte, error) {
	body, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	return body, nil
}

// does the same as send normal response
func SendSuccessResponse(httpWriter http.ResponseWriter, httpRequest *http.Request, payload interface{}) {
	SendNormalResponse(httpWriter, httpRequest, map[string]interface{}{
		"data":    payload,
		"success": true,
	})
}

// SendNormalResponse sends a JSON response with status 200 OK.
func SendNormalResponse(httpWriter http.ResponseWriter, httpRequest *http.Request, payload interface{}) {
	httpWriter.Header().Set("Content-Type", "application/json")
	httpWriter.WriteHeader(http.StatusOK)
	json.NewEncoder(httpWriter).Encode(payload)
}

// SendErrorResponse sends a JSON error response with the specified status code and message.
func SendErrorResponse(httpWriter http.ResponseWriter, httpRequest *http.Request, message string, code int) {
	httpWriter.Header().Set("Content-Type", "application/json")
	httpWriter.WriteHeader(code)
	errorJSON, _ := json.Marshal(ErrorResponse{Error: message, Code: code})
	httpWriter.Write(errorJSON)
}
