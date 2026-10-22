package tfacc

import (
	"testing"

	"github.com/computomatic/terraform-provider-stripe/internal/acctest"
	"github.com/computomatic/terraform-provider-stripe/internal/wire"
)

// headOfficeAddress is the suite's head office address, where no registration makes tax due.
func headOfficeAddress() wire.Object {
	return wire.Object{
		{Name: "line1", Value: wire.String("354 Oyster Point Blvd")},
		{Name: "city", Value: wire.String("South San Francisco")},
		{Name: "state", Value: wire.String("CA")},
		{Name: "postal_code", Value: wire.String("94080")},
		{Name: "country", Value: wire.String("US")},
	}
}

// TaxTransaction records a tax transaction, with the reference given, from a calculation of
// two line items of 1000 and 2500 at the head office address, which it gives the account
// first (suite: "createTaxTransaction"). Each line item carries the general goods tax code,
// because an account's Tax settings may have no default ("Tax calculations need a tax code").
// It returns the transaction's ID.
func TaxTransaction(t *testing.T, c acctest.Config, reference string) string {
	t.Helper()
	Post(t, c, "/v1/tax/settings", wire.Object{{Name: "head_office", Value: wire.Object{{Name: "address", Value: headOfficeAddress()}}}})
	item := func(reference string, amount int64) wire.Object {
		return wire.Object{
			{Name: "reference", Value: wire.String(reference)},
			{Name: "amount", Value: wire.Int(amount)},
			{Name: "tax_code", Value: wire.String("txcd_99999999")},
		}
	}
	calculation := Create(t, c, "/v1/tax/calculations", wire.Object{
		{Name: "currency", Value: wire.String(AccountCurrency(t, c))},
		{Name: "customer_details", Value: wire.Object{
			{Name: "address", Value: headOfficeAddress()},
			{Name: "address_source", Value: wire.String("shipping")},
		}},
		{Name: "line_items", Value: wire.Array{item("tfacc-a", 1000), item("tfacc-b", 2500)}},
	})
	return Create(t, c, "/v1/tax/transactions/create_from_calculation", wire.Object{
		{Name: "calculation", Value: wire.String(calculation)},
		{Name: "reference", Value: wire.String(reference)},
	})
}
