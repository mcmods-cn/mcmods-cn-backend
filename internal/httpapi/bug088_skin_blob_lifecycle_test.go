package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestSharedMinecraftTextureBlobsHaveSystemOwnershipAndARecoverableLifecycle(t *testing.T) {
	t.Parallel()
	textureRaw, err := os.ReadFile("skin_texture.go")
	if err != nil {
		t.Fatal(err)
	}
	textureSource := strings.ToLower(string(textureRaw))
	for _, required := range []string{
		"minecraft_texture_derived",
		"source_size_bytes,sha256,status,scan_status",
		"'image/png',$7,0,$8,'active','trusted_generated'",
		"newminecraftuuid()",
		"tombstoneunreferencedminecrafttextureblobtx",
		"active_reference_count",
		"minecraft-texture-transaction-aborted",
		"pg_advisory_xact_lock_shared",
		"lockminecrafttexturelifecyclestore",
	} {
		if !strings.Contains(textureSource, strings.ToLower(required)) {
			t.Errorf("shared texture lifecycle is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"source_size_bytes,sha256,uploader_id,status,scan_status",
		"len(texture.data), computedhash, ownerid",
		"objectkey := path.join(ossobjectprefix(cfg.prefix, texturecategory), computedhash+\".png\")",
	} {
		if strings.Contains(textureSource, forbidden) {
			t.Errorf("shared texture lifecycle retains user attribution or a reusable key: %q", forbidden)
		}
	}

	handlersRaw, err := os.ReadFile("skin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handlersSource := strings.ToLower(string(handlersRaw))
	for _, required := range []string{
		"softdeleteskinassettx",
		"pendingtextureupload",
		"compensateuncommittedminecrafttextureupload",
	} {
		if !strings.Contains(handlersSource, required) {
			t.Errorf("skin deletion/upload transaction boundary is missing %q", required)
		}
	}

	maintenanceRaw, err := os.ReadFile("maintenance_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	maintenanceSource := strings.ToLower(string(maintenanceRaw))
	for _, required := range []string{
		"pruneskintextureblobs",
		"idx_skin_texture_blobs_unreferenced",
		"active_reference_count=0",
	} {
		if !strings.Contains(maintenanceSource, required) {
			t.Errorf("skin blob calibration worker is missing %q", required)
		}
	}
	governanceRaw, err := os.ReadFile("governance_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(governanceRaw)), "softdeleteskinassettx") {
		t.Fatal("moderation deletion bypasses the shared skin/blob lifecycle")
	}
}
