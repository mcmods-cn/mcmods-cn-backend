package searchindex

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestTEST030SearchRegistryAndRollingVersionContracts(t *testing.T) {
	workerSource, err := os.ReadFile("worker.go")
	if err != nil {
		t.Fatal(err)
	}
	schemaSource, err := os.ReadFile("schema.go")
	if err != nil {
		t.Fatal(err)
	}
	contract := strings.ToLower(string(workerSource) + "\n" + string(schemaSource))
	for _, required := range []string{
		"errsearchprojectionversionsuperseded",
		"ensureprojectionversionnotsuperseded",
		"searchregistry",
		"registereddocument",
	} {
		if !strings.Contains(contract, required) {
			t.Errorf("search state-machine contract is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"func collectionkind(documenttype string) string",
		"failed := false",
		"if failed {",
	} {
		if strings.Contains(contract, forbidden) {
			t.Errorf("search state-machine still contains obsolete mapping/control flow %q", forbidden)
		}
	}
}

func TestTEST030SearchRegistryMatchesDatabaseDocumentTypes(t *testing.T) {
	wantCollections := []string{"projects", "community", "creators", "resources", "servers"}
	wantDocuments := []string{"mod", "modpack", "simple_project", "community_post", "creator", "resource", "server"}
	collections := make([]string, 0, len(searchRegistry))
	documents := make([]string, 0, len(wantDocuments))
	seenDocuments := make(map[string]bool, len(wantDocuments))
	for _, collection := range searchRegistry {
		collections = append(collections, collection.kind)
		if collection.kind == "" || len(collection.schema.Fields) == 0 || len(collection.documents) == 0 {
			t.Fatalf("incomplete collection registration: %#v", collection)
		}
		for _, document := range collection.documents {
			if seenDocuments[document.documentType] || document.idPageQuery == "" || document.load == nil {
				t.Fatalf("duplicate or incomplete document registration: %#v", document)
			}
			seenDocuments[document.documentType] = true
			documents = append(documents, document.documentType)
			registeredCollection, registeredDocument, err := registeredDocument(document.documentType)
			if err != nil || registeredCollection.kind != collection.kind || registeredDocument.documentType != document.documentType {
				t.Fatalf("lookup %q returned collection=%q document=%q err=%v",
					document.documentType, registeredCollection.kind, registeredDocument.documentType, err)
			}
		}
	}
	if !reflect.DeepEqual(collections, wantCollections) || !reflect.DeepEqual(documents, wantDocuments) {
		t.Fatalf("registry collections=%v documents=%v", collections, documents)
	}
	if len(collectionSchemas()) != len(wantCollections) {
		t.Fatalf("derived collection schemas=%d want %d", len(collectionSchemas()), len(wantCollections))
	}
	if _, _, err := registeredDocument("unknown"); err == nil {
		t.Fatal("unknown document type was accepted")
	}
	if _, err := registeredCollection("unknown"); err == nil {
		t.Fatal("unknown collection was accepted")
	}

	databaseSchema, err := os.ReadFile("../database/search_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	databaseContract := strings.ReplaceAll(strings.ToLower(string(databaseSchema)), "\r", "")
	wantConstraint := "document_type text not null check(document_type in ('mod','modpack','simple_project','creator','community_post','resource','server'))"
	if !strings.Contains(databaseContract, wantConstraint) {
		t.Fatalf("database search document constraint no longer matches the registry: want %q", wantConstraint)
	}
}
