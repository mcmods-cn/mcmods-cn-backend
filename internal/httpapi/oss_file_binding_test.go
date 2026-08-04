package httpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type trustedRasterTestQuery struct {
	file trustedRasterOSSFile
	err  error
	sql  string
	args []any
}

func (query *trustedRasterTestQuery) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	query.sql = sql
	query.args = append([]any(nil), args...)
	return trustedRasterTestRow{file: query.file, err: query.err}
}

type trustedRasterTestRow struct {
	file trustedRasterOSSFile
	err  error
}

func (row trustedRasterTestRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(dest) != 9 {
		return fmt.Errorf("unexpected destination count: %d", len(dest))
	}
	*dest[0].(*int64) = row.file.ID
	*dest[1].(*string) = row.file.PublicID
	*dest[2].(*int64) = row.file.UploaderID
	*dest[3].(*string) = row.file.ObjectKey
	*dest[4].(*string) = row.file.OriginalName
	*dest[5].(*string) = row.file.ContentType
	*dest[6].(*int64) = row.file.SizeBytes
	*dest[7].(*string) = row.file.Status
	*dest[8].(*string) = row.file.ScanStatus
	return nil
}

func TestResolveTrustedRasterOSSFilePublicID(t *testing.T) {
	baseFile := trustedRasterOSSFile{
		ID:           42,
		PublicID:     "abc123xyz",
		UploaderID:   17,
		ObjectKey:    "uploads/17/avatar.png",
		OriginalName: "avatar.png",
		ContentType:  "image/png",
		SizeBytes:    1024,
		Status:       "active",
		ScanStatus:   "clean",
	}
	tests := []struct {
		name      string
		mutate    func(*trustedRasterOSSFile)
		scope     ossRasterBindingScope
		wantError bool
	}{
		{
			name:  "active clean raster owned by uploader",
			scope: ossRasterBindingScope{UploaderID: 17},
		},
		{
			name: "trusted generated raster may be bound by explicitly authorized workflow",
			mutate: func(file *trustedRasterOSSFile) {
				file.ScanStatus = "trusted_generated"
				file.ContentType = "image/webp; charset=binary"
				file.UploaderID = 99
			},
			scope: ossRasterBindingScope{UploaderID: 17, AllowAnyUploader: true},
		},
		{
			name: "svg is rejected",
			mutate: func(file *trustedRasterOSSFile) {
				file.ContentType = "image/svg+xml"
			},
			scope:     ossRasterBindingScope{UploaderID: 17},
			wantError: true,
		},
		{
			name: "pending scan is rejected",
			mutate: func(file *trustedRasterOSSFile) {
				file.ScanStatus = "pending"
			},
			scope:     ossRasterBindingScope{UploaderID: 17},
			wantError: true,
		},
		{
			name: "wrong uploader is rejected",
			mutate: func(file *trustedRasterOSSFile) {
				file.UploaderID = 99
			},
			scope:     ossRasterBindingScope{UploaderID: 17},
			wantError: true,
		},
		{
			name: "inactive file is rejected",
			mutate: func(file *trustedRasterOSSFile) {
				file.Status = "deleted"
			},
			scope:     ossRasterBindingScope{UploaderID: 17},
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := baseFile
			if test.mutate != nil {
				test.mutate(&file)
			}
			query := &trustedRasterTestQuery{file: file}
			resolved, err := resolveTrustedRasterOSSFilePublicID(
				context.Background(),
				query,
				" ABC123XYZ ",
				test.scope,
			)
			assertTrustedRasterBindingSQL(t, query.sql, query.args)
			if test.wantError {
				if !errors.Is(err, errOSSFileNotBindable) {
					t.Fatalf("expected bindable-file error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve trusted raster file: %v", err)
			}
			if resolved != file {
				t.Fatalf("resolved file mismatch: got %#v, want %#v", resolved, file)
			}
		})
	}
}

func TestSafeRasterContentType(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "image/png", want: true},
		{value: " IMAGE/JPEG ; charset=binary ", want: true},
		{value: "image/apng", want: true},
		{value: "image/webp", want: true},
		{value: "image/svg+xml", want: false},
		{value: "image/png+xml", want: false},
		{value: "text/html", want: false},
		{value: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			if got := safeRasterContentType(test.value); got != test.want {
				t.Fatalf("safeRasterContentType(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func assertTrustedRasterBindingSQL(t *testing.T, sql string, args []any) {
	t.Helper()
	for _, fragment := range []string{
		"status='active'",
		"scan_status in ('clean','trusted_generated')",
		"split_part(content_type,';',1)",
		"=any($4::text[])",
		"($2 or uploader_id=$3)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("binding query is missing %q: %s", fragment, sql)
		}
	}
	if len(args) != 4 {
		t.Fatalf("binding query got %d arguments, want 4", len(args))
	}
	if got, ok := args[0].(string); !ok || got != "abc123xyz" {
		t.Errorf("public ID argument = %#v, want normalized public ID", args[0])
	}
	allowed, ok := args[3].([]string)
	if !ok {
		t.Fatalf("allowed MIME argument has type %T, want []string", args[3])
	}
	for _, contentType := range allowed {
		if strings.EqualFold(contentType, "image/svg+xml") {
			t.Fatal("SVG must not be present in the SQL raster allowlist")
		}
	}
}
