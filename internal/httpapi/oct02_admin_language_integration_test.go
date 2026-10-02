package httpapi

import (
	"testing"
)

func TestOCT02AdminDetailsSeparateSecondaryContentAndUILanguageIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	if _, err := f.db.Exec(f.ctx, `update users set preferred_content_language='zh-CN',
	 secondary_content_language='ja-JP',preferred_ui_language='de-DE' where id=$1`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	details, err := (&Server{db: f.db}).loadAdminUserDetails(f.ctx, f.userIDs[f.editor])
	if err != nil {
		t.Fatal(err)
	}
	if details.PrimaryLanguage != "zh-CN" || details.SecondaryLanguage != "ja-JP" {
		t.Fatalf("content languages reported primary=%s secondary=%s", details.PrimaryLanguage, details.SecondaryLanguage)
	}
}
