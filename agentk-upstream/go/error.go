package upstream

import "fmt"

type APIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("HTTP %d code=%s: %s", e.StatusCode, e.Code, e.Message)
}
