package searchindex

import (
	"context"
	"fmt"
)

// Increment when an indexed field or document shape changes after deployment.
// The worker will build new collections and switch aliases without downtime.
// Version 5 removes unpublished import names and associated mod metadata from
// public search, and retains version 4's optional-empty-field correction.
// The collection shape and public search contract are stable.
const projectionSchemaVersion = 5

type searchDocumentLoader func(context.Context, *Worker, []int64) ([]map[string]any, error)

type searchDocumentRegistration struct {
	documentType string
	idPageQuery  string
	load         searchDocumentLoader
}

type searchCollectionRegistration struct {
	kind      string
	schema    CollectionSchema
	documents []searchDocumentRegistration
}

// searchRegistry is the single authority for collection schemas, document
// types, stable ID scans, collection routing, and projection loaders.
var searchRegistry = []searchCollectionRegistration{
	{
		kind: "projects",
		schema: CollectionSchema{
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
				{Name: "submitted_by", Type: "int64", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		documents: []searchDocumentRegistration{
			{
				documentType: "mod",
				idPageQuery:  `select document.id from mods document where document.id>$1 order by document.id limit $2`,
				load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
					return worker.loadProjectDocuments(ctx, ids, "mod")
				},
			},
			{
				documentType: "modpack",
				idPageQuery:  `select document.id from modpacks document where document.id>$1 order by document.id limit $2`,
				load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
					return worker.loadProjectDocuments(ctx, ids, "modpack")
				},
			},
			{
				documentType: "simple_project",
				idPageQuery:  `select document.id from simple_projects document where document.id>$1 order by document.id limit $2`,
				load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
					return worker.loadProjectDocuments(ctx, ids, "simple_project")
				},
			},
		},
	},
	{
		kind: "community",
		schema: CollectionSchema{
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
		documents: []searchDocumentRegistration{{
			documentType: "community_post",
			idPageQuery:  `select document.id from community_posts document where document.id>$1 order by document.id limit $2`,
			load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
				return worker.loadCommunityDocuments(ctx, ids)
			},
		}},
	},
	{
		kind: "creators",
		schema: CollectionSchema{
			Fields: []Field{
				{Name: "kind", Type: "string", Facet: true},
				{Name: "name", Type: "string", Infix: true},
				{Name: "text", Type: "string[]"},
				{Name: "review_status", Type: "string", Facet: true},
				{Name: "created_by", Type: "int64", Facet: true},
				{Name: "claimed_user_id", Type: "int64", Facet: true},
				{Name: "updated_at", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		documents: []searchDocumentRegistration{{
			documentType: "creator",
			idPageQuery:  `select document.id from creators document where document.id>$1 order by document.id limit $2`,
			load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
				return worker.loadCreatorDocuments(ctx, ids)
			},
		}},
	},
	{
		kind: "resources",
		schema: CollectionSchema{
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
		documents: []searchDocumentRegistration{{
			documentType: "resource",
			idPageQuery:  `select document.entity_id from game_resources document where document.entity_id>$1 order by document.entity_id limit $2`,
			load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
				return worker.loadResourceDocuments(ctx, ids)
			},
		}},
	},
	{
		kind: "servers",
		schema: CollectionSchema{
			Fields: []Field{
				{Name: "internal_id", Type: "int64"},
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
				{Name: "created_at", Type: "int64"},
				{Name: "updated_at", Type: "int64"},
				{Name: "heat_sort_asc", Type: "int64"},
				{Name: "heat_sort_desc", Type: "int64"},
				{Name: "download_count", Type: "int64"},
				{Name: "favorite_count", Type: "int64"},
				{Name: "rating_score", Type: "int64"},
				{Name: "rating_count", Type: "int64"},
				{Name: "view_count", Type: "int64"},
				{Name: "comment_count", Type: "int64"},
			},
			DefaultSortingField: "updated_at",
		},
		documents: []searchDocumentRegistration{{
			documentType: "server",
			idPageQuery:  `select document.id from minecraft_servers document where document.id>$1 order by document.id limit $2`,
			load: func(ctx context.Context, worker *Worker, ids []int64) ([]map[string]any, error) {
				return worker.loadServerDocuments(ctx, ids)
			},
		}},
	},
}

func collectionSchemas() map[string]CollectionSchema {
	schemas := make(map[string]CollectionSchema, len(searchRegistry))
	for _, registration := range searchRegistry {
		schemas[registration.kind] = registration.schema
	}
	return schemas
}

func registeredCollection(kind string) (searchCollectionRegistration, error) {
	for _, registration := range searchRegistry {
		if registration.kind == kind {
			return registration, nil
		}
	}
	return searchCollectionRegistration{}, fmt.Errorf("unknown search collection %q", kind)
}

func registeredDocument(documentType string) (searchCollectionRegistration, searchDocumentRegistration, error) {
	for _, collection := range searchRegistry {
		for _, document := range collection.documents {
			if document.documentType == documentType {
				return collection, document, nil
			}
		}
	}
	return searchCollectionRegistration{}, searchDocumentRegistration{},
		fmt.Errorf("unknown search document type %q", documentType)
}
