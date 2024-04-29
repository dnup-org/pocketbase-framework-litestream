// main.go
package main

import (
	"backend/handler"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

func init() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}
	handler.SetupHandlerVars()
}

var app *pocketbase.PocketBase

func main() {

	app = pocketbase.New()
	appHandler := &handler.AppHandler{App: app}

	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {

		// Load auth state from cookie
		e.Router.Use(appHandler.LoadAuthContextFromCookie())

		// Stripe endpoints
		e.Router.GET("/checkout", appHandler.CheckoutHandler, handler.AuthGuard)
		e.Router.POST("/webhook", appHandler.WebhookHandler)

		// Pay protected files
		e.Router.GET("/content/:file", func(c echo.Context) error {
			fileName := c.PathParam("file")
			filePath := "./content/" + fileName
			return c.File(filePath)
		}, handler.PaidGuard)

		// Static files
		e.Router.GET("/public/*", apis.StaticDirectoryHandler(os.DirFS("/usr/local/bin/pb_public"), false))

		// Doc Views
		e.Router.GET("/docs/:doc", handler.RenderDocViewHandler("./public/docs/"))

		// Template Views
		e.Router.GET("/", handler.RenderViewHandler("./public/views/index.html", nil))
		e.Router.GET("/get", handler.RenderViewHandler("./public/views/get.html", nil))
		e.Router.GET("/login", handler.RenderViewHandler("./public/views/login.html", nil))
		e.Router.GET("/signup", handler.RenderViewHandler("./public/views/signup.html", nil))
		e.Router.GET("/reset", handler.RenderViewHandler("./public/views/reset.html", nil))
		e.Router.GET("/dashboard", handler.RenderViewHandler("./public/views/dashboard.html", nil))
		e.Router.GET("/app", handler.RenderViewHandler("./public/views/app.html", nil), handler.PaidGuard)

		// System Statistics
		e.Router.GET("/stats/cpu", getCPUStats)
		e.Router.GET("/stats/ram", getRAMStats)

		return nil
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

func getCPUStats(c echo.Context) error {
	percentages, err := cpu.Percent(0, false)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if len(percentages) > 0 {
		return c.String(http.StatusOK, fmt.Sprintf("%.2f%%", percentages[0]))
	}
	return c.NoContent(http.StatusOK)
}

func getRAMStats(c echo.Context) error {
	vmStat, err := mem.VirtualMemory()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.String(http.StatusOK, fmt.Sprintf("%.2f%%", vmStat.UsedPercent))
}
