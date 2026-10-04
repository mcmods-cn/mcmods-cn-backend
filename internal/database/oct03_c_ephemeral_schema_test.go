package database

import (
	"regexp"
	"strings"
	"testing"
)

// This oracle preserves the previous ordered transformation. Compiling its
// patterns once changes no output and avoids repeating the old setup cost.
func oct03COriginalFunctionQualifier(namespace string, names []string) func(string) string {
	patterns := make([]*regexp.Regexp, len(names))
	for i, name := range names {
		patterns[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*\(`)
	}
	return func(statement string) string {
		for i, name := range names {
			statement = patterns[i].ReplaceAllString(statement, namespace+"."+name+"(")
		}
		return statement
	}
}

func TestOCT03CEphemeralFunctionQualificationMatchesEveryCurrentSchemaStatement(t *testing.T) {
	statements := schemaInstallationStatements()
	names := ephemeralSchemaFunctionNames(statements)
	if len(names) == 0 || len(statements) == 0 {
		t.Fatal("the real schema statements and functions must be present")
	}
	const namespace = "pg_temp_314159"
	previous := oct03COriginalFunctionQualifier(namespace, names)
	current := ephemeralSchemaFunctionQualifier(namespace, names)
	for i, raw := range statements {
		statement := ephemeralTableDeclarationPattern.ReplaceAllString(raw, "create temporary table")
		statement = strings.ReplaceAll(statement, "on public.%I", "on %I")
		statement = strings.ReplaceAll(statement, "namespace_row.nspname='public'", "namespace_row.oid=pg_my_temp_schema()")
		want, got := previous(statement), current(statement)
		if got != want {
			t.Fatalf("schema statement %d qualification differs: got %d bytes, want %d bytes", i, len(got), len(want))
		}
	}
	t.Logf("all %d actual schema statements preserve ordered qualification for %d functions byte-for-byte", len(statements), len(names))
}

func TestOCT03CEphemeralFunctionQualificationPreservesBoundariesAndCanonicalization(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		functions []string
		statement string
	}{
		{"case and whitespace", []string{"foo", "foo_bar"}, "CREATE FUNCTION FOO \n\t( ); select FOO_BAR ( ), foo( ), foo\r\n( )"},
		{"word boundaries and qualification", []string{"foo", "foo_bar"}, "select other_foo(), 1foo(), foo_bar(), public.foo(), pg_temp_3.foo()"},
		{"repeated and quoted body", []string{"foo"}, "do $$ begin perform foo(); execute 'select FOO ( )'; end $$"},
		{"unrelated statement", []string{"foo"}, "select unrelated(1), fooish(2)"},
		{"no function names", nil, "select foo()"},
		{"unicode folding and boundaries", []string{"ask", "starts"}, "select aſk(), ſtarts(), xſtarts(), aKk()"},
		{"unicode function names", []string{"ask", "aſk"}, "select ASK(), ask(), aſk()"},
		{"duplicate folded names", []string{"foo", "FOO"}, "select foo(), FOO ( )"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			previous := oct03COriginalFunctionQualifier("pg_temp_17", scenario.functions)
			current := ephemeralSchemaFunctionQualifier("pg_temp_17", scenario.functions)
			want, got := previous(scenario.statement), current(scenario.statement)
			if got != want {
				t.Fatalf("ordered qualification changed: got %q, want %q", got, want)
			}
		})
	}
}
