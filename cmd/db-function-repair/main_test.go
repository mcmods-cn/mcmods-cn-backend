package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"mcmods-cn-backend/internal/database"
)

func TestFunctionBackupIsPrivateDurableAndCannotOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original.json")
	backup := database.ProjectionFunctionBackup{Version: 1, Database: "owned", Generation: 168,
		Functions: map[string]string{"fixture": "definition"}}
	if err := writeBackup(path, backup); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatalf("backup permissions are not private: %v", err)
	}
	if err := writeBackup(path, database.ProjectionFunctionBackup{}); err == nil {
		t.Fatal("existing backup was overwritten")
	}
	read, err := readBackup(path)
	if err != nil || !reflect.DeepEqual(read, backup) {
		t.Fatalf("backup did not retain original definitions: %v", err)
	}
}

func TestFunctionBackupRejectsMalformedUnknownAndOversizedInput(t *testing.T) {
	for _, input := range []string{"not JSON", `{"version":1,"unexpected":true}`, `{} {}`, string(make([]byte, (1<<20)+1))} {
		path := filepath.Join(t.TempDir(), "bad.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readBackup(path); err == nil {
			t.Fatal("invalid backup was accepted")
		}
	}
}
