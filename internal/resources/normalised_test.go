package resources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// A request leaf the record gives in a named normalised form is read as the form says, wherever a
// table exposes it: a list Stripe returns `sorted` keeps its configured order (PreserveConfig),
// and a currency map Stripe returns `with_own_currency` or `with_own_tax_behavior` leaves the
// object's own currency out (CurrencyOptions at the top level, OwnCurrency below it). The other
// names (`tiers`) say what Stripe returns of an element's member left out, which ServerDefaulted
// and NullAs already read.
func TestNormalisedFormsAreReadAsTheRecordNamesThem(t *testing.T) {
	var record struct {
		Leaves []struct {
			Object, Side, Operation, Path, Branch string
			Returned                              *struct {
				Form string `json:"form"`
				Name string `json:"name"`
			} `json:"returned"`
		} `json:"leaves"`
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "spec", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	var coverage struct {
		Leaves []struct {
			Table, Object, Side, Operation, Path, Branch, State string
		} `json:"leaves"`
	}
	if raw, err = os.ReadFile(filepath.Join("..", "generated", "coverage.json")); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatal(err)
	}
	exposed := map[string][]string{}
	for _, l := range coverage.Leaves {
		if l.State == "exposed" {
			key := strings.Join([]string{l.Object, l.Side, l.Operation, l.Path, l.Branch}, " ")
			exposed[key] = append(exposed[key], l.Table)
		}
	}
	quoted := regexp.MustCompile(`"([^"]+)"`)
	preserved := regexp.MustCompile(`PreserveConfig:\s*\[\]string\{([^}]*)\}`)
	ownCurrency := regexp.MustCompile(`OwnCurrency:\s*map\[string\]string\{([^}]*)\}`)
	ownKey := regexp.MustCompile(`"([^"]+)":`)
	currencyOptions := regexp.MustCompile(`CurrencyOptions:\s*"([^"]+)"`)
	declared := func(table string) (keep, maps []string) {
		src, err := os.ReadFile(filepath.Join(table, "resource.go"))
		if err != nil {
			t.Fatal(err)
		}
		if m := preserved.FindSubmatch(src); m != nil {
			for _, e := range quoted.FindAllSubmatch(m[1], -1) {
				keep = append(keep, string(e[1]))
			}
		}
		if m := ownCurrency.FindSubmatch(src); m != nil {
			for _, e := range ownKey.FindAllSubmatch(m[1], -1) {
				maps = append(maps, string(e[1]))
			}
		}
		if m := currencyOptions.FindSubmatch(src); m != nil {
			maps = append(maps, string(m[1]))
		}
		return keep, maps
	}
	checked := 0
	for _, l := range record.Leaves {
		if l.Returned == nil || l.Returned.Form != "normalised" || l.Returned.Name == "" {
			continue
		}
		for _, table := range exposed[strings.Join([]string{l.Object, l.Side, l.Operation, l.Path, l.Branch}, " ")] {
			keep, maps := declared(table)
			switch l.Returned.Name {
			case "sorted":
				checked++
				if !slices.Contains(keep, l.Path) {
					t.Errorf("%s: Stripe returns %s %s sorted: name it in PreserveConfig", table, l.Operation, l.Path)
				}
			case "with_own_currency", "with_own_tax_behavior":
				checked++
				at, _, _ := strings.Cut(l.Path, "currency_options")
				if !slices.Contains(maps, at+"currency_options") {
					t.Errorf("%s: Stripe returns %s %s with the object's own currency: name %scurrency_options in CurrencyOptions or OwnCurrency", table, l.Operation, l.Path, at)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no exposed leaf has a named normalised form; the record or the pattern no longer matches")
	}
}
