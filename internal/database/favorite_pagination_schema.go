package database

func favoritePaginationSchemaStatements() []string {
	return []string{
		`create index idx_favorite_collections_user_page
			on favorite_collections(user_id,is_default desc,created_at,id)`,
		`create index idx_favorite_collections_public_page
			on favorite_collections(user_id,is_default desc,created_at,id) where is_public`,
		`create index idx_favorite_items_collection_page
			on favorite_collection_items(collection_id,created_at desc,id desc)`,
		`create index idx_favorite_items_target_collection
			on favorite_collection_items(entity_type,entity_id,collection_id)`,
	}
}
