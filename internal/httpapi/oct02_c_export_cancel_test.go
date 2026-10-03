package httpapi

import (
	"context"
	"errors"
	"testing"
)

func TestOCT02CExportRecipeParsingPreservesCancellationCause(t *testing.T) {
	files := testExportZIPFiles(t, map[string]string{"recipes/jei/recipes/cancelled.json": `{}`})
	ctx, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("owned import attempt cancelled")
	cancel(cause)
	consumed := false
	err := processExportRecipeJSONFiles(ctx, files, "recipes/jei/recipes/",
		func(raw []byte) ([]byte, error) { return raw, nil },
		func(_ string, _ []byte) error { consumed = true; return nil })
	if !errors.Is(err, cause) {
		t.Fatalf("cancelled recipe parsing error=%v, want original cancellation cause", err)
	}
	if consumed {
		t.Fatal("cancelled recipe parsing consumed a document")
	}
}

func TestOCT02CExportRecipeParsingRejectsLateDecodedResult(t *testing.T) {
	files := testExportZIPFiles(t, map[string]string{"recipes/jei/recipes/cancelled.json": `{}`})
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	cause := errors.New("owned import attempt cancelled during decoding")
	decoded, consumed := false, false
	err := processExportRecipeJSONFiles(ctx, files, "recipes/jei/recipes/",
		func(raw []byte) ([]byte, error) { decoded = true; cancel(cause); return raw, nil },
		func(_ string, _ []byte) error { consumed = true; return nil })
	if !errors.Is(err, cause) || !decoded || consumed {
		t.Fatalf("cancelled late result: err=%v decoded=%t consumed=%t", err, decoded, consumed)
	}
}
