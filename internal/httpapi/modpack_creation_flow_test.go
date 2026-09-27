package httpapi

import "testing"

func TestApprovedModpackCreationSkipsPreRevisionAssociationProjection(t *testing.T) {
	if modpackCreationNeedsPreviewAssociations("approved") {
		t.Fatal("approved creation would write associations before its published revision exists")
	}
	if !modpackCreationNeedsPreviewAssociations("pending") {
		t.Fatal("pending creation lost its submitter-visible association projection")
	}
}

func TestApprovedModpackCreationDecisionCoversMaximumModList(t *testing.T) {
	snapshot := perf020ModpackSnapshot("perf020-boundary", 2000)
	if err := normalizeAndValidateModpackRequest(&snapshot); err != nil {
		t.Fatalf("2,000-mod boundary snapshot was rejected: %v", err)
	}
	if len(snapshot.Mods) != 2000 || modpackCreationNeedsPreviewAssociations("approved") {
		t.Fatalf("approved maximum snapshot retained mods=%d or selected the preview write path", len(snapshot.Mods))
	}
	snapshot = perf020ModpackSnapshot("perf020-over-boundary", 2001)
	if err := normalizeAndValidateModpackRequest(&snapshot); err == nil {
		t.Fatal("2,001-mod snapshot was accepted")
	}
}
