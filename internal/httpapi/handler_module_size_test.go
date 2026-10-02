package httpapi

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionHandlerModulesStayReviewable(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_handlers.go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, openErr := os.Open(filepath.Clean(name))
		if openErr != nil {
			t.Fatal(openErr)
		}
		lines := 0
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines++
		}
		scanErr := scanner.Err()
		closeErr := file.Close()
		if scanErr != nil {
			t.Fatal(scanErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		if lines > 1500 {
			t.Errorf("%s has %d lines; split handler responsibilities at 1500", name, lines)
		}
	}
}
