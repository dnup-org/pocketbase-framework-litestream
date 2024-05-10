// main.go
package main

import (
	"backend/handler"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"

	"github.com/pocketbase/pocketbase/models"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

func init() {
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

	app.OnRecordBeforeCreateRequest("workspaces").Add(func(e *core.RecordCreateEvent) error {
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}

		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord
		workspace_roles_collection, err := app.Dao().FindCollectionByNameOrId("workspace_roles")
		if err != nil {
			return err
		}
		owner_role, err := app.Dao().FindFirstRecordByData("roles", "name", "Owner")
		if err != nil {
			return fmt.Errorf("error finding role: %v", err)
		}

		var total int

		// The transaction has started, so expect to find the current record in the count.
		err = app.Dao().RecordQuery(workspace_roles_collection).
			Select("count(*)").
			AndWhere(dbx.HashExp{"role": owner_role.Id}).
			AndWhere(dbx.HashExp{"user": user_record.Id}).
			Row(&total)

		if err != nil || total >= 25 {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Errorf("you can only be an Owner of 25 workspaces"))
		}

		// Set Defaults
		e.Record.Set("user_limit", 2)
		return nil
	})

	app.OnRecordBeforeCreateRequest("workspace_roles").Add(func(e *core.RecordCreateEvent) error {
		//admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		//if admin != nil {
		//	return nil // ignore for admins
		//}
		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord
		workspace, err := app.Dao().FindFirstRecordByData("workspaces", "id", e.Record.GetString("workspace"))
		if err != nil {
			return err
		}

		var total int

		// The transaction has started, so expect to find the current record in the count.
		err = app.Dao().RecordQuery(e.Record.Collection()).
			Select("count(*)").
			AndWhere(dbx.HashExp{"workspace": e.Record.GetString("workspace")}).
			AndWhere(dbx.HashExp{"user": user_record.Id}).
			Row(&total)

		if err != nil || total >= workspace.GetInt("user_limit") {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Errorf("you have exceeded the user limit for this workspace"))
		}

		return nil
	})

	app.OnRecordAfterCreateRequest("workspaces").Add(func(e *core.RecordCreateEvent) error {
		// Create a workspace_role such that the creator of the workspace is now the owner.
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}
		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord

		owner_role, err := app.Dao().FindFirstRecordByData("roles", "name", "Owner")
		if err != nil {
			return fmt.Errorf("error finding role: %v", err)
		}

		workspace_roles_collection, err := app.Dao().FindCollectionByNameOrId("workspace_roles")
		if err != nil {
			return err
		}

		workspace_role := models.NewRecord(workspace_roles_collection)
		workspace_role.Set("workspace", e.Record.Id)
		workspace_role.Set("user", user_record.Id)
		workspace_role.Set("role", owner_role.Id)

		if err := app.Dao().SaveRecord(workspace_role); err != nil {
			return err
		}

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
