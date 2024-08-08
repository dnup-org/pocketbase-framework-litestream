package redirect

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/models"
	"github.com/pocketbase/pocketbase/tools/subscriptions"
)

func BindRecordAuthApi(app core.App, rg *echo.Group) {
	api := recordAuthApi{app: app}

	// global oauth2 subscription redirect handler
	rg.GET("/oauth2-redirect", api.oauth2SubscriptionRedirect)
	rg.POST("/oauth2-redirect", api.oauth2SubscriptionRedirect) // needed in case of response_mode=form_post
}

type recordAuthApi struct {
	app core.App
}

const (
	oauth2SubscriptionTopic   string = "@oauth2"
	oauth2RedirectFailurePath string = "../_/#/auth/oauth2-redirect-failure"
	oauth2RedirectSuccessPath string = "../_/#/auth/oauth2-redirect-success"
)

type oauth2RedirectData struct {
	State string `form:"state" query:"state" json:"state"`
	Code  string `form:"code" query:"code" json:"code"`
	Error string `form:"error" query:"error" json:"error,omitempty"`
}

func (api *recordAuthApi) oauth2SubscriptionRedirect(c echo.Context) error {
	redirectStatusCode := http.StatusTemporaryRedirect
	if c.Request().Method != http.MethodGet {
		redirectStatusCode = http.StatusSeeOther
	}

	data := oauth2RedirectData{}
	if err := c.Bind(&data); err != nil {
		api.app.Logger().Debug("Failed to read OAuth2 redirect data", "error", err)
		return c.Redirect(redirectStatusCode, oauth2RedirectFailurePath)
	}

	if data.State == "" {
		api.app.Logger().Debug("Missing OAuth2 state parameter")
		return c.Redirect(redirectStatusCode, oauth2RedirectFailurePath)
	}

	encodedData, err := json.Marshal(data)
	if err != nil {
		api.app.Logger().Debug("Failed to marshalize OAuth2 redirect data", "error", err)
		return c.Redirect(redirectStatusCode, oauth2RedirectFailurePath)
	}

	// Start a goroutine to handle the retry mechanism
	go func() {
		retryDuration := time.Minute
		retryInterval := 2 * time.Second
		startTime := time.Now()
		retry_number := 0
		saved := false

		for time.Since(startTime) < retryDuration {
			client, err := api.app.SubscriptionsBroker().ClientById(data.State)
			if err == nil && !client.IsDiscarded() && client.HasSubscription(oauth2SubscriptionTopic) {
				msg := subscriptions.Message{
					Name: oauth2SubscriptionTopic,
					Data: encodedData,
				}
				client.Send(msg)
				client.Unsubscribe(oauth2SubscriptionTopic)
				api.app.Logger().Debug("Successfully sent OAuth2 subscription message", "clientId", data.State, "retry_number", retry_number)
				return
			}
			if !saved {
				// If we couldn't find a valid client, save the data to the database to support polling
				if err := api.saveCodeExchangeRecord(data); err != nil {
					api.app.Logger().Error("Failed to save code exchange record", "error", err)
				}
				saved = true
			}
			retry_number += 1

			// keep trying in case they reconnect
			time.Sleep(retryInterval)
		}

		api.app.Logger().Debug("Failed to find valid OAuth2 subscription client after retrying", "clientId", data.State)
	}()

	if data.Error != "" || data.Code == "" {
		api.app.Logger().Debug("Failed OAuth2 redirect due to an error or missing code parameter", "error", data.Error, "clientId", data.State)
		return c.Redirect(redirectStatusCode, oauth2RedirectFailurePath)
	}

	return c.Redirect(redirectStatusCode, oauth2RedirectSuccessPath)
}

func (api *recordAuthApi) saveCodeExchangeRecord(data oauth2RedirectData) error {
	codeExchangeCollection, err := api.app.Dao().FindCollectionByNameOrId("code_exchange")
	if err != nil {
		return err
	}
	codeExchangeRecord := models.NewRecord(codeExchangeCollection)
	codeExchangeRecord.Set("id", data.State[:15])
	codeExchangeRecord.Set("state", data.State)
	codeExchangeRecord.Set("code", data.Code)
	codeExchangeRecord.Set("otp", 123456) // Note: You might want to generate this dynamically
	return api.app.Dao().SaveRecord(codeExchangeRecord)
}
