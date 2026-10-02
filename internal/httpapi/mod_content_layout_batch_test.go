package httpapi

import (
	"fmt"
	"testing"
)

func TestModContentLayoutGeneratedIDsMapMaximumCategoriesAndGroups(t *testing.T) {
	t.Parallel()
	temporaryCategoryIDs := make([]string, maxModContentCategories)
	temporaryGroupIDs := make([]string, maxModContentResources/2)
	generatedIDs := make([]string, len(temporaryCategoryIDs)+len(temporaryGroupIDs))
	edit := modContentLayoutEdit{
		RootSectionPublicID: "root00001",
		Categories:          make([]modContentLayoutCategoryEdit, len(temporaryCategoryIDs)),
		Resources:           make([]modContentLayoutResourceEdit, maxModContentResources),
	}
	for index := range temporaryCategoryIDs {
		temporaryCategoryIDs[index] = fmt.Sprintf("temporary-category-%04d", index)
		edit.Categories[index] = modContentLayoutCategoryEdit{
			PublicID:       temporaryCategoryIDs[index],
			ParentPublicID: edit.RootSectionPublicID,
		}
	}
	for index := range temporaryGroupIDs {
		temporaryGroupIDs[index] = fmt.Sprintf("temporary-group-%05d", index)
	}
	for index := range generatedIDs {
		generatedIDs[index] = fmt.Sprintf("g%08d", index+1)
	}
	for index := range edit.Resources {
		edit.Resources[index] = modContentLayoutResourceEdit{
			ResourcePublicID: fmt.Sprintf("res%06d", index),
			SectionPublicID:  temporaryCategoryIDs[index%len(temporaryCategoryIDs)],
			SimilarGroupID:   temporaryGroupIDs[index/2],
		}
	}
	if err := applyModContentLayoutGeneratedIDs(&edit, temporaryCategoryIDs, temporaryGroupIDs, generatedIDs); err != nil {
		t.Fatal(err)
	}
	if edit.Categories[0].PublicID != generatedIDs[0] ||
		edit.Categories[len(edit.Categories)-1].PublicID != generatedIDs[len(temporaryCategoryIDs)-1] {
		t.Fatalf("category replacements did not preserve input order: first=%q last=%q",
			edit.Categories[0].PublicID, edit.Categories[len(edit.Categories)-1].PublicID)
	}
	for index, resource := range edit.Resources {
		wantSection := generatedIDs[index%len(temporaryCategoryIDs)]
		wantGroup := generatedIDs[len(temporaryCategoryIDs)+index/2]
		if resource.SectionPublicID != wantSection || resource.SimilarGroupID != wantGroup {
			t.Fatalf("resource %d replacement section/group=%q/%q want %q/%q",
				index, resource.SectionPublicID, resource.SimilarGroupID, wantSection, wantGroup)
		}
	}
	if err := applyModContentLayoutGeneratedIDs(&edit, temporaryCategoryIDs, temporaryGroupIDs, generatedIDs[:len(generatedIDs)-1]); err == nil {
		t.Fatal("mismatched generated ID count was accepted")
	}
}
