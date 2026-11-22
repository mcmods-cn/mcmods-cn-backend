package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateRepairStateIsExclusiveOwnerOnlyAndRejectsSymlinks(t *testing.T) {
	directory := privateRepairTestDirectory(t)
	path := filepath.Join(directory, "private-plan.json")
	if err := writePrivate(path, map[string]any{"private": "synthetic capability"}); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("private state must actually be 0600", err)
	}
	if err = writePrivate(path, map[string]any{"replace": true}); err == nil {
		t.Fatal("existing rollback/plan must never be overwritten")
	}
	var value map[string]any
	if err = readPrivate(path, &value); err != nil || value["private"] != "synthetic capability" {
		t.Fatal("actual private state roundtrip failed", err)
	}
	link := filepath.Join(directory, "link.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err = readPrivate(link, &value); err == nil {
		t.Fatal("private input symlink must not be followed")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err = readPrivate(path, &value); err == nil {
		t.Fatal("world-readable private state must be rejected")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(directory, 0750); err != nil {
		t.Fatal(err)
	}
	if err = readPrivate(path, &value); err == nil {
		t.Fatal("private input under a group-accessible directory must be rejected")
	}
	if err = writePrivate(filepath.Join(directory, "another.json"), value); err == nil {
		t.Fatal("private output under a group-accessible directory must be rejected")
	}
}

func TestVerifiedBackupDeclarationChecksActualBytesAndExactTarget(t *testing.T) {
	directory := privateRepairTestDirectory(t)
	path := filepath.Join(directory, "backup.json")
	data := []byte("owned selected-row synthetic backup")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	target := strings.Repeat("a", 64)
	if _, err := verifiedBackup(path, digest, target, target); err != nil {
		t.Fatal(err)
	}
	if _, err := verifiedBackup(path, digest, strings.Repeat("b", 64), target); err == nil {
		t.Fatal("backup declaration for a different database target must reject")
	}
	if _, err := verifiedBackup(path, strings.Repeat("0", 64), target, target); err == nil {
		t.Fatal("actual backup checksum mismatch must reject")
	}
}

func privateRepairTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	return directory
}
