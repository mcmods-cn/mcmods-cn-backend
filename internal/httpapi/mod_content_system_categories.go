package httpapi

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ensureItemBlockSystemCategoriesTx materializes the two initial branches of
// an item/block documentation page. They are ordinary content sections after
// creation, so the layout editor can rename them and move resources or custom
// child categories below them.
func ensureItemBlockSystemCategoriesTx(
	ctx context.Context,
	tx pgx.Tx,
	rootID, versionID, modID, templateID, actorID int64,
	defaultLocale, displayMode string,
) error {
	if _, err := tx.Exec(ctx, `with requested(system_key,requested_ordinal) as (
			values ('blocks'::text,0),('items'::text,1)
		), missing as (
			select requested.system_key,requested.requested_ordinal
			from requested
			where not exists (
				select 1 from mod_content_sections child
				where child.version_id=$2 and child.parent_id=$1
				  and child.system_key=requested.system_key and child.status='active'
			)
		), offset_value as (
			select coalesce(max(child.ordinal)+1,0) value
			from mod_content_sections child
			where child.parent_id=$1 and child.status='active'
		)
		insert into mod_content_sections(
			mod_id,version_id,template_id,parent_id,system_key,default_locale,
			display_mode,ordinal,status,created_by,updated_by)
		select $3,$2,$4,$1,missing.system_key,$6,$7,
			offset_value.value+row_number() over(order by missing.requested_ordinal)-1,
			'active',nullif($5,0),nullif($5,0)
		from missing cross join offset_value
		on conflict do nothing`,
		rootID, versionID, modID, templateID, actorID, defaultLocale, displayMode); err != nil {
		return fmt.Errorf("create item and block categories: %w", err)
	}

	if _, err := tx.Exec(ctx, `insert into mod_content_section_localizations(
			section_id,locale,name,description)
		select child.id,localization.locale,localization.name,''
		from mod_content_sections child
		cross join (values
			('blocks'::text,'en-US'::text,'Blocks'::text),
			('blocks','zh-CN','方块'),('blocks','zh-TW','方塊'),
			('items','en-US','Items'),('items','zh-CN','物品'),('items','zh-TW','物品')
		) localization(system_key,locale,name)
		where child.version_id=$2 and child.parent_id=$1
		  and child.status='active' and child.system_key=localization.system_key
		on conflict(section_id,locale) do nothing`, rootID, versionID); err != nil {
		return fmt.Errorf("localize item and block categories: %w", err)
	}
	return nil
}
