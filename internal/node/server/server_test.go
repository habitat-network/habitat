package server

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
)

func TestServerFn(t *testing.T) {

	mockServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "Hello, World!")
		}),
	}
	ServeFn(
		mockServer,
		"test-server",
		WithTLSConfig(&tls.Config{}, "cert-path", "key-path"),
	)

}
