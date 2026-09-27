package database

import (
	"os"
	"strings"
	"testing"
)

func TestMAP003SchemaCleanupAndProgressionShareActivityDictionary(t *testing.T) {
	communitySource, err := os.ReadFile("community_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	cleanupSource, err := os.ReadFile("../httpapi/activity_retention_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	progressionSource, err := os.ReadFile("../progression/service.go")
	if err != nil {
		t.Fatal(err)
	}
	configurationSource, err := os.ReadFile("../progression/task_configuration.go")
	if err != nil {
		t.Fatal(err)
	}

	for name, source := range map[string]string{
		"schema":        string(communitySource),
		"cleanup":       string(cleanupSource),
		"progression":   string(progressionSource),
		"configuration": string(configurationSource),
	} {
		if !strings.Contains(source, "activitycatalog.") {
			t.Errorf("%s does not consume the authoritative activity dictionary", name)
		}
	}
	for _, required := range []string{"activitycatalog.ActionDefinitions()", "activitycatalog.ObjectTypeDefinitions()"} {
		if !strings.Contains(string(communitySource), required) {
			t.Errorf("Schema seed is not derived through %s", required)
		}
	}
	for _, required := range []string{"activitycatalog.ActionIDs()", "activitycatalog.ObjectTypeIDs()"} {
		if !strings.Contains(string(cleanupSource), required) {
			t.Errorf("cleanup filters are not derived through %s", required)
		}
	}
	for _, source := range []string{string(progressionSource), string(configurationSource)} {
		if !strings.Contains(source, "activitycatalog.ActionID(") || !strings.Contains(source, "activitycatalog.ObjectTypeID(") {
			t.Error("progression task matching does not use authoritative activity lookups")
		}
	}
	if strings.Contains(string(progressionSource), "func actionID(") || strings.Contains(string(progressionSource), "func objectTypeID(") {
		t.Fatal("progression still owns duplicate activity code switches")
	}
}
