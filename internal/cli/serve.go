package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/LaurPl/shiptrace/internal/paths"
	"github.com/LaurPl/shiptrace/internal/server"
	"github.com/LaurPl/shiptrace/internal/store"
)

// loopbackHostsForBind is the closed set of hostnames the serve command
// considers safe to bind without --listen-public. Kept in sync with the
// server-side middleware in internal/server/middleware.go.
var loopbackHostsForBind = map[string]struct{}{
	"":          {}, // implicit loopback when only a port is given
	"127.0.0.1": {},
	"localhost": {},
	"::1":       {},
}

// isLoopbackAddr returns true when addr binds only the loopback interface.
// addr is in net.JoinHostPort form (e.g. "127.0.0.1:7777", "0.0.0.0:7777").
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port — treat the whole string as the host. Unlikely in practice;
		// cobra's flag default always has a port.
		host = strings.TrimSpace(addr)
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	_, ok := loopbackHostsForBind[host]
	return ok
}

// bundleFS is wired by cmd/shiptrace/main.go via SetBundle so the cli
// package doesn't need to import the embed.FS directly. nil = no UI.
var bundleFS fs.FS

// SetBundle records the embedded React bundle. Called from main during
// startup; safe to call once.
func SetBundle(b fs.FS) { bundleFS = b }

func newServeCommand(stdout, _ io.Writer) *cobra.Command {
	var (
		addr         string
		listenPublic bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the local dashboard on http://localhost:7777",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !isLoopbackAddr(addr) && !listenPublic {
				return fmt.Errorf(
					"refusing to bind %s: this exposes shiptrace's unauthenticated JSON API (session labels, project names, file paths, replan signals) to any peer that can reach the interface.\n"+
						"If you really mean it, pass --listen-public. Otherwise drop --addr or set it to 127.0.0.1:7777.", addr)
			}
			dbPath, err := paths.DBPath()
			if err != nil {
				return err
			}
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			srv, err := server.New(server.Options{
				Addr:         addr,
				Store:        s,
				Bundle:       bundleFS,
				AllowAnyHost: listenPublic,
			})
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
			if listenPublic && !isLoopbackAddr(addr) {
				// Red-on-stdout warning so a user who consents-by-flag still
				// sees the consequences spelled out.
				fmt.Fprintln(stdout, "  \x1b[31m⚠ --listen-public is on:\x1b[0m the JSON API is reachable from any peer that can route to this interface.")
				fmt.Fprintln(stdout, "  \x1b[31m⚠\x1b[0m shiptrace has no authentication. Session labels, project names, file paths and replan signals are readable to anyone who connects.")
				fmt.Fprintln(stdout, "  \x1b[31m⚠\x1b[0m if you didn't mean this, Ctrl-C and rerun without --addr (or with --addr 127.0.0.1:7777).")
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
	cmd.Flags().BoolVar(&listenPublic, "listen-public", false, "Allow binding a non-loopback interface. shiptrace has no auth; only use this on a network you trust.")
	return cmd
}
