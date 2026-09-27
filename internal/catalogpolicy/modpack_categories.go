package catalogpolicy

var modpackCategories = [...]string{
	"technology",
	"magic",
	"adventure",
	"building",
	"map",
	"quests",
	"optimization",
	"hardcore",
	"casual",
	"large",
	"lightweight",
	"story",
	"kitchen_sink",
	"skyblock",
	"pvp",
	"chinese",
}

var modpackCategorySet = func() map[string]struct{} {
	result := make(map[string]struct{}, len(modpackCategories))
	for _, category := range modpackCategories {
		result[category] = struct{}{}
	}
	return result
}()

// ModpackCategories returns a copy of the product-supported category registry.
// The same registry drives request validation and the database CHECK constraint.
func ModpackCategories() []string {
	return append([]string(nil), modpackCategories[:]...)
}

func IsModpackCategory(category string) bool {
	_, ok := modpackCategorySet[category]
	return ok
}
