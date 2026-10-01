package httpapi

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type reviewConfigFailureQuery struct {
	raw string
	err error
}

func (q reviewConfigFailureQuery) QueryRow(context.Context, string, ...any) pgx.Row { return q }
func (q reviewConfigFailureQuery) Scan(destinations ...any) error {
	if q.err != nil {
		return q.err
	}
	*destinations[0].(*[]byte) = []byte(q.raw)
	return nil
}

func TestReviewConfigurationFailureCannotDisableModeration(t *testing.T) {
	for _, query := range []reviewConfigFailureQuery{
		{err: errors.New("synthetic query failure")}, {raw: `42`}, {raw: `null`}, {raw: `{"aiTranslation":null}`}, {raw: `{"aiTranslation":false,"modCreate":"invalid"}`},
	} {
		config := loadReviewConfig(context.Background(), query)
		if !config.AITranslation || !config.BlueprintCreate || !config.BlueprintEdit || !config.ModContentSectionCreate || !config.ModCreate {
			t.Fatal("failed configuration silently disabled moderation")
		}
	}
	if config := loadReviewConfig(context.Background(), reviewConfigFailureQuery{err: pgx.ErrNoRows}); config != defaultReviewConfig() {
		t.Fatal("missing bootstrap configuration changed the documented defaults")
	}
	if config := loadReviewConfig(context.Background(), reviewConfigFailureQuery{raw: `{"aiTranslation":false,"modCreate":false}`}); config.AITranslation || config.ModCreate {
		t.Fatal("valid administrator moderation settings were ignored")
	}
}

func TestNotificationTemplateConfigurationFailureIsVisibleToAdministrators(t *testing.T) {
	for _, query := range []reviewConfigFailureQuery{
		{err: errors.New("synthetic query failure")}, {raw: `null`}, {raw: `42`}, {raw: `{"templates":"invalid"}`},
	} {
		if _, err := loadNotificationTemplateConfigChecked(context.Background(), query); err == nil {
			t.Fatal("invalid notification template configuration was accepted")
		}
		if config := loadNotificationTemplateConfig(context.Background(), query); len(config.Templates) == 0 {
			t.Fatal("runtime delivery lost the documented built-in fallback")
		}
	}
}
