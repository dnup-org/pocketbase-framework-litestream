package handler

import (
	"io"
	"log"
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/models"

	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/webhook"
)

// @requires("authenticated")
// async def billing(request: Request):
//     subs = Subscriptions(request).list(filter="(active=true)")
//     sub = None
//     try:
//         sub = subs[0]
//     except (KeyError, TypeError, IndexError):
//         pass
//
//     if sub is None:
//         payment_link = stripe.PaymentLink.create(
//             line_items=[
//                 {
//                     "price": "price_1Otw4OIaQA2eZE1x60IFe7Ro",
//                     "quantity": 1,
//                 }
//             ],
//             payment_method_types=["card"],
//         )
//         return JSONResponse(
//             {
//                 "active": False,
//                 "subscribe": payment_link.url
//                 + f"?client_reference_id={request.user.id}&customer_email={request.user.username}",
//                 "cancel": None,
//                 "subs": subs,
//             }
//         )
//
//     try:
//         cancel = stripe.billing_portal.Session.create(
//             customer=subs[0]["customerId"],
//             flow_data={
//                 "type": "subscription_cancel",
//                 "subscription_cancel": {"subscription": subs[0]["stripeId"]},
//             },
//         ).url
//     except Exception:
//         cancel = None
//
//     manage = stripe.billing_portal.Session.create(
//         customer=subs[0]["customerId"],
//     )
//
//     return JSONResponse(
//         {
//             "active": True,
//             "subscribe": None,
//             "manage": manage.url,
//             "cancel": cancel,
//             "subs": subs,
//         }
//     )

// handleCheckout creates a Stripe checkout session using a pre-configured Stripe product
// Also fetches the current user email, and attaches it to metadata. This is used later
// To find which user is associated with subsequent webhook requests.
func (h *AppHandler) CheckoutHandler(c echo.Context) error {

	params := &stripe.CheckoutSessionParams{
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			&stripe.CheckoutSessionLineItemParams{
				Price:    stripe.String("price_1PB3AOJ43qYJCSJEGh5ibE22"),
				Quantity: stripe.Int64(1),
			},
		},
		Mode:         stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL:   stripe.String(DOMAIN_NAME + "/login"),
		CancelURL:    stripe.String(DOMAIN_NAME + "/get"),
		AutomaticTax: &stripe.CheckoutSessionAutomaticTaxParams{Enabled: stripe.Bool(true)},
	}

	record, _ := c.Get(apis.ContextAuthRecordKey).(*models.Record)
	params.AddMetadata("email", record.Email())

	s, err := session.New(params)

	if err != nil {
		log.Printf("session.New: %v", err)
	}

	return c.Redirect(http.StatusSeeOther, s.URL)
}

// handleWebhook is the handler that stripe's servers call. It contains the (pocketbase)
// email of the user as request metadata. This function activates the "paid" status of that email.
func (h *AppHandler) WebhookHandler(c echo.Context) error {
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
		record, _ := h.App.Dao().FindAuthRecordByEmail("users", email)
		record.Set("paid", true)
		if err := h.App.Dao().SaveRecord(record); err != nil {
			return err
		}
	}

	return c.String(http.StatusOK, "OK")
}
