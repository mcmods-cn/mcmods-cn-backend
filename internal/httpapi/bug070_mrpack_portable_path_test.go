package httpapi

import (
	"strings"
	"testing"
)

func TestBuildMRPackRejectsPortablePathCollisions(t *testing.T) {
	testCases := []struct {
		name  string
		left  string
		right string
	}{
		{name: "case fold", left: "Example.jar", right: "example.jar"},
		{name: "canonical Unicode", left: "Caf\u00e9.jar", right: "Cafe\u0301.jar"},
		{name: "compatibility Unicode", left: "mod\ufb01le.jar", right: "modfile.jar"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			left := validMRPackTestFile("left.jar")
			left.Path = "mods/" + testCase.left
			right := validMRPackTestFile("right.jar")
			right.Path = "mods/" + testCase.right
			_, err := buildMRPack(mrpackBuildInput{
				Name: "BUG-070 pack", VersionID: "1.0.0", MinecraftVersion: "1.21.1",
				Loader: "fabric", LoaderVersion: "0.16.14", Files: []mrpackFile{left, right},
			})
			if err == nil || !strings.Contains(err.Error(), "portable file path") {
				t.Fatalf("portable collision %q/%q result = %v", testCase.left, testCase.right, err)
			}
		})
	}
}
