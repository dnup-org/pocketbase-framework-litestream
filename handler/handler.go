// handler/handler.go

package handler

import (
	"github.com/pocketbase/pocketbase"
	"github.com/stripe/stripe-go/v76"
	"os"
)

var STRIPE_WEBHOOK_SECRET string
var DOMAIN_NAME string

func SetupHandlerVars() {
	STRIPE_WEBHOOK_SECRET = os.Getenv("STRIPE_WEBHOOK_SECRET")
	DOMAIN_NAME = os.Getenv("DOMAIN_NAME")
	stripe.Key = os.Getenv("STRIPE_SECRET_KEY")
}

type AppHandler struct {
	App *pocketbase.PocketBase
}
