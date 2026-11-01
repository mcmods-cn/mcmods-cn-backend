package httpapi

import "testing"

func TestAuditTP004ActualHTTPPostgresWorkerManifestTerminalMatrixIntegration(t *testing.T) {
	for _, test := range []auditCFWorkerCase{
		{name: "valid-format1-positive-control", success: true},
		{name: "original-wrong-type-and-negative-version", mutate: func(m map[string]any) { m["manifestType"], m["manifestVersion"] = "not-a-minecraft-modpack", -1 }, errorText: "unsupported CurseForge manifest type or version (expected minecraftModpack version 1)"},
		{name: "wrong-type", mutate: func(m map[string]any) { m["manifestType"] = "not-a-minecraft-modpack" }, errorText: "unsupported CurseForge manifest type or version (expected minecraftModpack version 1)"},
		{name: "zero-version", mutate: func(m map[string]any) { m["manifestVersion"] = 0 }, errorText: "unsupported CurseForge manifest type or version (expected minecraftModpack version 1)"},
		{name: "negative-version", mutate: func(m map[string]any) { m["manifestVersion"] = -1 }, errorText: "unsupported CurseForge manifest type or version (expected minecraftModpack version 1)"},
		{name: "unknown-version", mutate: func(m map[string]any) { m["manifestVersion"] = 2 }, errorText: "unsupported CurseForge manifest type or version (expected minecraftModpack version 1)"},
		{name: "missing-minecraft", mutate: func(m map[string]any) { delete(m, "minecraft") }, errorText: "CurseForge manifest requires a Minecraft version and mod loader"},
		{name: "missing-loaders", mutate: func(m map[string]any) { m["minecraft"] = map[string]any{"version": "1.20.1"} }, errorText: "CurseForge manifest requires a Minecraft version and mod loader"},
		{name: "empty-loader", mutate: func(m map[string]any) {
			m["minecraft"] = map[string]any{"version": "1.20.1", "modLoaders": []map[string]string{{"id": ""}}}
		}, errorText: "CurseForge manifest contains an empty mod loader"},
		{name: "invalid-project-reference", mutate: func(m map[string]any) { m["files"] = []map[string]any{{"projectID": 0, "fileID": 33}} }, errorText: "CurseForge manifest contains an invalid project or file reference"},
		{name: "invalid-file-reference", mutate: func(m map[string]any) { m["files"] = []map[string]any{{"projectID": 22, "fileID": -1}} }, errorText: "CurseForge manifest contains an invalid project or file reference"},
		{name: "conflicting-references", mutate: func(m map[string]any) {
			m["files"] = []map[string]any{{"projectID": 22, "fileID": 33}, {"projectID": 22, "fileID": 34}}
		}, errorText: "CurseForge manifest contains conflicting project references"},
	} {
		test.mode, test.pages, test.archive = "page2", 2, true
		t.Run(test.name, func(t *testing.T) { auditRunCFWorkerCase(t, test) })
	}
}
