package activitycatalog

import "strings"

const (
	ActionEdit     int16 = 1
	ActionCreate   int16 = 2
	ActionView     int16 = 3
	ActionDelete   int16 = 4
	ActionClaim    int16 = 5
	ActionDownload int16 = 6
	ActionUpload   int16 = 7
	ActionPurchase int16 = 8
	ActionTransfer int16 = 9
	ActionCheckIn  int16 = 10
	ActionUse      int16 = 11
)

const (
	ObjectRecipe        int16 = 1
	ObjectMod           int16 = 2
	ObjectBlueprint     int16 = 3
	ObjectPlugin        int16 = 4
	ObjectAuthor        int16 = 5
	ObjectTeam          int16 = 6
	ObjectUser          int16 = 7
	ObjectComment       int16 = 8
	ObjectTag           int16 = 9
	ObjectFile          int16 = 10
	ObjectEconomy       int16 = 11
	ObjectTask          int16 = 12
	ObjectShopItem      int16 = 13
	ObjectResource      int16 = 14
	ObjectModpack       int16 = 15
	ObjectServer        int16 = 16
	ObjectMap           int16 = 17
	ObjectResourcePack  int16 = 18
	ObjectShaderPack    int16 = 19
	ObjectDatapack      int16 = 20
	ObjectAddon         int16 = 21
	ObjectCommunityPost int16 = 22
	ObjectReview        int16 = 23
	ObjectSkin          int16 = 24
	ObjectPlayerProfile int16 = 25
	ObjectChangelog     int16 = 26
	ObjectRating        int16 = 27
)

// Entry is the single code/ID/name authority shared by Schema installation,
// retention filters, activity recording, and progression task matching.
type Entry struct {
	ID   int16
	Code string
	Name string
}

var actionDefinitions = []Entry{
	{ID: ActionEdit, Code: "edit", Name: "Edit"},
	{ID: ActionCreate, Code: "create", Name: "Create"},
	{ID: ActionView, Code: "view", Name: "View"},
	{ID: ActionDelete, Code: "delete", Name: "Delete"},
	{ID: ActionClaim, Code: "claim", Name: "Claim"},
	{ID: ActionDownload, Code: "download", Name: "Download"},
	{ID: ActionUpload, Code: "upload", Name: "Upload"},
	{ID: ActionPurchase, Code: "purchase", Name: "Purchase"},
	{ID: ActionTransfer, Code: "transfer", Name: "Transfer"},
	{ID: ActionCheckIn, Code: "checkin", Name: "Check in"},
	{ID: ActionUse, Code: "use", Name: "Use"},
}

var objectTypeDefinitions = []Entry{
	{ID: ObjectRecipe, Code: "recipe", Name: "Recipe"},
	{ID: ObjectMod, Code: "mod", Name: "Mod"},
	{ID: ObjectBlueprint, Code: "blueprint", Name: "Blueprint"},
	{ID: ObjectPlugin, Code: "plugin", Name: "Plugin"},
	{ID: ObjectAuthor, Code: "author", Name: "Author"},
	{ID: ObjectTeam, Code: "team", Name: "Team"},
	{ID: ObjectUser, Code: "user", Name: "User"},
	{ID: ObjectComment, Code: "comment", Name: "Comment"},
	{ID: ObjectTag, Code: "tag", Name: "Tag"},
	{ID: ObjectFile, Code: "file", Name: "File"},
	{ID: ObjectEconomy, Code: "economy", Name: "Economy"},
	{ID: ObjectTask, Code: "task", Name: "Task"},
	{ID: ObjectShopItem, Code: "shop_item", Name: "Shop item"},
	{ID: ObjectResource, Code: "resource", Name: "Resource"},
	{ID: ObjectModpack, Code: "modpack", Name: "Modpack"},
	{ID: ObjectServer, Code: "server", Name: "Server"},
	{ID: ObjectMap, Code: "map", Name: "Map"},
	{ID: ObjectResourcePack, Code: "resource_pack", Name: "Resource pack"},
	{ID: ObjectShaderPack, Code: "shader_pack", Name: "Shader pack"},
	{ID: ObjectDatapack, Code: "datapack", Name: "Data pack"},
	{ID: ObjectAddon, Code: "addon", Name: "Addon"},
	{ID: ObjectCommunityPost, Code: "community_post", Name: "Community post"},
	{ID: ObjectReview, Code: "review", Name: "Review"},
	{ID: ObjectSkin, Code: "skin", Name: "Skin"},
	{ID: ObjectPlayerProfile, Code: "player_profile", Name: "Player profile"},
	{ID: ObjectChangelog, Code: "changelog", Name: "Changelog"},
	{ID: ObjectRating, Code: "rating", Name: "Rating"},
}

var actionIDs = dictionaryIDs(actionDefinitions)
var objectTypeIDs = dictionaryIDs(objectTypeDefinitions)

func dictionaryIDs(definitions []Entry) map[string]int16 {
	result := make(map[string]int16, len(definitions))
	for _, definition := range definitions {
		result[definition.Code] = definition.ID
	}
	return result
}

func definitionsCopy(definitions []Entry) []Entry { return append([]Entry(nil), definitions...) }

func idsCopy(values map[string]int16) map[string]int16 {
	result := make(map[string]int16, len(values))
	for code, id := range values {
		result[code] = id
	}
	return result
}

func ActionDefinitions() []Entry { return definitionsCopy(actionDefinitions) }

func ObjectTypeDefinitions() []Entry { return definitionsCopy(objectTypeDefinitions) }

func ActionIDs() map[string]int16 { return idsCopy(actionIDs) }

func ObjectTypeIDs() map[string]int16 { return idsCopy(objectTypeIDs) }

func ActionID(code string) int16 { return actionIDs[strings.ToLower(strings.TrimSpace(code))] }

func ObjectTypeID(code string) int16 { return objectTypeIDs[strings.ToLower(strings.TrimSpace(code))] }
