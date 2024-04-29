package handler

import (
	"encoding/json"
	"net/url"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/models"

)

// loadAuthContextFromCookie is an HTTP middleware that takes the Pocketbase auth token from the `pb_auth` cookie
// It then manually retrieves the auth state from this token, and places it in the echo context, accessible by HTTP handlers.
func (h *AppHandler) LoadAuthContextFromCookie() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tokenCookie, err := c.Request().Cookie("pb_auth")
			if err != nil {
				return next(c)
			}

			decodedCookie, err := url.QueryUnescape(tokenCookie.Value)
			if err != nil {
				return next(c)
			}

			var cookieObject map[string]interface{}
			err = json.Unmarshal([]byte(decodedCookie), &cookieObject)
			if err != nil {
				return next(c)
			}

			token := cookieObject["token"].(string)

			record, err := h.App.Dao().FindAuthRecordByToken(
				token,
				h.App.Settings().RecordAuthToken.Secret,
			)

			if err != nil {
				return next(c)
			}

			c.Set(apis.ContextAuthRecordKey, record)
			return next(c)
		}
	}
}

// authGuard checks whether the auth context is valid, and reroutes to login if not.
// used for ensuring an endpoint can only be accessed if authenticated.
func AuthGuard(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		record := c.Get(apis.ContextAuthRecordKey)

		if record == nil {
			return c.Redirect(302, "/login")
		}

		return next(c)
	}
}

// paidGuard is a middleware function that only allows an HTTP route to be
// accessed if the current user has the paid=true
func PaidGuard(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		record, ok := c.Get(apis.ContextAuthRecordKey).(*models.Record)
		if !ok {
			return c.Redirect(302, "/login")
		}

		if record.Get("paid") == false {
			return c.Redirect(302, "/get")
		}

		return next(c)
	}
}
