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

	"io"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/forms"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/plugins/jsvm"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/tjarratt/babble"

	"github.com/labstack/echo/v5"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/subscription"
	"github.com/stripe/stripe-go/v76/webhook"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"backend/myh2c"
	"backend/redirect"
)

type OAuthResponse struct {
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

var STRIPE_WEBHOOK_SECRET string
var DOMAIN_NAME string

const FREE_USER_LIMIT = 3

func init() {
	STRIPE_WEBHOOK_SECRET = os.Getenv("STRIPE_WEBHOOK_SECRET")
	DOMAIN_NAME = os.Getenv("DOMAIN_NAME")
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
	prometheus.MustRegister(httpRequestsTotal)
}

var (
	httpRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint"},
	)
)

func logMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		req := c.Request()
		fmt.Printf("Method: %s\n", req.Method)
		fmt.Printf("URL: %s\n", req.URL)
		fmt.Printf("Protocol: %s\n", req.Proto)
		fmt.Println("Headers:")
		for name, headers := range req.Header {
			for _, h := range headers {
				if name == "Authorization" {
					fmt.Printf("  %v: %v\n", name, "**************")
					continue
				}
				fmt.Printf("  %v: %v\n", name, h)
			}
		}
		fmt.Println("---")

		return next(c)
	}
}

var app *pocketbase.PocketBase

func main() {

	app = pocketbase.New()

	// loosely check if it was executed using "go run"
	isGoRun := strings.HasPrefix(os.Args[0], os.TempDir())

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		// enable auto creation of migration files when making collection changes in the Admin UI
		// (the isGoRun check is to enable it only during development)
		Automigrate: isGoRun,
	})

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

	app.RootCmd.AddCommand(myh2c.NewServeH2CCommand(app, true))

	app.RootCmd.ParseFlags(os.Args[1:])

	// load jsvm (hooks and migrations)
	jsvm.MustRegister(app, jsvm.Config{
		MigrationsDir: migrationsDir,
		HooksDir:      hooksDir,
		HooksWatch:    hooksWatch,
		HooksPoolSize: hooksPool,
	})

	go func() {
		metricsServer := &http.Server{
			Addr:    ":9091",
			Handler: promhttp.Handler(),
		}
		log.Printf("Starting metrics server on :9091")
		if err := metricsServer.ListenAndServe(); err != nil {
			log.Printf("Metrics server error: %v", err)
		}
	}()

	app.OnBeforeServe().Add(func(e *core.ServeEvent) error {

		group := e.Router.Group("/api")
		redirect.BindRecordAuthApi(app, group)

		// Load auth state from cookie
		e.Router.Use(appHandler.LoadAuthContextFromCookie())
		e.Router.Use(logMiddleware)

		e.Router.POST("/webhook", WebhookHandler)
		e.Router.POST("/api/cancel-subscription", handleCancelSubscription, handler.AuthGuard)

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
				form := forms.NewRecordUpsert(app, relay_role)

				form.LoadData(map[string]any{
					"relay": invitation.Get("relay"),
					"role":  invitation.Get("role"),
					"user":  user_record.Id,
				})
				if err := form.Submit(); err != nil {
					return err
				}
			}
			relay, err := app.Dao().FindRecordById("relays", invitation.GetString("relay"))
			if err != nil {
				return err
			}
			if errs := app.Dao().ExpandRecord(relay, []string{"relay_roles_via_relay.user", "shared_folders_via_relay"}, nil); len(errs) > 0 {
				return fmt.Errorf("failed to expand: %v", errs)
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
		e.Record.Set("user_limit", FREE_USER_LIMIT)
		e.Record.Set("creator", user_record.Id)
		return nil
	})

	app.OnRecordBeforeCreateRequest("relay_roles").Add(func(e *core.RecordCreateEvent) error {
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

		if err != nil || total > relay.GetInt("user_limit") {
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

	app.OnRecordBeforeCreateRequest("code_exchange").Add(func(e *core.RecordCreateEvent) error {

		admin, _ := e.HttpContext.Get(apis.ContextAdminKey).(*models.Admin)
		if admin != nil {
			return nil // ignore for admins
		}

		e.Record.Set("code", 0)
		e.Record.Set("state", "")
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

	app.OnRecordBeforeDeleteRequest("relays").Add(func(e *core.RecordDeleteEvent) error {
		// Check if there's an active subscription for this relay
		subscription, err := app.Dao().FindFirstRecordByFilter("subscriptions", "relay={:relay} && active=true", dbx.Params{
			"relay": e.Record.Id,
		})

		if err == nil && subscription != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Errorf("cannot delete relay with active subscription"))
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

func WebhookHandler(c echo.Context) error {
	const maxBytes = int64(65536)
	limitedReader := http.MaxBytesReader(c.Response().Writer, c.Request().Body, maxBytes)

	payload, err := io.ReadAll(limitedReader)
	if err != nil {
		return err
	}

	event, err := webhook.ConstructEvent(payload, c.Request().Header.Get("Stripe-Signature"), STRIPE_WEBHOOK_SECRET)
	if err != nil {
		return err
	}

	if event.Type == "checkout.session.completed" {
		// Successful payment. Retrieve user by email and update paid status in database.
		metadata, _ := event.Data.Object["metadata"].(map[string]interface{})
		email, _ := metadata["email"].(string)
		relay_id, _ := metadata["relay"].(string)
		quantity_string, _ := metadata["quantity"].(string)
		quantity, _ := strconv.Atoi(quantity_string)
		relay, err := app.Dao().FindRecordById("relays", relay_id)
		if err != nil {
			return err
		}

		relay.Set("user_limit", quantity)
		if err := app.App.Dao().SaveRecord(relay); err != nil {
			return err
		}
		user, err := app.App.Dao().FindAuthRecordByEmail("users", email)
		if err != nil {
			user.Set("paid", true)
			if err := app.App.Dao().SaveRecord(user); err != nil {
				return err
			}
		}

		subcriptions_collection, err := app.Dao().FindCollectionByNameOrId("subscriptions")
		if err != nil {
			return err
		}
		record := models.NewRecord(subcriptions_collection)
		record.Set("active", true)
		record.Set("user", user.Id)
		record.Set("relay", relay.Id)
		record.Set("stripe_quantity", quantity)
		record.Set("stripe_customer", event.Data.Object["customer"])
		record.Set("stripe_subscription", event.Data.Object["subscription"])
		app.Dao().SaveRecord(record)
	} else if event.Type == "customer.subscription.updated" {
		subscription, err := app.Dao().FindFirstRecordByData("subscriptions", "stripe_subscription", event.Data.Object["id"])
		if err != nil {
			return err
		}
		subscription.Set("active", event.Data.Object["status"] == "active")
		subscription.Set("stripe_cancel_at", event.Data.Object["cancel_at"])
		subscription.Set("stripe_quantity", event.Data.Object["quantity"])
		if err := app.Dao().SaveRecord(subscription); err != nil {
			return err
		}
		relayId := subscription.GetString("relay")
		relay, err := app.Dao().FindRecordById("relays", relayId)
		if err != nil {
			return err
		}
		relay.Set("user_limit", event.Data.Object["quantity"])
		if err := app.Dao().SaveRecord(relay); err != nil {
			return err
		}
	} else if event.Type == "customer.subscription.deleted" {
		subscription, err := app.Dao().FindFirstRecordByData("subscriptions", "stripe_subscription", event.Data.Object["id"])
		if err != nil {
			return err
		}
		relayId := subscription.GetString("relay")
		if err := app.Dao().DeleteRecord(subscription); err != nil {
			return err
		}
		relay, err := app.Dao().FindRecordById("relays", relayId)
		if err != nil {
			return err
		}
		relay.Set("user_limit", FREE_USER_LIMIT)
		if err := app.Dao().SaveRecord(relay); err != nil {
			return err
		}
	}
	return c.String(http.StatusOK, "OK")
}

func handleCancelSubscription(c echo.Context) error {
	info := apis.RequestInfo(c)
	user := info.AuthRecord

	var req struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
	}

	subscription_record, err := app.Dao().FindRecordById("subscriptions", req.SubscriptionID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Subscription not found"})
	}

	if subscription_record.GetString("user") != user.Id {
		return c.JSON(http.StatusForbidden, map[string]string{"error": "Not authorized to cancel this subscription"})
	}

	stripeSubscriptionID := subscription_record.GetString("stripe_subscription")

	stripeSubscription, err := subscription.Cancel(stripeSubscriptionID, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to cancel subscription"})
	}

	// Update subscription record
	subscription_record.Set("active", false)
	subscription_record.Set("stripe_cancel_at", stripeSubscription.CanceledAt)
	if err := app.Dao().SaveRecord(subscription_record); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update subscription_record record"})
	}

	// Expand the subscription_record record to include related data if needed
	if err := app.Dao().ExpandRecord(subscription_record, []string{"user", "relay"}, nil); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to expand subscription_record record"})
	}

	// Return the updated subscription_record object
	return c.JSON(http.StatusOK, subscription_record)
}
