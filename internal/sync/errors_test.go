package sync

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{Code: "TestError", Message: "test message", Status: 400}
	assert.Equal(t, "TestError: test message", err.Error())
}

func TestAPIError_StatusCode(t *testing.T) {
	tests := []struct {
		err      *APIError
		expected int
	}{
		{ErrUnauthorized, http.StatusUnauthorized},
		{ErrForbidden, http.StatusForbidden},
		{ErrSpaceNotFound, http.StatusNotFound},
		{ErrRateLimited, http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.err.Code, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Status)
		})
	}
}

func TestAPIError_ImplementsError(t *testing.T) {
	var _ error = &APIError{}

	err := ErrUnauthorized
	assert.True(t, errors.Is(err, ErrUnauthorized))
	assert.False(t, errors.Is(err, ErrForbidden))
}

func TestCursorTooOld(t *testing.T) {
	err := CursorTooOld("cursor is beyond retention window")
	assert.Equal(t, "CursorTooOld", err.Code)
	assert.Equal(t, "cursor is beyond retention window", err.Message)
	assert.Equal(t, http.StatusGone, err.Status)
	assert.Equal(t, "CursorTooOld: cursor is beyond retention window", err.Error())
}

func TestCursorTooOld_DefaultMessage(t *testing.T) {
	err := CursorTooOld("")
	assert.Equal(t, "CursorTooOld", err.Code)
	assert.Equal(t, "", err.Message)
	assert.Equal(t, http.StatusGone, err.Status)
}
