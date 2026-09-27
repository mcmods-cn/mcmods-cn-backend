package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseStickerTokenRequiresOneExactCanonicalToken(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		token      string
		pack, code string
		valid      bool
	}{
		{token: "[sticker:animals:happy_cat]", pack: "animals", code: "happy_cat", valid: true},
		{token: "[sticker:a:b]", pack: "a", code: "b", valid: true},
		{token: "prefix [sticker:a:b]", valid: false},
		{token: "[sticker:a:b] suffix", valid: false},
		{token: "[sticker:A:b]", valid: false},
		{token: "[sticker:a:b:c]", valid: false},
		{token: "[sticker:a:]", valid: false},
		{token: "", valid: false},
	} {
		pack, code, ok := parseStickerToken(test.token)
		if pack != test.pack || code != test.code || ok != test.valid {
			t.Fatalf("parseStickerToken(%q) = (%q, %q, %v), want (%q, %q, %v)", test.token, pack, code, ok, test.pack, test.code, test.valid)
		}
	}
}

func TestStickerLifecycleUsesStructuredReferencesAndTransactionLocks(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("sticker_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	definition := strings.ToLower(string(source))
	for _, required := range []string{
		"select id from sticker_packs where code=$1 for update",
		"select exists(select 1 from sticker_content_references where pack_code=$1 and sticker_code=$2)",
		"select pg_advisory_xact_lock(hashtext('sticker-reference'),hashtext($1||':'||$2))",
		"tombstoneunreferencedstickerfiletx",
		"select status from oss_files where id=$1 for update",
		"lockstickercatalogbudgettx",
		"select count(*)<$1 from sticker_packs",
		"select (select count(*) from stickers where pack_id=$1)<$2",
		"limits.maxcatalogitems+1",
		"limits.maxcatalogitems+limits.maxpacks+1",
		"if err = rows.err(); err != nil",
		"failed to decode sticker translations",
		"sticker_packs_code_key",
		"sticker_derived",
		"sticker_source_consumed",
		"cleanupunboundstickerimage",
	} {
		if !strings.Contains(definition, required) {
			t.Fatalf("sticker lifecycle is missing %q", required)
		}
	}
	if got := strings.Count(definition, "select id from sticker_packs where code=$1 for update"); got < 3 {
		t.Fatalf("only %d sticker mutation/deletion paths lock the parent pack, want at least 3", got)
	}
	for _, forbidden := range []string{
		"position($1 in body)",
		"position($1 in body_markdown)",
		"position($1 in description_markdown)",
		"position($1 in content_markdown)",
	} {
		if strings.Contains(definition, forbidden) {
			t.Fatalf("sticker lifecycle retains full-table substring scan %q", forbidden)
		}
	}
}

func TestStickerMutationDatabaseErrorsDistinguishImageOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		err        error
		statusCode int
		apiCode    string
	}{
		{
			name:       "exclusive image",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "uq_stickers_image_file"},
			statusCode: http.StatusConflict,
			apiCode:    "STICKER_IMAGE_IN_USE",
		},
		{
			name:       "duplicate code",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "stickers_pack_id_code_key"},
			statusCode: http.StatusConflict,
			apiCode:    "STICKER_CODE_EXISTS",
		},
		{
			name:       "unexpected database failure",
			err:        errors.New("database unavailable"),
			statusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeStickerMutationDatabaseError(response, test.err)
			if response.Code != test.statusCode {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.statusCode, response.Body.String())
			}
			if test.apiCode != "" && !strings.Contains(response.Body.String(), test.apiCode) {
				t.Fatalf("response is missing %s: %s", test.apiCode, response.Body.String())
			}
		})
	}
}

func TestStickerPackCreateErrorsOnlyClassifyItsCodeConstraintAsConflict(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		err        error
		statusCode int
		apiCode    string
	}{
		{
			name:       "duplicate pack code",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "sticker_packs_code_key"},
			statusCode: http.StatusConflict,
			apiCode:    "STICKER_PACK_CODE_EXISTS",
		},
		{
			name:       "different unique constraint",
			err:        &pgconn.PgError{Code: "23505", ConstraintName: "unrelated_constraint"},
			statusCode: http.StatusInternalServerError,
		},
		{
			name:       "database unavailable",
			err:        errors.New("database unavailable"),
			statusCode: http.StatusInternalServerError,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeStickerPackCreateDatabaseError(response, test.err)
			if response.Code != test.statusCode {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.statusCode, response.Body.String())
			}
			if test.apiCode != "" && !strings.Contains(response.Body.String(), test.apiCode) {
				t.Fatalf("response is missing %s: %s", test.apiCode, response.Body.String())
			}
		})
	}
}
