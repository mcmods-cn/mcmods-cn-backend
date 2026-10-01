package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestExportRecipeCancellationIsNotPartialSuccess(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("recipes/fixture.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte(`{"synthetic":true}`)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]*zip.File{reader.File[0].Name: reader.File[0]}
	for _, beforeParsing := range []bool{true, false} {
		t.Run(map[bool]string{true: "before parsing", false: "while consuming"}[beforeParsing], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if beforeParsing {
				cancel()
			}
			calls := 0
			err := processExportRecipeJSONFiles(ctx, files, "recipes/", func(raw []byte) (string, error) { return string(raw), nil }, func(_ string, _ string) error { calls++; cancel(); return nil })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled import returned success: %v", err)
			}
			if beforeParsing && calls != 0 {
				t.Fatalf("cancelled import wrote %d files", calls)
			}
		})
	}
}
