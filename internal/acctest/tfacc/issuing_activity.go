package tfacc

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/stripeapi"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

const (
	// logoPurpose is the file purpose of a personalization design's card logo.
	logoPurpose = "issuing_logo"
	// logoFilename names the one card logo the harness uploads to a connected account. Files
	// take no metadata and Stripe never deletes one, so smoke runs find it again by its purpose
	// and this name.
	logoFilename = "tfacc-issuing-logo.png"
	// cardFunding is the received credit, in cents, that funds a card's authorizations.
	cardFunding = 20000
)

// CardLogo returns the ID of a card logo file on the connected account conn names: a 1000 by
// 200 black and white PNG with the purpose `issuing_logo`, which a personalization design on a
// bundle that takes a logo needs (suite: "Issuing personalization designs (first run)").
// Terraform cannot upload a file, so the harness does. Against the fake every test's account is
// fresh, so each test uploads its own; in smoke mode the logo is uploaded once and reused.
func CardLogo(t *testing.T, conn acctest.Config) string {
	t.Helper()
	if conn.Smoke {
		resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/files", Params: wire.Object{
			{Name: "purpose", Value: wire.String(logoPurpose)},
			{Name: "limit", Value: wire.Int(100)},
		}})
		if err != nil {
			t.Fatalf("GET /v1/files: %v", err)
		}
		data, _ := resp.Body.(wire.Object).Get("data")
		items, _ := data.(wire.Array)
		for _, item := range items {
			file, _ := item.(wire.Object)
			if name, _ := file.GetString("filename"); name == logoFilename {
				id, _ := file.GetString("id")
				return id
			}
		}
	}
	return upload(t, conn, logoPurpose, logoFilename, cardLogoPNG(t))
}

// cardLogoPNG is a card logo of the kind Stripe documents for Issuing: 1000 by 200 pixels of
// black on white only, which a two-colour palette makes a one-bit image.
func cardLogoPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 1000, 200), color.Palette{color.White, color.Black})
	for y := 60; y < 140; y++ {
		for x := 100; x < 900; x++ {
			img.SetColorIndex(x, y, 1)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the card logo: %v", err)
	}
	return buf.Bytes()
}

// upload sends `POST /v1/files` to the Files API host: Stripe's in smoke mode, the fake's own
// port otherwise. It returns the file's ID.
func upload(t *testing.T, c acctest.Config, purpose, filename string, content []byte) string {
	t.Helper()
	resp, err := Client(c).Do(t.Context(), stripeapi.Request{
		Method: "POST", Path: "/v1/files", Host: stripeapi.FilesHost,
		Params: wire.Object{{Name: "purpose", Value: wire.String(purpose)}},
		File:   &stripeapi.File{Part: "file", Name: filename, Content: content},
	})
	if err != nil {
		t.Fatalf("POST /v1/files: %v", err)
	}
	id, _ := resp.Body.(wire.Object).GetString("id")
	return id
}

// PhysicalBundle returns the ID of an active physical bundle of the connected account conn
// names that takes a card logo and carrier text, which the tests' designs set. Stripe makes the
// bundles; none can be created (suite: "Issuing physical bundles on a US test-mode connected
// account (first run)"). A connected account without one skips the test.
func PhysicalBundle(t *testing.T, conn acctest.Config) string {
	t.Helper()
	resp, err := Client(conn).Do(t.Context(), stripeapi.Request{Method: "GET", Path: "/v1/issuing/physical_bundles", Params: wire.Object{
		{Name: "status", Value: wire.String("active")},
		{Name: "limit", Value: wire.Int(100)},
	}})
	if err != nil {
		t.Fatalf("GET /v1/issuing/physical_bundles: %v", err)
	}
	data, _ := resp.Body.(wire.Object).Get("data")
	items, _ := data.(wire.Array)
	for _, item := range items {
		bundle, _ := item.(wire.Object)
		features, _ := bundle.GetObject("features")
		logo, _ := features.GetString("card_logo")
		carrierText, _ := features.GetString("carrier_text")
		if logo != "unsupported" && carrierText != "unsupported" {
			id, _ := bundle.GetString("id")
			return id
		}
	}
	t.Skipf("connected account %s has no active physical bundle that takes a card logo and carrier text", conn.StripeAccount)
	return ""
}

// ActiveCard returns an active virtual card, and its cardholder, that draws on the financial
// account, and funds the account so that authorizations on the card are approved (suite:
// "activeCard", "ensureFunds"). The cardholder carries the details Stripe needs to activate a
// card, and the card is created once Stripe has reviewed it.
func ActiveCard(t *testing.T, conn acctest.Config, financialAccount string) (card, cardholder string) {
	t.Helper()
	ReceivedCredit(t, conn, financialAccount, cardFunding)
	cardholder = Create(t, conn, "/v1/issuing/cardholders", wire.Object{
		{Name: "type", Value: wire.String("individual")},
		{Name: "name", Value: wire.String("Jenny Rosen")},
		{Name: "email", Value: wire.String(conn.Name("jenny-card") + "@example.com")},
		{Name: "phone_number", Value: wire.String("+18008675309")},
		{Name: "individual", Value: wire.Object{
			{Name: "first_name", Value: wire.String("Jenny")},
			{Name: "last_name", Value: wire.String("Rosen")},
			{Name: "dob", Value: wire.Object{{Name: "day", Value: wire.Int(1)}, {Name: "month", Value: wire.Int(1)}, {Name: "year", Value: wire.Int(1990)}}},
			{Name: "card_issuing", Value: wire.Object{{Name: "user_terms_acceptance", Value: wire.Object{
				{Name: "date", Value: wire.Int(1609459200)},
				{Name: "ip", Value: wire.String("8.8.8.8")},
			}}}},
		}},
		{Name: "billing", Value: wire.Object{{Name: "address", Value: wire.Object{
			{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
			{Name: "city", Value: wire.String("South San Francisco")},
			{Name: "state", Value: wire.String("CA")},
			{Name: "postal_code", Value: wire.String("94080")},
			{Name: "country", Value: wire.String("US")},
		}}}},
	})
	AwaitCardholderReview(t, conn, cardholder)
	card = Create(t, conn, "/v1/issuing/cards", wire.Object{
		{Name: "cardholder", Value: wire.String(cardholder)},
		{Name: "currency", Value: wire.String("usd")},
		{Name: "type", Value: wire.String("virtual")},
		{Name: "financial_account", Value: wire.String(financialAccount)},
		{Name: "status", Value: wire.String("active")},
	})
	return card, cardholder
}

// Authorization makes an approved authorization of amount cents on the card with Stripe's test
// helper and returns its ID. It is `pending` until it is captured or reversed (suite: "create
// with the test helper").
func Authorization(t *testing.T, conn acctest.Config, card string, amount int64) string {
	t.Helper()
	return Create(t, conn, "/v1/test_helpers/issuing/authorizations", wire.Object{
		{Name: "card", Value: wire.String(card)},
		{Name: "amount", Value: wire.Int(amount)},
	})
}

// CapturedTransaction authorizes amount cents on the card, captures the authorization in full
// with Stripe's test helper and returns the authorization's ID and the capture transaction's
// (suite: "capturedTransaction"). Stripe may list the transaction a moment after the capture.
func CapturedTransaction(t *testing.T, conn acctest.Config, card string, amount int64) (authorization, transaction string) {
	t.Helper()
	authorization = Authorization(t, conn, card, amount)
	Post(t, conn, "/v1/test_helpers/issuing/authorizations/"+authorization+"/capture", nil)
	if !poll(func() bool {
		v, _ := get(t, conn, "/v1/issuing/authorizations/"+authorization).Get("transactions")
		txns, _ := v.(wire.Array)
		if len(txns) == 0 {
			return false
		}
		transaction, _ = txns[0].(wire.Object).GetString("id")
		return true
	}) {
		t.Fatalf("authorization %s has no transaction %s after its capture", authorization, activationLimit)
	}
	return authorization, transaction
}
