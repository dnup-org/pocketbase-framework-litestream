package myh2c

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// NewServeH2CCommand creates and returns new command for starting the h2c-enabled server.
func NewServeH2CCommand(app core.App, showStartBanner bool) *cobra.Command {
	var allowedOrigins []string
	var httpAddr string

	command := &cobra.Command{
		Use:     "serve_h2c [domain(s)]",
		Short:   "Starts the h2c-enabled web server",
		Example: "pocketbase serve_h2c",
		Run: func(cmd *cobra.Command, args []string) {
			configureServer(app, cmd, args, allowedOrigins, httpAddr, showStartBanner)
		},
	}

	command.PersistentFlags().StringSliceVar(
		&allowedOrigins,
		"origins",
		[]string{"*"},
		"CORS allowed domain origins list",
	)

	command.PersistentFlags().StringVar(
		&httpAddr,
		"http",
		"127.0.0.1:8080",
		"HTTP server address",
	)

	return command
}

func configureServer(app core.App, cmd *cobra.Command, args []string, allowedOrigins []string, httpAddr string, showStartBanner bool) {
	config := ServeConfig{
		HttpAddr:           httpAddr,
		ShowStartBanner:    showStartBanner,
		AllowedOrigins:     allowedOrigins,
		CertificateDomains: args,
	}

	if err := ServeH2C(app, config); err != nil && !errors.Is(err, http.ErrServerClosed) {
		cmd.PrintErrf("Failed to start the h2c server: %v", err)
	}
}
