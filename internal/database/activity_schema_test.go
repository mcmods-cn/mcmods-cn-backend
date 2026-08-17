package database

import (
	"strings"
	"testing"
)

func TestActivityTableUsesLeanNumericStorage(t *testing.T) {
	t.Parallel()
	var definition string
	for _, statement := range communitySchemaStatements() {
		if strings.Contains(statement, "create table if not exists user_activity_events") {
			definition = strings.ToLower(statement)
			break
		}
	}
	if definition == "" {
		t.Fatal("user_activity_events schema was not found")
	}
	for _, required := range []string{"id bigserial primary key", "user_id bigint", "object_route_id bigint references public_routes(id)", "occurred_at timestamptz"} {
		if !strings.Contains(definition, required) {
			t.Fatalf("activity schema does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"metadata jsonb", "object_public_id", "user_public_id", "public_id text"} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("activity schema still contains high-volume field %q", forbidden)
		}
	}
}

func TestContributionsArePersistedAsDailyAggregates(t *testing.T) {
	t.Parallel()
	statements := strings.ToLower(strings.Join(communitySchemaStatements(), "\n"))
	for _, required := range []string{
		"create table if not exists user_daily_contributions",
		"primary key(user_id,contribution_date)",
		"sync_user_daily_contribution",
		"trg_change_requests_daily_contribution",
		"from change_requests",
		"where status='approved'",
	} {
		if !strings.Contains(statements, required) {
			t.Fatalf("contribution aggregate schema does not contain %q", required)
		}
	}
}

func TestFavoriteCollectionsHaveExplicitVisibility(t *testing.T) {
	t.Parallel()
	statements := strings.ToLower(strings.Join(baselineSchemaStatements(), "\n"))
	if !strings.Contains(statements, "is_public boolean not null default false") {
		t.Fatal("favorite collections must default to private visibility")
	}
	if !strings.Contains(statements, "idx_favorite_collections_public") {
		t.Fatal("public favorite collection lookup index is missing")
	}
}

func TestMinecraftServersExistBeforeDraftForeignKey(t *testing.T) {
	t.Parallel()
	statements := schemaInstallationStatements()
	serverTable, draftTable := -1, -1
	for index, statement := range statements {
		normalized := strings.ToLower(statement)
		if strings.Contains(normalized, "create table minecraft_servers") {
			serverTable = index
		}
		if strings.Contains(normalized, "create table user_drafts") {
			draftTable = index
		}
	}
	if serverTable < 0 || draftTable < 0 || serverTable >= draftTable {
		t.Fatalf("minecraft_servers must be created before user_drafts: server=%d draft=%d", serverTable, draftTable)
	}
}

func TestModSearchTriggerOnlyReferencesCurrentModColumns(t *testing.T) {
	definition := strings.Join(searchSchemaStatements(), "\n")
	if strings.Contains(definition, "secondary_name,public_id,project_code") {
		t.Fatal("mod search trigger still references removed mods.public_id column")
	}
	if !strings.Contains(definition, "update of primary_name,secondary_name,project_code or delete on mods") {
		t.Fatal("mod search trigger does not track the current mod identity and display columns")
	}
}

func TestRatingsAndPopularityUseCompactNumericRelations(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, required := range []string{
		"object_route_id bigint not null references public_routes(id)",
		"rating_id bigint not null references content_ratings(id)",
		"primary key(object_route_id,view_date,counter_shard)",
		"create table content_popularity_stats",
		"refresh_content_popularity",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("rating schema does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"target_public_id", "object_public_id", "view_events", "metadata jsonb"} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("rating schema contains high-volume or character relation %q", forbidden)
		}
	}
}

func TestPopularityFormulaAndPromotionItemsMatchDesign(t *testing.T) {
	t.Parallel()
	ratingDefinition := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	for _, required := range []string{
		"0.45*long_term+0.35*trend+0.10*effective_views+0.10*promotion",
		"quality:=0.85+0.15",
		"/7)),0) into trend_raw",
		"new_boost:=1+0.15*power(2::numeric",
		"promotion_kind in ('project','server')",
	} {
		if !strings.Contains(ratingDefinition, required) {
			t.Errorf("rating heat schema is missing design term %q", required)
		}
	}
	commentDefinition := strings.ToLower(strings.Join(commentSchemaStatements(), "\n"))
	for _, required := range []string{
		"0.30*ln(1+likes::numeric)",
		"0.30*ln(1+weighted_replies)",
		"0.15*user_score+0.10*quality",
		"0.15*ln(1+watches::numeric)",
		"/1209600.0",
	} {
		if !strings.Contains(commentDefinition, required) {
			t.Errorf("comment heat schema is missing design term %q", required)
		}
	}
	shopDefinition := strings.ToLower(strings.Join(communitySchemaStatements(), "\n"))
	for _, required := range []string{"'project_heat_boost'", "'server_heat_boost'", "shop.project_heat_boost.use", "shop.server_heat_boost.use"} {
		if !strings.Contains(shopDefinition, required) {
			t.Errorf("shop schema is missing separate heat item %q", required)
		}
	}
}

func TestPopularityThresholdColumnsAreQualified(t *testing.T) {
	t.Parallel()
	definition := strings.ToLower(strings.Join(ratingSchemaStatements(), "\n"))
	qualified := `select threshold.favorite_threshold,threshold.commenter_threshold,threshold.download_threshold,
				threshold.rating_threshold,threshold.view_threshold,threshold.trend_threshold`
	if !strings.Contains(definition, qualified) {
		t.Fatal("popularity threshold columns must be qualified to avoid PL/pgSQL variable ambiguity")
	}
	if strings.Contains(definition, "select favorite_threshold,commenter_threshold,download_threshold") {
		t.Fatal("popularity function still contains ambiguous unqualified threshold columns")
	}
}
