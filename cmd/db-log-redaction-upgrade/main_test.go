package main

import (
	"os"
	"path/filepath"
	"testing"

	"mcmods-cn-backend/internal/database"
)

func TestLogRedactionMetadataRecordIsPrivateAndCannotBeOverwritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), "before.json")
	if err := persistState(path, database.LogRedactionSchemaState{Version: 1, Database: "owned-fixture", Generation: 168}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("schema metadata record is not private: %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := persistState(path, database.LogRedactionSchemaState{Generation: 999}); err == nil {
		t.Fatal("existing metadata record was overwritten")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("existing metadata record changed: %v", err)
	}
}
