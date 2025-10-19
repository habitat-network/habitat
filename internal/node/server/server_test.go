package server

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestServerFn(t *testing.T) {
	mockServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := fmt.Fprint(w, "Hello, World!")
			require.NoError(t, err)
		}),
	}
	ServeFn(
		mockServer,
		"test-server",
		WithTLSConfig(&tls.Config{}, "cert-path", "key-path"),
	)
}
