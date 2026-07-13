package httpapi

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// This test is opt-in because the real exporter packages are large and live
// outside the repository. It validates the package contract without requiring
// PostgreSQL or OSS.
func TestExporterSamplePackageContracts(t *testing.T) {
	directory := strings.TrimSpace(os.Getenv("MCMODS_EXPORT_TEST_DIR"))
	if directory == "" {
		t.Skip("MCMODS_EXPORT_TEST_DIR is not configured")
	}
	archives, err := filepath.Glob(filepath.Join(directory, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) == 0 {
		t.Fatal("no exporter ZIP packages found")
	}
	for _, archive := range archives {
		archive := archive
		t.Run(filepath.Base(archive), func(t *testing.T) {
			reader, openErr := zip.OpenReader(archive)
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer reader.Close()
			files, validateErr := validateExportZIP(reader.File)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			manifestRaw, readErr := readExportZIPFile(files["manifest.json"], 4<<20)
			if readErr != nil {
				t.Fatal(readErr)
			}
			var manifest modExportManifest
			if unmarshalErr := json.Unmarshal(manifestRaw, &manifest); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			if validateErr = validateModExportManifest(manifest); validateErr != nil {
				t.Fatal(validateErr)
			}
			capabilityFile := files[modExportCapabilitiesPath]
			if capabilityFile == nil {
				t.Fatalf("%s is missing", modExportCapabilitiesPath)
			}
			capabilityRaw, readErr := readExportZIPFile(capabilityFile, 4<<20)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if _, decodeErr := decodeModExportCapabilities(capabilityRaw, manifest); decodeErr != nil {
				t.Fatal(decodeErr)
			}

			layoutFiles := 0
			layoutRows := 0
			for name, file := range files {
				if file.FileInfo().IsDir() || !strings.HasSuffix(name, ".json") {
					continue
				}
				if !strings.HasPrefix(name, "recipes/jei/layouts/") &&
					!(strings.HasPrefix(name, "translations/") && path.Base(name) != "languages.json") {
					continue
				}
				raw, fileErr := readExportZIPFile(file, maxExportJSONSize)
				if fileErr != nil {
					t.Fatalf("read %s: %v", name, fileErr)
				}
				if strings.HasPrefix(name, "translations/") {
					if _, _, _, decodeErr := decodeExportTranslationValues(raw); decodeErr != nil {
						t.Fatalf("decode %s: %v", name, decodeErr)
					}
					continue
				}
				batch := newModExportWriteBatch()
				if queueErr := queueExportRecipeLayout(batch, "sample-package", "sample-revision", name, raw); queueErr != nil {
					t.Fatal(queueErr)
				}
				layoutFiles++
				layoutRows += batch.rows
			}
			if layoutFiles == 0 || layoutRows == 0 {
				t.Fatal("package contains no importable JEI layout collections")
			}
		})
	}
}
