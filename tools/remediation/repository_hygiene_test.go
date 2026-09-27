package remediation

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func remediationRepositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

func TestRepositoryGoFilesAreFormatted(t *testing.T) {
	root := remediationRepositoryRoot(t)
	var unformatted []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
		formatted, err := format.Source(normalized)
		if err != nil {
			return err
		}
		if !bytes.Equal(normalized, formatted) {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			unformatted = append(unformatted, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(unformatted) > 0 {
		t.Fatalf("Go files require gofmt: %s", strings.Join(unformatted, ", "))
	}
}

func TestIgnoredIDEFilesAreNotTracked(t *testing.T) {
	root := remediationRepositoryRoot(t)
	command := exec.Command("git", "-C", root, "ls-files", "--", ".idea")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list tracked IDE files: %v", err)
	}
	for _, tracked := range strings.Fields(string(output)) {
		_, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(tracked)))
		if statErr == nil {
			t.Fatalf("ignored IDE file remains present in the tracked working tree: %s", tracked)
		}
		if !os.IsNotExist(statErr) {
			t.Fatalf("inspect tracked IDE file %s: %v", tracked, statErr)
		}
	}
}
