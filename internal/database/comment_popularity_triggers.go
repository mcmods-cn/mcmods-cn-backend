package database

import (
	"errors"
	"strings"
)

// These statement bindings are shared by fresh schema installation and the
// explicit generation-168 forward repair. Updates deliberately have no column
// restriction: unchanged published groups cancel in the transition-table delta,
// while changes of author or target must update both affected route facts.
var commentPopularityTriggerDefinitions = map[string]string{
	"trg_comments_popularity_insert": `create trigger trg_comments_popularity_insert after insert on comments
		referencing new table as comment_popularity_new for each statement execute function refresh_popularity_from_comment()`,
	"trg_comments_popularity_update": `create trigger trg_comments_popularity_update after update on comments
		referencing old table as comment_popularity_old new table as comment_popularity_new
		for each statement execute function refresh_popularity_from_comment()`,
	"trg_comments_popularity_delete": `create trigger trg_comments_popularity_delete after delete on comments
		referencing old table as comment_popularity_old for each statement execute function refresh_popularity_from_comment()`,
}

const legacyCommentPopularityTriggerSQL = `create trigger trg_comments_popularity
	after insert or delete or update of status on comments
	for each row execute function refresh_popularity_from_comment()`

var commentPopularityTriggerNames = []string{"trg_comments_popularity", "trg_comments_popularity_insert",
	"trg_comments_popularity_update", "trg_comments_popularity_delete"}

func normalizedCommentTriggerSQL(sql string) string {
	value := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	value = strings.ReplaceAll(value, "public.comments", "comments")
	return strings.ReplaceAll(value, "public.refresh_popularity_from_comment", "refresh_popularity_from_comment")
}

func validateCommentTriggerBackup(triggers map[string]string) error {
	if len(triggers) == 1 && normalizedCommentTriggerSQL(triggers["trg_comments_popularity"]) ==
		normalizedCommentTriggerSQL(legacyCommentPopularityTriggerSQL) {
		return nil
	}
	if len(triggers) != len(commentPopularityTriggerDefinitions) {
		return errors.New("comment trigger backup does not match the legacy or current binding set")
	}
	for name, definition := range commentPopularityTriggerDefinitions {
		if normalizedCommentTriggerSQL(triggers[name]) != normalizedCommentTriggerSQL(definition) {
			return errors.New("comment trigger backup contains an unsupported binding definition")
		}
	}
	return nil
}
