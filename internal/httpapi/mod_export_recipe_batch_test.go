package httpapi

import "testing"

func TestUniqueRecipeImportCandidateResourcesDeduplicatesRepeatedIngredients(t *testing.T) {
	candidates := []recipeImportCandidateWrite{
		{
			ResourceIdentity: "res_identity", ResourcePublicID: "abc234def",
			ResourceCanonicalID: "minecraft:stick", ResourceNamespace: "minecraft",
			ResourcePath: "stick", KindCode: "minecraft.item",
		},
		{
			ResourceIdentity: "res_identity", ResourcePublicID: "different",
			ResourceCanonicalID: "minecraft:stick", ResourceNamespace: "minecraft",
			ResourcePath: "stick", KindCode: "minecraft.item",
		},
	}
	resources, err := uniqueRecipeImportCandidateResources(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected one staged resource, got %d", len(resources))
	}
	if resources[0].PublicID != "abc234def" || resources[0].CanonicalID != "minecraft:stick" {
		t.Fatalf("unexpected staged resource: %#v", resources[0])
	}
}

func TestUniqueRecipeImportCandidateResourcesRejectsIdentityCollision(t *testing.T) {
	_, err := uniqueRecipeImportCandidateResources([]recipeImportCandidateWrite{
		{ResourceIdentity: "same", ResourceCanonicalID: "minecraft:stick", KindCode: "minecraft.item"},
		{ResourceIdentity: "same", ResourceCanonicalID: "minecraft:stone", KindCode: "minecraft.item"},
	})
	if err == nil {
		t.Fatal("expected identity collision to be rejected")
	}
}
