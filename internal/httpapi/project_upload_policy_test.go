package httpapi

import "testing"

func TestOSSProjectDownloadUploadsMatchProjectReleaseFormats(t *testing.T) {
	for _, test := range []struct {
		projectType string
		extension   string
		allowed     bool
	}{
		{"mod", ".jar", true}, {"mod", ".zip", false},
		{"plugin", ".jar", true}, {"plugin", ".zip", false},
		{"modpack", ".mrpack", true}, {"modpack", ".zip", true}, {"modpack", ".jar", false},
		{"map", ".zip", true}, {"map", ".jar", false},
		{"resource_pack", ".zip", true}, {"shader_pack", ".zip", true},
		{"datapack", ".zip", true}, {"addon", ".jar", true}, {"addon", ".zip", true},
	} {
		t.Run(test.projectType+test.extension, func(t *testing.T) {
			scope := ossProjectDownloadScope(test.projectType, "abc123xyz")
			if got := ossUploadExtensionAllowed(scope, test.extension, nil); got != test.allowed {
				t.Fatalf("release upload %s %s allowed=%v want=%v", test.projectType, test.extension, got, test.allowed)
			}
		})
	}
}

func TestOSSUploadExtensionPolicyPreservesDedicatedScopes(t *testing.T) {
	for _, test := range []struct {
		scope     string
		extension string
		allowed   bool
	}{
		{ossModExportScopePrefix + "abc123xyz", ".zip", true},
		{ossModExportScopePrefix + "abc123xyz", ".jar", false},
		{ossModCatalogScopePrefix + "abc123xyz", ".json", true},
		{ossModCatalogScopePrefix + "abc123xyz", ".zip", false},
		{ossReportEvidenceScope, ".pdf", true},
		{ossReportEvidenceScope, ".jar", false},
		{"user", ".png", true}, {"user", ".exe", false},
	} {
		if got := ossUploadExtensionAllowed(test.scope, test.extension, []string{".png"}); got != test.allowed {
			t.Fatalf("upload %s %s allowed=%v want=%v", test.scope, test.extension, got, test.allowed)
		}
	}
}
