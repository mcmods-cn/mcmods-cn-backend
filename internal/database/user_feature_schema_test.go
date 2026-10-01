package database

import (
	"strings"
	"testing"
)

func TestUserFeatureSchemaKeepsStatisticsIndependentFromActivityRetention(t *testing.T) {
	t.Parallel()
	if schemaGeneration != 168 {
		t.Fatalf("unexpected schema generation %d", schemaGeneration)
	}
	definition := strings.ToLower(strings.Join(userFeatureSchemaStatements(), "\n"))
	for _, required := range []string{
		"show_online_status boolean not null default false",
		"cardinality(public_card_stat_slots)=6",
		"create table if not exists user_presence_sessions",
		"create table if not exists user_statistics_daily",
		"create table user_statistics_retained_actions",
		"create trigger trg_retain_deleted_user_activity_statistics after delete on user_activity_events",
		"referencing old table as deleted_user_activity_events for each statement",
		"user_statistics_retained_actions.event_count+excluded.event_count",
		"filter(where event.action_id=2 and event.object_type_id=8)",
		"create table if not exists user_statistics_totals",
		"create table if not exists user_content_creation_facts",
		"create table if not exists activity_cleanup_runs",
		"create trigger trg_user_activity_statistics after insert on user_activity_events",
		"idx_activity_action_time",
		"create table activity_event_outbox",
		"idx_activity_outbox_available",
		"create or replace function sync_user_content_creation_fact()",
		"row_data->>'submitted_by'",
		"row_data->>'author_id'",
		"object_identifier text",
		"values(content_kind,object_identifier,actor_id",
		"user_id=excluded.user_id",
		"update of review_status,submitted_by or delete on mods",
		"update of review_status,submitted_by or delete on modpacks",
		"update of review_status,submitted_by or delete on simple_projects",
		"update of review_status,submitted_by or delete on minecraft_servers",
		"update of review_status,status,author_id or delete on community_posts",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("user feature schema is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"truncate user_activity_events",
		"delete from user_statistics_",
		"insert into user_statistics_daily select",
		"object_key text; review_state",
		"row_data->>'created_by'",
	} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("current schema contains forbidden SQL: %q", forbidden)
		}
	}
}
