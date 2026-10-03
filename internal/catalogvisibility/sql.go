package catalogvisibility

import "fmt"

// Only code-owned SQL aliases and entity types are accepted here. This is a
// predicate builder for public projections, not a substitute for route ACLs.
func EntitySQL(alias, entityType string) string {
	var canonical, observation, identity string
	switch entityType {
	case "resource", "structure", "document":
		canonical = `exists(select 1 from catalog_resource_definitions public_definition where public_definition.resource_id=` + alias + `.id)`
		observation = `exists(select 1 from resource_import_snapshots public_snapshot
		 join catalog_import_revisions public_revision on public_revision.id=public_snapshot.revision_id
		 join mods public_mod on public_mod.id=public_revision.mod_id
		 where public_snapshot.resource_id=` + alias + `.id and public_revision.is_active
		 and public_revision.status in ('ready','partial') and public_mod.review_status='approved')
		 or exists(select 1 from mod_resource_version_details public_detail
		 join mod_content_versions public_version on public_version.id=public_detail.version_id
		 join mods public_mod on public_mod.id=public_version.mod_id
		 where public_detail.resource_id=` + alias + `.id and public_detail.status='active'
		 and public_version.status='active' and public_mod.review_status='approved')`
		identity = `exists(select 1 from game_resources public_resource where public_resource.entity_id=` + alias + `.id
		 and public_resource.created_from_revision_id is null
		 and not exists(select 1 from resource_import_snapshots private_snapshot where private_snapshot.resource_id=` + alias + `.id)
		 and not exists(select 1 from mod_resource_version_details private_detail where private_detail.resource_id=` + alias + `.id)
		 and (public_resource.owner_mod_id is null or exists(select 1 from mods public_owner
		 where public_owner.id=public_resource.owner_mod_id and public_owner.review_status='approved')))`
	case "recipe":
		canonical = `exists(select 1 from recipe_definitions public_definition where public_definition.recipe_id=` + alias + `.id)`
		observation = snapshotSQL(alias, "recipe_import_snapshots", "recipe_id")
		identity = `exists(select 1 from recipes public_recipe where public_recipe.entity_id=` + alias + `.id
		 and public_recipe.identity_source='manual'
		 and not exists(select 1 from recipe_import_snapshots private_snapshot where private_snapshot.recipe_id=` + alias + `.id))`
	case "recipe_type":
		canonical = `exists(select 1 from recipe_type_definitions public_definition where public_definition.recipe_type_id=` + alias + `.id)`
		observation = snapshotSQL(alias, "recipe_type_import_snapshots", "recipe_type_id")
		identity = `not exists(select 1 from recipe_type_import_snapshots private_snapshot where private_snapshot.recipe_type_id=` + alias + `.id)`
	case "tag":
		canonical = alias + `.published_revision_id is not null`
		observation = snapshotSQL(alias, "tag_import_snapshots", "tag_id")
		identity = `not exists(select 1 from tag_import_snapshots private_snapshot where private_snapshot.tag_id=` + alias + `.id)`
	case "recipe_template":
		canonical = alias + `.published_revision_id is not null`
		observation = `exists(select 1 from recipe_layout_templates public_template
		 join recipe_template_import_snapshots public_snapshot on public_snapshot.id=public_template.import_snapshot_id
		 join catalog_import_revisions public_revision on public_revision.id=public_snapshot.revision_id
		 join mods public_mod on public_mod.id=public_revision.mod_id
		 where public_template.entity_id=` + alias + `.id and public_revision.is_active
		 and public_revision.status in ('ready','partial') and public_mod.review_status='approved')`
		identity = `exists(select 1 from recipe_layout_templates public_template where public_template.entity_id=` + alias + `.id
		 and public_template.import_snapshot_id is null)`
	default:
		panic("unsupported public catalog entity type")
	}
	return fmt.Sprintf("(%s or %s or %s)", canonical, observation, identity)
}

func snapshotSQL(alias, table, column string) string {
	return `exists(select 1 from ` + table + ` public_snapshot
	 join catalog_import_revisions public_revision on public_revision.id=public_snapshot.revision_id
	 join mods public_mod on public_mod.id=public_revision.mod_id
	 where public_snapshot.` + column + `=` + alias + `.id and public_revision.is_active
	 and public_revision.status in ('ready','partial') and public_mod.review_status='approved')`
}
