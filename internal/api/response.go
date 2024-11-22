package api

// Response represents the standard API response envelope
type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// ErrorResponse represents the error response envelope
type ErrorResponse struct {
	Status    string      `json:"status"`
	Message   string      `json:"message"`
	ErrorType string      `json:"error_type"`
	Data      interface{} `json:"data,omitempty"`
}

// NewResponse creates a success response
func NewResponse(data interface{}) Response {
	return Response{
		Status: "success",
		Data:   data,
	}
}

// NewErrorResponse creates an error response
func NewErrorResponse(message string, errorType string, data interface{}) ErrorResponse {
	return ErrorResponse{
		Status:    "error",
		Message:   message,
		ErrorType: "GeneralException",
		Data:      data,
	}
}
