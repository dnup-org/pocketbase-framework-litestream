package myh2c

import (
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// ServeConfig defines a configuration struct for ServeH2C().
type ServeConfig struct {
	HttpAddr           string
	ShowStartBanner    bool
	AllowedOrigins     []string
	CertificateDomains []string
}

// ServeH2C starts a new app web server with h2c support.
func ServeH2C(app core.App, config ServeConfig) error {
	if len(config.AllowedOrigins) == 0 {
		config.AllowedOrigins = []string{"*"}
	}

	// ensure that the latest migrations are applied before starting the server
	if err := runMigrations(app); err != nil {
		return err
	}

	// reload app settings in case a new default value was set with a migration
	if err := app.RefreshSettings(); err != nil {
		color.Yellow("=====================================")
		color.Yellow("WARNING: Settings load error! \n%v", err)
		color.Yellow("Fallback to the application defaults.")
		color.Yellow("=====================================")
	}

	router, err := apis.InitApi(app)
	if err != nil {
		return err
	}

	// configure cors
	router.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper:      middleware.DefaultSkipper,
		AllowOrigins: config.AllowedOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
	}))

	// extract the host names for the certificate host policy
	var hostNames []string
	if len(config.CertificateDomains) == 0 {
		host, _, _ := net.SplitHostPort(config.HttpAddr)
		hostNames = append(hostNames, host)
	} else {
		hostNames = config.CertificateDomains
	}

	// ensure valid hostNames
	for i, name := range hostNames {
		hostNames[i] = strings.TrimSpace(name)
	}
	hostNames = removeDuplicates(hostNames)

	// implicit www->non-www redirect(s)
	var wwwRedirects []string
	for _, host := range hostNames {
		if strings.HasPrefix(host, "www.") {
			continue // explicitly set www host
		}
		wwwHost := "www." + host
		if !contains(hostNames, wwwHost) {
			wwwRedirects = append(wwwRedirects, wwwHost)
		}
	}
	if len(wwwRedirects) > 0 {
		router.Pre(func(next echo.HandlerFunc) echo.HandlerFunc {
			return func(c echo.Context) error {
				host := c.Request().Host
				if strings.HasPrefix(host, "www.") && contains(wwwRedirects, host) {
					return c.Redirect(
						http.StatusTemporaryRedirect,
						"http://"+host[4:]+c.Request().RequestURI,
					)
				}
				return next(c)
			}
		})
	}

	// create and use the h2c handler
	h2s := &http2.Server{
		MaxConcurrentStreams: 250,
		MaxReadFrameSize:     1 << 20,
		IdleTimeout:          10 * time.Second,
	}

	// Add logging middleware to the Echo router
	router.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Received request: %s %s", req.Method, req.URL.Path)
			log.Printf("Headers: %v", req.Header)
			return next(c)
		}
	})

	// Create the h2c handler using the Echo router
	h2cHandler := h2c.NewHandler(router, h2s)

	// create the server
	server := &http.Server{
		ReadTimeout:       10 * time.Minute,
		ReadHeaderTimeout: 30 * time.Second,
		// WriteTimeout: 60 * time.Second, // breaks sse!
		Handler: h2cHandler,
		Addr:    config.HttpAddr,
	}

	serveEvent := &core.ServeEvent{
		App:         app,
		Router:      router,
		Server:      server,
		CertManager: nil, // We don't use CertManager with h2c
	}
	if err := app.OnBeforeServe().Trigger(serveEvent); err != nil {
		return err
	}

	if config.ShowStartBanner {
		schema := "http"
		addr := server.Addr

		date := new(strings.Builder)
		log.New(date, "", log.LstdFlags).Print()

		bold := color.New(color.Bold).Add(color.FgGreen)
		bold.Printf(
			"%s Server started at %s\n",
			strings.TrimSpace(date.String()),
			color.CyanString("%s://%s", schema, addr),
		)

		regular := color.New()
		regular.Printf("├─ REST API: %s\n", color.CyanString("%s://%s/api/", schema, addr))
		regular.Printf("└─ Admin UI: %s\n", color.CyanString("%s://%s/_/", schema, addr))
	}

	return server.ListenAndServe()
}

func runMigrations(app core.App) error {
	// This is a placeholder for the migration logic.
	// In the actual implementation, you'd need to run the migrations for both the main app and logs.
	// The exact implementation depends on how PocketBase handles migrations internally.
	return nil
}

// Helper function to remove duplicates from a slice of strings
func removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

// Helper function to check if a slice contains a string
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}
