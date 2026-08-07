package httpapi

import (
	"strings"
	"testing"
)

func TestRatingDimensionsCoverEverySupportedTarget(t *testing.T) {
	expectedCounts := map[string]int{
		"mod": 8, "modpack": 8, "plugin": 8, "addon": 8,
		"shader_pack": 7, "resource_pack": 7, "datapack": 8, "map": 8,
		"minecraft_server": 8,
	}
	if len(ratingDimensions) != len(expectedCounts) {
		t.Fatalf("rating target count = %d, want %d", len(ratingDimensions), len(expectedCounts))
	}
	for targetType, count := range expectedCounts {
		dimensions := ratingDimensions[targetType]
		if len(dimensions) != count {
			t.Errorf("%s dimension count = %d, want %d", targetType, len(dimensions), count)
		}
		seen := make(map[string]struct{}, len(dimensions))
		for _, dimension := range dimensions {
			if _, exists := seen[dimension.Code]; exists {
				t.Errorf("%s contains duplicate dimension %q", targetType, dimension.Code)
			}
			seen[dimension.Code] = struct{}{}
		}
	}
}

func TestNormalizeRatingTargetType(t *testing.T) {
	tests := map[string]string{
		"server": "minecraft_server", "minecraft-server": "minecraft_server",
		"resource-pack": "resource_pack", "shader": "shader_pack",
		" MOD ": "mod", "unknown": "",
	}
	for input, expected := range tests {
		if actual := normalizeRatingTargetType(input); actual != expected {
			t.Errorf("normalizeRatingTargetType(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestValidateRatingRequestRequiresExactDimensionSet(t *testing.T) {
	dimensions := ratingDimensions["mod"]
	scores := make(map[string]int, len(dimensions))
	for _, dimension := range dimensions {
		scores[dimension.Code] = 4
	}
	valid := ratingUpsertRequest{OverallScore: 5, Scores: scores, Message: "useful review"}
	if err := validateRatingRequest("mod", valid); err != nil {
		t.Fatalf("valid rating rejected: %v", err)
	}

	missing := ratingUpsertRequest{OverallScore: 5, Scores: cloneRatingScores(scores)}
	delete(missing.Scores, dimensions[0].Code)
	if err := validateRatingRequest("mod", missing); err == nil {
		t.Fatal("rating with a missing dimension was accepted")
	}

	extra := ratingUpsertRequest{OverallScore: 5, Scores: cloneRatingScores(scores)}
	extra.Scores["unexpected"] = 3
	if err := validateRatingRequest("mod", extra); err == nil {
		t.Fatal("rating with an unsupported dimension was accepted")
	}

	invalidScore := ratingUpsertRequest{OverallScore: 5, Scores: cloneRatingScores(scores)}
	invalidScore.Scores[dimensions[0].Code] = 0
	if err := validateRatingRequest("mod", invalidScore); err == nil {
		t.Fatal("rating with an out-of-range dimension score was accepted")
	}

	tooLong := valid
	tooLong.Message = strings.Repeat("评", ratingMessageLimit+1)
	if err := validateRatingRequest("mod", tooLong); err == nil {
		t.Fatal("rating with an overlong Unicode message was accepted")
	}
}

func cloneRatingScores(source map[string]int) map[string]int {
	result := make(map[string]int, len(source))
	for code, score := range source {
		result[code] = score
	}
	return result
}

func TestEffectivePromotionPowerDiminishesRepeatedUse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		used int
		want float64
	}{{0, 1}, {1, 1 / 1.2}, {5, 0.5}}
	for _, test := range tests {
		if got := effectivePromotionPower(1, test.used); got != test.want {
			t.Errorf("effectivePromotionPower(1, %d) = %v, want %v", test.used, got, test.want)
		}
	}
}
