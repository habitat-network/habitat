package pearsetup

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"
)

// Run serves until ctx is cancelled, then shuts the HTTP server and the OAuth
// garbage collector down. It does not call Close; the caller owns that, since
// a caller may want to inspect the instance after Run returns.
func (p *Pear) Run(ctx context.Context) error {
	s := &http.Server{
		Handler:           p.Handler(),
		Addr:              fmt.Sprintf(":%s", p.Config.Port),
		ReadHeaderTimeout: 30 * time.Second,
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return p.oauthGC.Run(egCtx)
	})
	eg.Go(func() error {
		slog.InfoContext(egCtx, "starting server", "port", p.Config.Port)
		if p.Config.HTTPSCerts == "" {
			return s.ListenAndServe()
		}
		return s.ListenAndServeTLS(
			filepath.Join(p.Config.HTTPSCerts, "fullchain.pem"),
			filepath.Join(p.Config.HTTPSCerts, "privkey.pem"),
		)
	})
	eg.Go(func() error {
		<-egCtx.Done()
		slog.InfoContext(egCtx, "shutting down server")
		if err := s.Shutdown(context.Background()); err != nil {
			slog.ErrorContext(egCtx, "error shutting down http server", "err", err)
		}
		return nil
	})

	return eg.Wait()
}
