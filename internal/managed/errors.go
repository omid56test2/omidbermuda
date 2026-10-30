package managed

import "net/http"

type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }
func bad(m string) error          { return &APIError{Status: http.StatusBadRequest, Message: m} }
func notFound(m string) error     { return &APIError{Status: http.StatusNotFound, Message: m} }
func conflict(m string) error     { return &APIError{Status: http.StatusConflict, Message: m} }
func forbidden(m string) error    { return &APIError{Status: http.StatusForbidden, Message: m} }
