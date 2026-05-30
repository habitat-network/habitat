package sync

import "net/http"

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

var (
	ErrUnauthorized  = &APIError{Code: "Unauthorized", Message: "missing or invalid OAuth token", Status: http.StatusUnauthorized}
	ErrForbidden     = &APIError{Code: "Forbidden", Message: "token lacks required scope", Status: http.StatusForbidden}
	ErrSpaceNotFound = &APIError{Code: "SpaceNotFound", Message: "space does not exist or is not visible", Status: http.StatusNotFound}
	ErrRateLimited   = &APIError{Code: "RateLimited", Message: "caller exceeded server policy", Status: http.StatusTooManyRequests}
)

func CursorTooOld(msg string) *APIError {
	return &APIError{Code: "CursorTooOld", Message: msg, Status: http.StatusGone}
}
