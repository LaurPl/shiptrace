package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/server"
	"github.com/LaurPl/shiptrace/internal/store"
)

// bundleFS is wired by cmd/shiptrace/main.go via SetBundle so the cli
// package doesn't need to import the embed.FS directly. nil = no UI.
var bundleFS fs.FS

// SetBundle records the embedded React bundle. Called from main during
// startup; safe to call once.
func SetBundle(b fs.FS) { bundleFS = b }

func newServeCommand(stdout, _ io.Writer) *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local dashboard on http://localhost:7777",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := paths.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			srv, err := server.New(server.Options{Addr: addr, Store: s, Bundle: bundleFS})
			if err != nil {
				return err
			}

			ctx, cancel := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			httpSrv := &http.Server{
				Addr:              srv.Addr(),
				Handler:           srv.Handler(),
				ReadHeaderTimeout: 5 * time.Second,
			}
			fmt.Fprintf(stdout, "shiptrace serve → http://%s/\n", srv.Addr())
			if bundleFS == nil {
				fmt.Fprintln(stdout, "  (bundle not embedded; serving JSON API + fallback page only)")
				fmt.Fprintln(stdout, "  build it with: cd web && npm install && npm run build")
			}
			fmt.Fprintln(stdout, "  press Ctrl-C to stop")

			errCh := make(chan error, 1)
			go func() { errCh <- httpSrv.ListenAndServe() }()
			select {
			case <-ctx.Done():
				shutdownCtx, sCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer sCancel()
				return httpSrv.Shutdown(shutdownCtx)
			case err := <-errCh:
				if err != nil && err != http.ErrServerClosed {
					return err
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&addr, "addr", server.DefaultAddr, "Address to bind (default: 127.0.0.1:7777)")
	return cmd
}
