// main.go
package main

import (
	"backend/handler"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/tjarratt/babble"
)

type OAuthResponse struct {
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func init() {
	handler.SetupHandlerVars()
}

var app *pocketbase.PocketBase

func main() {

	app = pocketbase.New()
	appHandler := &handler.AppHandler{App: app}
	babbler := babble.NewBabbler()
	babbler.Count = 3

	var hooksDir string
	app.RootCmd.PersistentFlags().StringVar(
		&hooksDir,
		"hooksDir",
		"",
		"the directory with the JS app hooks",
	)

	var hooksWatch bool
	app.RootCmd.PersistentFlags().BoolVar(
		&hooksWatch,
		"hooksWatch",
		true,
		"auto restart the app on pb_hooks file change",
	)

	var hooksPool int
	app.RootCmd.PersistentFlags().IntVar(
		&hooksPool,
		"hooksPool",
		25,
		"the total prewarm goja.Runtime instances for the JS app hooks execution",
	)

	var migrationsDir string
	app.RootCmd.PersistentFlags().StringVar(
		&migrationsDir,
		"migrationsDir",
		"",
		"the directory with the user defined migrations",
	)

	var automigrate bool
	app.RootCmd.PersistentFlags().BoolVar(
		&automigrate,
		"automigrate",
		false,
		"enable/disable auto migrations",
	)

	app.RootCmd.ParseFlags(os.Args[1:])

	// load jsvm (hooks and migrations)
	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir: migrationsDir,
		HooksDir:      hooksDir,
		HooksWatch:    hooksWatch,
		HooksPoolSize: hooksPool,
	})

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

		e.Router.POST("/api/accept-invitation", func(c echo.Context) error {
			info := apis.RequestInfo(c)
			user_record := info.AuthRecord

			data := struct {
				Key string `json:"key" form:"key"`
			}{}
			if err := c.Bind(&data); err != nil {
				return apis.NewBadRequestError("Failed to read request data", err)
			}

			invitation, err := app.Dao().FindFirstRecordByData("relay_invitations", "key", data.Key)
			if err != nil {
				return err
			}

			relay_roles_collection, err := app.Dao().FindCollectionByNameOrId("relay_roles")
			if err != nil {
				return err
			}

			var total int
			err = app.Dao().RecordQuery(relay_roles_collection).
				Select("count(*)").
				AndWhere(dbx.HashExp{"relay": invitation.GetString("relay")}).
				AndWhere(dbx.HashExp{"role": invitation.GetString("role")}).
				AndWhere(dbx.HashExp{"user": user_record.Id}).
				Row(&total)

			if err != nil || total == 0 {
				relay_role := models.NewRecord(relay_roles_collection)
				relay_role.Set("relay", invitation.Get("relay"))
				relay_role.Set("role", invitation.Get("role"))
				relay_role.Set("user", user_record.Id)
				if err := app.Dao().SaveRecord(relay_role); err != nil {
					return err
				}
			}
			relay, err := app.Dao().FindRecordById("relays", invitation.GetString("relay"))
			if err != nil {
				return err
			}

			return c.JSON(http.StatusOK, relay)
		}, handler.AuthGuard)

		return nil
	})

	app.OnRecordBeforeCreateRequest("oauth2_response").Add(func(e *core.RecordCreateEvent) error {
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}
		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord

		var oauth_response OAuthResponse
		oauth_response_string := e.Record.GetString("oauth_response")
		err := json.Unmarshal([]byte(oauth_response_string), &oauth_response)
		if err != nil {
			return err
		}

		user_record.Set("name", oauth_response.Name)
		user_record.Set("picture", oauth_response.Picture)

		if err := app.Dao().SaveRecord(user_record); err != nil {
			return err
		}
		return nil
	})

	app.OnRecordBeforeCreateRequest("relays").Add(func(e *core.RecordCreateEvent) error {
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}

		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord
		relay_roles_collection, err := app.Dao().FindCollectionByNameOrId("relay_roles")
		if err != nil {
			return err
		}
		owner_role, err := app.Dao().FindFirstRecordByData("roles", "name", "Owner")
		if err != nil {
			return fmt.Errorf("error finding role: %v", err)
		}

		var total int

		// The transaction has started, so expect to find the current record in the count.
		err = app.Dao().RecordQuery(relay_roles_collection).
			Select("count(*)").
			AndWhere(dbx.HashExp{"role": owner_role.Id}).
			AndWhere(dbx.HashExp{"user": user_record.Id}).
			Row(&total)

		if err != nil || total >= 25 {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Errorf("you can only be an Owner of 25 relays"))
		}

		// Set Defaults
		e.Record.Set("user_limit", 2)
		e.Record.Set("creator", user_record.Id)
		return nil
	})

	app.OnRecordBeforeCreateRequest("relay_roles").Add(func(e *core.RecordCreateEvent) error {
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}
		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord
		relay, err := app.Dao().FindFirstRecordByData("relays", "id", e.Record.GetString("relay"))
		if err != nil {
			return err
		}

		var total int

		// The transaction has started, so expect to find the current record in the count.
		err = app.Dao().RecordQuery(e.Record.Collection()).
			Select("count(*)").
			AndWhere(dbx.HashExp{"relay": e.Record.GetString("relay")}).
			AndWhere(dbx.HashExp{"user": user_record.Id}).
			Row(&total)

		if err != nil || total >= relay.GetInt("user_limit") {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Errorf("you have exceeded the user limit for this relay"))
		}

		return nil
	})

	app.OnRecordAfterCreateRequest("relays").Add(func(e *core.RecordCreateEvent) error {
		// Create a relay_role such that the creator of the relay is now the owner.
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}
		user_record := e.HttpContext.Get(apis.ContextRequestInfoKey).(*models.RequestInfo).AuthRecord

		owner_role, err := app.Dao().FindFirstRecordByData("roles", "name", "Owner")
		if err != nil {
			return fmt.Errorf("error finding role: %v", err)
		}

		relay_roles_collection, err := app.Dao().FindCollectionByNameOrId("relay_roles")
		if err != nil {
			return err
		}

		relay_role := models.NewRecord(relay_roles_collection)
		relay_role.Set("relay", e.Record.Id)
		relay_role.Set("user", user_record.Id)
		relay_role.Set("role", owner_role.Id)

		if err := app.Dao().SaveRecord(relay_role); err != nil {
			return err
		}

		return nil
	})

	app.OnRecordAfterCreateRequest("relays").Add(func(e *core.RecordCreateEvent) error {
		// Create a relay_role such that the creator of the relay is now the owner.
		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}

		member_role, err := app.Dao().FindFirstRecordByData("roles", "name", "Member")
		if err != nil {
			return fmt.Errorf("error finding role: %v", err)
		}

		relay_invitations_collection, err := app.Dao().FindCollectionByNameOrId("relay_invitations")
		if err != nil {
			return err
		}

		relay_invitation := models.NewRecord(relay_invitations_collection)
		relay_invitation.Set("relay", e.Record.Id)
		relay_invitation.Set("role", member_role.Id)

		key := babbler.Babble()
		key = strings.Replace(key, "'s", "", -1)
		key = strings.Replace(key, "'", "", -1)
		key = strings.ToLower(key)

		relay_invitation.Set("key", key)

		if err := app.Dao().SaveRecord(relay_invitation); err != nil {
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
