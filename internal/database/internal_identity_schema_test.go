package database

import (
	"regexp"
	"strings"
	"testing"
)

func TestSchemaDoesNotStorePublicIDsAsInternalRelations(t *testing.T) {
	t.Parallel()
	groups := [][]string{
		baselineSchemaStatements(), catalogSchemaStatements(), catalogEditorSchemaStatements(),
		skinSchemaStatements(), reviewSchemaStatements(), blueprintRelationSchemaStatements(),
		communitySchemaStatements(), projectFileSchemaStatements(), modContentSchemaStatements(),
		commentSchemaStatements(), communityPostSchemaStatements(), modpackSchemaStatements(),
		simpleProjectSchemaStatements(), draftSchemaStatements(), serverSchemaStatements(),
	}
	forbidden := regexp.MustCompile(`(?i)\b[a-z][a-z0-9_]+_public_id\s+(text|varchar|bigint)\b`)
	for _, statements := range groups {
		for _, statement := range statements {
			if field := forbidden.FindString(statement); field != "" {
				t.Fatalf("schema stores public ID as an internal relation: %s", field)
			}
		}
	}
}

func TestDraftReviewTargetUsesNumericRelation(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(draftSchemaStatements(), "\n"))
	if !strings.Contains(definition, "review_target_id bigint references minecraft_servers(id)") {
		t.Fatal("draft review target is not a numeric foreign key")
	}
	if strings.Contains(definition, "review_target_public_id") {
		t.Fatal("draft review target still stores an internal public-ID relation")
	}
}

func TestDownloadCountersUseNumericRelations(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(communitySchemaStatements(), "\n"))
	for _, required := range []string{
		"object_route_id bigint not null references public_routes(id)",
		"primary key(object_route_id,owner_id)",
		"primary key(object_route_id,owner_id,currency_id)",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("numeric download relation %q is missing", required)
		}
	}
	if strings.Contains(definition, "object_public_id") {
		t.Fatal("download counters still store an internal public-ID relation")
	}
}

func TestPublicRouteHasGlobalNumericInternalIdentity(t *testing.T) {
	t.Parallel()
	var definition string
	for _, statement := range baselineSchemaStatements() {
		if strings.Contains(statement, "create table public_routes") {
			definition = strings.ToLower(statement)
			break
		}
	}
	if definition == "" {
		t.Fatal("public_routes schema was not found")
	}
	if !strings.Contains(definition, "id bigserial primary key") {
		t.Fatal("public route does not have a global numeric internal ID")
	}
	if !strings.Contains(definition, "public_id varchar(16) not null unique") {
		t.Fatal("public route external ID is not a unique boundary identifier")
	}
	if strings.Contains(definition, "public_id varchar(16) primary key") {
		t.Fatal("public route still uses the external character ID as its internal primary key")
	}
}
