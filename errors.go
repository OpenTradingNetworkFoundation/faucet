package main

const (
	ErrUnknown = iota
	ErrInvalidArgument
	ErrRateLimit
	ErrInvalidName
	ErrExpectedCheapName
	ErrInternalError
	ErrOperationFailed
)

type ErrorValue struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}
type ErrorResponse struct {
	Error ErrorValue `json:"error"`
}

func NewErrorResponse(code int, message string) *ErrorResponse {
	return &ErrorResponse{Error: ErrorValue{code, message}}
}
