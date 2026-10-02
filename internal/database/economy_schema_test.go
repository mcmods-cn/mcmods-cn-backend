package database

import (
	"strings"
	"testing"
	"unicode"
)

func TestEconomySchemaConstrainsMonetaryRangesAndConservation(t *testing.T) {
	definition := strings.ToLower(strings.Join(append(communitySchemaStatements(), communityPostSchemaStatements()...), "\n"))
	definition = strings.Map(func(character rune) rune {
		if unicode.IsSpace(character) {
			return -1
		}
		return character
	}, definition)
	for _, required := range []string{
		"balancebigintnotnulldefault0check(balancebetween0and100000000000000)",
		"amount_deltabigintnotnullcheck(amount_deltabetween-100000000000000and100000000000000)",
		"price_amountbigintnotnulldefault0check(price_amountbetween0and1000000000000)",
		"check(total_price=unit_price*quantity::bigint)",
		"amountbigintnotnullcheck(amountbetween1and1000000000000)",
		"check(status<>'awarded'ortax_amount+net_amount=amount)",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("economy schema is missing invariant %q", required)
		}
	}
}
