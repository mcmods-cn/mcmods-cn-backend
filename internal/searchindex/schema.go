package searchindex

// Increment when an indexed field or document shape changes after deployment.
// The worker will build new collections and switch aliases without downtime.
const projectionSchemaVersion = 1

func collectionSchemas() map[string]CollectionSchema {
	return map[string]CollectionSchema{
		"projects": {
			Fields: []Field{
				{Name: "entity_type", Type: "string", Facet: true},
				{Name: "public_id", Type: "string", Infix: true},
				{Name: "slug", Type: "string", Infix: true},
				{Name: "names", Type: "string[]", Infix: true},
				{Name: "text", Type: "string[]"},
				{Name: "identifiers", Type: "string[]", Infix: true},
				{Name: "creators", Type: "string[]", Infix: true},
				{Name: "keywords", Type: "string[]", Infix: true},
				{Name: "categories", Type: "string[]", Facet: true},
				{Name: "minecraft_versions", Type: "string[]", Facet: true},
				{Name: "loaders", Type: "string[]", Facet: true},
				{Name: "review_status", Type: "string", Facet: true},
				{Name: "created_by", Type: "int64", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		"community": {
			Fields: []Field{
				{Name: "kind", Type: "string", Facet: true},
				{Name: "title", Type: "string[]", Infix: true},
				{Name: "body", Type: "string[]"},
				{Name: "project_ids", Type: "string[]", Facet: true},
				{Name: "resource_ids", Type: "string[]", Facet: true},
				{Name: "minecraft_versions", Type: "string[]", Facet: true},
				{Name: "review_status", Type: "string", Facet: true},
				{Name: "status", Type: "string", Facet: true},
				{Name: "author_id", Type: "int64", Facet: true},
				{Name: "published_at", Type: "int64"},
			},
			DefaultSortingField: "published_at",
		},
		"creators": {
			Fields: []Field{
				{Name: "kind", Type: "string", Facet: true},
				{Name: "name", Type: "string", Infix: true},
				{Name: "text", Type: "string[]"},
				{Name: "review_status", Type: "string", Facet: true},
				{Name: "created_by", Type: "int64", Facet: true},
				{Name: "claimed_by", Type: "int64", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		"resources": {
			Fields: []Field{
				{Name: "public_id", Type: "string", Infix: true},
				{Name: "kind_code", Type: "string", Facet: true},
				{Name: "namespace", Type: "string", Facet: true},
				{Name: "canonical_id", Type: "string", Infix: true},
				{Name: "names", Type: "string[]", Infix: true},
				{Name: "status", Type: "string", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		"servers": {
			Fields: []Field{
				{Name: "public_id", Type: "string", Infix: true},
				{Name: "name", Type: "string", Infix: true},
				{Name: "text", Type: "string[]"},
				{Name: "mods", Type: "string[]", Infix: true, Facet: true},
				{Name: "primary_tag", Type: "string", Facet: true},
				{Name: "languages", Type: "string[]", Facet: true},
				{Name: "minecraft_versions", Type: "string[]", Facet: true},
				{Name: "loader", Type: "string", Facet: true},
				{Name: "modded", Type: "bool", Facet: true},
				{Name: "online", Type: "bool", Facet: true},
				{Name: "whitelist", Type: "bool", Facet: true},
				{Name: "online_mode", Type: "bool", Facet: true},
				{Name: "review_status", Type: "string", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
	}
}
