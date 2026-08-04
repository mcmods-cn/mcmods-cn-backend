package database

const builtinBlockCompatibilityFields = `[
  {"code":"requiredTier","type":"text","names":{"en-US":"Required tool tier","zh-CN":"最低工具等级","zh-TW":"最低工具等級"},"paths":[["required_tier"]]},
  {"code":"miningTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:block","names":{"en-US":"Mining tool tags","zh-CN":"挖掘工具标签","zh-TW":"挖掘工具標籤"},"paths":[["mining_tags"]]},
  {"code":"tierTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:block","names":{"en-US":"Mining tier tags","zh-CN":"挖掘等级标签","zh-TW":"挖掘等級標籤"},"paths":[["tier_tags"]]},
  {"code":"blockTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:block","names":{"en-US":"Block tags","zh-CN":"方块标签","zh-TW":"方塊標籤"},"paths":[["block_tags"]]},
  {"code":"solid","type":"boolean","names":{"en-US":"Solid","zh-CN":"固体","zh-TW":"固體"},"paths":[["solid"]]},
  {"code":"liquid","type":"boolean","names":{"en-US":"Liquid","zh-CN":"液体","zh-TW":"液體"},"paths":[["liquid"]]},
  {"code":"canOcclude","type":"boolean","names":{"en-US":"Can occlude","zh-CN":"可遮挡","zh-TW":"可遮擋"},"paths":[["can_occlude"],["occlusion"]]},
  {"code":"blocksMotion","type":"boolean","names":{"en-US":"Blocks motion","zh-CN":"阻挡移动","zh-TW":"阻擋移動"},"paths":[["blocks_motion"]]},
  {"code":"renderShape","type":"text","names":{"en-US":"Render shape","zh-CN":"渲染形态","zh-TW":"渲染形態"},"paths":[["render_shape"]]},
  {"code":"hasBlockEntity","type":"boolean","names":{"en-US":"Has block entity","zh-CN":"包含方块实体","zh-TW":"包含方塊實體"},"paths":[["has_block_entity"]]},
  {"code":"randomlyTicking","type":"boolean","names":{"en-US":"Randomly ticking","zh-CN":"随机刻更新","zh-TW":"隨機刻更新"},"paths":[["randomly_ticking"]]},
  {"code":"pistonReaction","type":"text","names":{"en-US":"Piston reaction","zh-CN":"活塞反应","zh-TW":"活塞反應"},"paths":[["piston_reaction"]]},
  {"code":"lootTable","type":"reference","referenceKind":"minecraft.loot_table","names":{"en-US":"Loot table","zh-CN":"战利品表","zh-TW":"戰利品表"},"paths":[["loot_table"]]}
]`

const builtinItemCompatibilityFields = `[
  {"code":"damageable","type":"boolean","names":{"en-US":"Damageable","zh-CN":"可损耗耐久","zh-TW":"可損耗耐久"},"paths":[["damageable"],["durability","damageable"]]},
  {"code":"maxDamage","type":"number","names":{"en-US":"Maximum durability","zh-CN":"最大耐久","zh-TW":"最大耐久"},"paths":[["max_damage"],["durability","max_damage"]]},
  {"code":"primaryType","type":"text","names":{"en-US":"Primary type","zh-CN":"主要类型","zh-TW":"主要類型"},"paths":[["primary_type"]]},
  {"code":"itemTypes","type":"list","names":{"en-US":"Item types","zh-CN":"物品类型","zh-TW":"物品類型"},"paths":[["item_types"]]},
  {"code":"enchantable","type":"boolean","names":{"en-US":"Enchantable","zh-CN":"可附魔","zh-TW":"可附魔"},"paths":[["enchanting","enchantable"]]},
  {"code":"repairItems","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Repair materials","zh-CN":"修复材料","zh-TW":"修復材料"},"paths":[["repair_items"]]}
]`

const builtinToolCompatibilityFields = `[
  {"code":"damageable","type":"boolean","names":{"en-US":"Damageable","zh-CN":"可损耗耐久","zh-TW":"可損耗耐久"},"paths":[["damageable"],["durability","damageable"]]},
  {"code":"maxStackSize","type":"number","names":{"en-US":"Maximum stack size","zh-CN":"最大堆叠数量","zh-TW":"最大堆疊數量"},"paths":[["max_stack_size"],["stack_size"]]},
  {"code":"primaryType","type":"text","names":{"en-US":"Primary type","zh-CN":"主要类型","zh-TW":"主要類型"},"paths":[["primary_type"]]},
  {"code":"itemTypes","type":"list","names":{"en-US":"Item types","zh-CN":"物品类型","zh-TW":"物品類型"},"paths":[["item_types"]]},
  {"code":"tierId","type":"text","names":{"en-US":"Tool tier ID","zh-CN":"工具等级 ID","zh-TW":"工具等級 ID"},"paths":[["tool","tier","id"]]},
  {"code":"tierDurability","type":"number","names":{"en-US":"Tier durability","zh-CN":"工具材料耐久","zh-TW":"工具材料耐久"},"paths":[["tool","tier","durability"]]},
  {"code":"attackDamageBonus","type":"number","names":{"en-US":"Attack damage bonus","zh-CN":"额外攻击伤害","zh-TW":"額外攻擊傷害"},"paths":[["tool","tier","attack_damage_bonus"]]},
  {"code":"attackDamageModifier","type":"number","names":{"en-US":"Attack damage modifier","zh-CN":"攻击伤害修饰值","zh-TW":"攻擊傷害修飾值"},"paths":[["combat","attack_damage_modifier"]]},
  {"code":"attackSpeedModifier","type":"number","names":{"en-US":"Attack speed modifier","zh-CN":"攻击速度修饰值","zh-TW":"攻擊速度修飾值"},"paths":[["combat","attack_speed_modifier"]]},
  {"code":"enchantable","type":"boolean","names":{"en-US":"Enchantable","zh-CN":"可附魔","zh-TW":"可附魔"},"paths":[["enchanting","enchantable"]]},
  {"code":"repairItems","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Repair materials","zh-CN":"修复材料","zh-TW":"修復材料"},"paths":[["tool","tier","repair_items"],["repair_items"]]},
  {"code":"incorrectBlocksForDrops","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:block","names":{"en-US":"Incorrect blocks for drops","zh-CN":"无法正确掉落的方块标签","zh-TW":"無法正確掉落的方塊標籤"},"paths":[["tool","tier","incorrect_blocks_for_drops"],["incorrect_blocks_for_drops"]]}
]`

const builtinEquipmentCompatibilityFields = `[
  {"code":"damageable","type":"boolean","names":{"en-US":"Damageable","zh-CN":"可损耗耐久","zh-TW":"可損耗耐久"},"paths":[["damageable"],["durability","damageable"]]},
  {"code":"maxStackSize","type":"number","names":{"en-US":"Maximum stack size","zh-CN":"最大堆叠数量","zh-TW":"最大堆疊數量"},"paths":[["max_stack_size"],["stack_size"]]},
  {"code":"primaryType","type":"text","names":{"en-US":"Primary type","zh-CN":"主要类型","zh-TW":"主要類型"},"paths":[["primary_type"]]},
  {"code":"itemTypes","type":"list","names":{"en-US":"Item types","zh-CN":"物品类型","zh-TW":"物品類型"},"paths":[["item_types"]]},
  {"code":"enchantable","type":"boolean","names":{"en-US":"Enchantable","zh-CN":"可附魔","zh-TW":"可附魔"},"paths":[["enchanting","enchantable"]]},
  {"code":"itemTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:item","names":{"en-US":"Item tags","zh-CN":"物品标签","zh-TW":"物品標籤"},"paths":[["item_tags"],["tags"]]}
]`

const builtinEntityCompatibilityFields = `[
  {"code":"maxAirSupply","type":"number","names":{"en-US":"Maximum air supply","zh-CN":"最大空气值","zh-TW":"最大空氣值"},"paths":[["max_air_supply"]]},
  {"code":"category","type":"text","names":{"en-US":"Entity category","zh-CN":"生物分类","zh-TW":"生物分類"},"paths":[["category"]]},
  {"code":"mobType","type":"text","names":{"en-US":"Mob type","zh-CN":"生物类型","zh-TW":"生物類型"},"paths":[["mob_type"]]},
  {"code":"living","type":"boolean","names":{"en-US":"Living entity","zh-CN":"生命实体","zh-TW":"生命實體"},"paths":[["living"]]},
  {"code":"mob","type":"boolean","names":{"en-US":"Mob","zh-CN":"生物","zh-TW":"生物"},"paths":[["mob"]]},
  {"code":"animal","type":"boolean","names":{"en-US":"Animal","zh-CN":"动物","zh-TW":"動物"},"paths":[["animal"]]},
  {"code":"hostile","type":"boolean","names":{"en-US":"Hostile","zh-CN":"敌对生物","zh-TW":"敵對生物"},"paths":[["hostile"]]},
  {"code":"tamable","type":"boolean","names":{"en-US":"Tamable","zh-CN":"可驯服","zh-TW":"可馴服"},"paths":[["tamable"]]},
  {"code":"ageable","type":"boolean","names":{"en-US":"Ageable","zh-CN":"可繁殖 / 可成长","zh-TW":"可繁殖 / 可成長"},"paths":[["ageable"]]},
  {"code":"waterAnimal","type":"boolean","names":{"en-US":"Water animal","zh-CN":"水生生物","zh-TW":"水生生物"},"paths":[["water_animal"]]},
  {"code":"canSummon","type":"boolean","names":{"en-US":"Can summon","zh-CN":"可召唤","zh-TW":"可召喚"},"paths":[["can_summon"]]},
  {"code":"canSerialize","type":"boolean","names":{"en-US":"Can serialize","zh-CN":"可序列化","zh-TW":"可序列化"},"paths":[["can_serialize"]]},
  {"code":"trackingRange","type":"number","names":{"en-US":"Client tracking range","zh-CN":"客户端追踪距离","zh-TW":"客戶端追蹤距離"},"paths":[["client_tracking_range"]]},
  {"code":"updateInterval","type":"number","names":{"en-US":"Update interval","zh-CN":"更新间隔","zh-TW":"更新間隔"},"paths":[["update_interval"]]},
  {"code":"runtimePropertiesAvailable","type":"boolean","names":{"en-US":"Runtime properties available","zh-CN":"运行时属性可用","zh-TW":"執行時屬性可用"},"paths":[["runtime_properties_available"]]},
  {"code":"spawnEggCount","type":"number","names":{"en-US":"Spawn egg count","zh-CN":"刷怪蛋数量","zh-TW":"生怪蛋數量"},"paths":[["spawn_egg_count"]]},
  {"code":"defaultLootTable","type":"reference","referenceKind":"minecraft.loot_table","names":{"en-US":"Default loot table","zh-CN":"默认战利品表","zh-TW":"預設戰利品表"},"paths":[["default_loot_table"]]},
  {"code":"spawnEggs","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Spawn eggs","zh-CN":"刷怪蛋","zh-TW":"生怪蛋"},"paths":[["spawn_eggs"]]},
  {"code":"breedingMaterials","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Breeding materials","zh-CN":"繁殖材料","zh-TW":"繁殖材料"},"paths":[["breeding_materials"],["breed_items"]]}
]`

// modContentSchemaStatements installs the human-authored, version-aware mod
// catalog layer. Global resources remain immutable identities; these tables
// hold project versions, version-scoped section trees, and complete independent detail
// documents for each version without duplicating the global identity graph.
func modContentSchemaStatements() []string {
	statements := []string{
		`create table mod_content_versions (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			mod_id bigint not null references mods(id) on delete cascade,
			label text not null default '',
			minecraft_versions text[] not null default '{}'::text[],
			loaders text[] not null default '{}'::text[],
			mod_version text not null default '',
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(status in ('active','pending','superseded','archived'))
		)`,
		`create index idx_mod_content_versions_mod_status on mod_content_versions(mod_id,status,updated_at desc)`,
		`alter table recipe_definitions add column source_mod_content_version_id bigint
			references mod_content_versions(id) on delete set null`,
		`create index idx_recipe_definitions_source_version on recipe_definitions(source_mod_content_version_id)
			where source_mod_content_version_id is not null`,
		`alter table catalog_import_jobs add constraint fk_catalog_import_jobs_target_version
			foreign key(target_version_id) references mod_content_versions(id) on delete cascade`,
		`alter table catalog_import_revisions add constraint fk_catalog_import_revisions_target_version
			foreign key(target_version_id) references mod_content_versions(id) on delete cascade`,
		`create table mod_content_templates (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			owner_mod_id bigint references mods(id) on delete cascade,
			code text not null,
			builtin boolean not null default false,
			i18n_key text not null default '',
			default_locale text not null default 'en-US',
			default_display_mode text not null default 'compact',
			definition jsonb not null default '{}'::jsonb,
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(code ~ '^[a-z][a-z0-9_]{1,63}$'),
			check(default_display_mode in ('compact','large')),
			check(status in ('active','pending','archived')),
			check(jsonb_typeof(definition)='object')
		)`,
		`create unique index idx_mod_content_templates_builtin_code on mod_content_templates(code) where builtin`,
		`create unique index idx_mod_content_templates_custom_code on mod_content_templates(owner_mod_id,code) where not builtin`,
		`create table mod_content_template_localizations (
			template_id bigint not null references mod_content_templates(id) on delete cascade,
			locale text not null,
			name text not null,
			description text not null default '',
			primary key(template_id,locale),
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$')
		)`,
		`insert into mod_content_templates(code,builtin,i18n_key,default_display_mode,definition) values
			('item_block',true,'itemBlock','compact','{"resourceKinds":["minecraft.item","minecraft.block"]}'::jsonb),
			('fluid',true,'fluid','compact','{"resourceKinds":["minecraft.fluid"]}'::jsonb),
			('dimension',true,'dimension','large','{"resourceKinds":["minecraft.dimension"]}'::jsonb),
			('biome',true,'biome','large','{"resourceKinds":["minecraft.biome"]}'::jsonb),
			('entity',true,'entity','large','{"resourceKinds":["minecraft.entity_type"]}'::jsonb),
			('enchantment',true,'enchantment','large','{"resourceKinds":["minecraft.enchantment"]}'::jsonb),
			('mob_effect',true,'mobEffect','large','{"resourceKinds":["minecraft.mob_effect"]}'::jsonb),
			('multiblock',true,'multiblock','large','{"resourceKinds":["minecraft.multiblock"]}'::jsonb),
			('natural_generation',true,'naturalGeneration','large','{"resourceKinds":["minecraft.natural_generation"]}'::jsonb),
			('world_structure',true,'worldStructure','large','{"resourceKinds":["minecraft.structure"]}'::jsonb),
			('key_mapping',true,'keyMapping','large','{"resourceKinds":["minecraft.key_mapping"]}'::jsonb),
			('command',true,'command','large','{"resourceKinds":["minecraft.command"]}'::jsonb),
			('advancement',true,'advancement','large','{"resourceKinds":["minecraft.advancement"]}'::jsonb),
			('loot_table',true,'lootTable','large','{"resourceKinds":["minecraft.loot_table"]}'::jsonb),
			('game_setting',true,'gameSetting','large','{"resourceKinds":["minecraft.game_setting"]}'::jsonb),
			('skill',true,'skill','large','{"resourceKinds":["mod.skill"]}'::jsonb),
			('element',true,'element','large','{"resourceKinds":["mod.element"]}'::jsonb),
			('chemical',true,'chemical','compact','{"resourceKinds":["mekanism.gas","mekanism.infusion","mekanism.pigment","mekanism.slurry"]}'::jsonb)
			on conflict do nothing`,
		`update mod_content_templates set definition=definition||'{
			"entryTypes":[
				{"code":"default","names":{"en-US":"Default","zh-CN":"默认","zh-TW":"預設"},"groups":[]},
				{"code":"block","kindCodes":["minecraft.block"],"names":{"en-US":"Block","zh-CN":"方块","zh-TW":"方塊"},"groups":[
					{"code":"physical","names":{"en-US":"Block properties","zh-CN":"方块属性","zh-TW":"方塊屬性"},"descriptions":{"en-US":"Physical and mining properties for this block.","zh-CN":"方块的物理、采掘与光照属性。","zh-TW":"方塊的物理、採掘與光照屬性。"},"fields":[
						{"code":"hardness","type":"number","names":{"en-US":"Hardness","zh-CN":"硬度","zh-TW":"硬度"},"paths":[["hardness"],["physical","hardness"]]},
						{"code":"explosionResistance","type":"number","names":{"en-US":"Explosion resistance","zh-CN":"爆炸抗性","zh-TW":"爆炸抗性"},"paths":[["explosion_resistance"],["blast_resistance"],["physical","explosionResistance"],["physical","blastResistance"]]},
						{"code":"harvestLevel","type":"number","names":{"en-US":"Mining level","zh-CN":"挖掘等级","zh-TW":"挖掘等級"},"paths":[["required_mining_level"],["harvest_level"],["physical","harvestLevel"],["tool","tier","mining_level"]]},
						{"code":"friction","type":"number","names":{"en-US":"Friction","zh-CN":"摩擦系数","zh-TW":"摩擦係數"},"paths":[["friction"],["physical","friction"]]},
						{"code":"speedFactor","type":"number","names":{"en-US":"Movement speed factor","zh-CN":"移动速度系数","zh-TW":"移動速度係數"},"paths":[["speed_factor"],["physical","speedFactor"]]},
						{"code":"jumpFactor","type":"number","names":{"en-US":"Jump factor","zh-CN":"跳跃系数","zh-TW":"跳躍係數"},"paths":[["jump_factor"],["physical","jumpFactor"]]},
						{"code":"lightLevel","type":"number","names":{"en-US":"Light level","zh-CN":"亮度等级","zh-TW":"亮度等級"},"paths":[["light_emission"],["light_level"],["physical","lightLevel"]]},
						{"code":"requiresCorrectTool","type":"boolean","names":{"en-US":"Requires correct tool","zh-CN":"需要正确工具","zh-TW":"需要正確工具"},"paths":[["requires_correct_tool"],["physical","requiresCorrectTool"]]},
						{"code":"preferredTools","type":"list","names":{"en-US":"Preferred tools","zh-CN":"适用工具","zh-TW":"適用工具"},"paths":[["preferred_tools"],["physical","preferredTools"]]}
					]}
				]},
				{"code":"item","kindCodes":["minecraft.item"],"names":{"en-US":"Regular item","zh-CN":"普通物品","zh-TW":"普通物品"},"groups":[
					{"code":"item","names":{"en-US":"Item properties","zh-CN":"物品属性","zh-TW":"物品屬性"},"fields":[
						{"code":"maxStackSize","type":"number","names":{"en-US":"Maximum stack size","zh-CN":"最大堆叠数量","zh-TW":"最大堆疊數量"},"paths":[["max_stack_size"],["stack_size"],["item","maxStackSize"]]},
						{"code":"enchantability","type":"number","names":{"en-US":"Enchantability","zh-CN":"附魔能力","zh-TW":"附魔能力"},"paths":[["enchanting","enchantment_value"],["enchantment_value"],["item","enchantability"]]},
						{"code":"compatibleEnchantments","type":"reference-list","referenceKind":"enchantment","names":{"en-US":"Compatible enchantments","zh-CN":"兼容附魔","zh-TW":"相容附魔"},"paths":[["enchanting","compatible_enchantments"],["compatible_enchantments"]]},
						{"code":"itemTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:item","names":{"en-US":"Item tags","zh-CN":"包含的标签","zh-TW":"包含的標籤"},"paths":[["item_tags"],["tags"]]}
					]}
				]},
				{"code":"tool","kindCodes":["minecraft.item"],"names":{"en-US":"Tool","zh-CN":"工具","zh-TW":"工具"},"groups":[
					{"code":"tool","names":{"en-US":"Tool properties","zh-CN":"工具属性","zh-TW":"工具屬性"},"descriptions":{"en-US":"Durability, mining, combat, repair, and enchanting values.","zh-CN":"工具的耐久、采掘、战斗、修复与附魔属性。","zh-TW":"工具的耐久、採掘、戰鬥、修復與附魔屬性。"},"fields":[
						{"code":"durability","type":"number","names":{"en-US":"Durability","zh-CN":"耐久","zh-TW":"耐久"},"paths":[["max_damage"],["durability","max_damage"],["tool","durability"]]},
						{"code":"miningSpeed","type":"number","names":{"en-US":"Mining speed","zh-CN":"挖掘速度","zh-TW":"挖掘速度"},"paths":[["tool","tier","mining_speed"],["tool","mining_speed"],["mining_speed"]]},
						{"code":"harvestLevel","type":"number","names":{"en-US":"Mining level","zh-CN":"挖掘等级","zh-TW":"挖掘等級"},"paths":[["tool","tier","mining_level"],["tool","mining_level"],["required_mining_level"]]},
						{"code":"attackDamage","type":"number","names":{"en-US":"Attack damage","zh-CN":"攻击伤害","zh-TW":"攻擊傷害"},"paths":[["combat","attack_damage"],["tool","attack_damage"],["attack_damage"]]},
						{"code":"attackSpeed","type":"number","names":{"en-US":"Attack speed","zh-CN":"攻击速度","zh-TW":"攻擊速度"},"paths":[["combat","attack_speed"],["tool","attack_speed"],["attack_speed"]]},
						{"code":"enchantability","type":"number","names":{"en-US":"Enchantability","zh-CN":"附魔能力","zh-TW":"附魔能力"},"paths":[["enchanting","enchantment_value"],["tool","tier","enchantment_value"],["enchantment_value"]]},
						{"code":"repairTag","type":"reference","referenceKind":"tag","referenceRegistry":"minecraft:item","names":{"en-US":"Repair material tag","zh-CN":"修复材料标签","zh-TW":"修復材料標籤"},"paths":[["tool","repair_tag"],["repair_tag"]]},
						{"code":"compatibleEnchantments","type":"reference-list","referenceKind":"enchantment","names":{"en-US":"Compatible enchantments","zh-CN":"兼容附魔","zh-TW":"相容附魔"},"paths":[["enchanting","compatible_enchantments"],["compatible_enchantments"]]},
						{"code":"itemTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:item","names":{"en-US":"Item tags","zh-CN":"包含的标签","zh-TW":"包含的標籤"},"paths":[["item_tags"],["tags"]]}
					]}
				]},
				{"code":"equipment","kindCodes":["minecraft.item"],"names":{"en-US":"Equipment","zh-CN":"装备","zh-TW":"裝備"},"groups":[
					{"code":"equipment","names":{"en-US":"Equipment properties","zh-CN":"装备属性","zh-TW":"裝備屬性"},"descriptions":{"en-US":"Durability, armor, slot, toughness, and enchanting values.","zh-CN":"装备的耐久、护甲、韧性、槽位与附魔属性。","zh-TW":"裝備的耐久、護甲、韌性、欄位與附魔屬性。"},"fields":[
						{"code":"durability","type":"number","names":{"en-US":"Durability","zh-CN":"耐久","zh-TW":"耐久"},"paths":[["max_damage"],["durability","max_damage"],["equipment","durability"]]},
						{"code":"armorValue","type":"number","format":"armor","names":{"en-US":"Armor value","zh-CN":"提供的护甲值","zh-TW":"提供的護甲值"},"paths":[["armor","defense"],["armor_value"],["equipment","armor"],["equipment","armor_value"]]},
						{"code":"armorToughness","type":"number","names":{"en-US":"Armor toughness","zh-CN":"护甲韧性","zh-TW":"護甲韌性"},"paths":[["armor","toughness"],["armor_toughness"],["toughness"],["equipment","toughness"]]},
						{"code":"knockbackResistance","type":"number","names":{"en-US":"Knockback resistance","zh-CN":"击退抗性","zh-TW":"擊退抗性"},"paths":[["armor","knockback_resistance"],["knockback_resistance"],["equipment","knockback_resistance"]]},
						{"code":"equipmentSlot","type":"text","names":{"en-US":"Equipment slot","zh-CN":"装备槽位","zh-TW":"裝備欄位"},"paths":[["equipment_slot"],["slot"],["equipment","slot"]]},
						{"code":"enchantability","type":"number","names":{"en-US":"Enchantability","zh-CN":"附魔能力","zh-TW":"附魔能力"},"paths":[["armor","enchantment_value"],["enchanting","enchantment_value"],["enchantment_value"],["equipment","enchantability"]]},
						{"code":"repairItems","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Repair materials","zh-CN":"修复材料","zh-TW":"修復材料"},"paths":[["armor","repair_items"],["equipment","repair_items"]]},
						{"code":"compatibleEnchantments","type":"reference-list","referenceKind":"enchantment","names":{"en-US":"Compatible enchantments","zh-CN":"兼容附魔","zh-TW":"相容附魔"},"paths":[["enchanting","compatible_enchantments"],["compatible_enchantments"]]}
					]}
				]}
			]
		}'::jsonb where code='item_block' and builtin`,
		`update mod_content_templates set definition=definition||jsonb_build_object('entryTypes',jsonb_build_array(
			jsonb_build_object('code','default','names','{"en-US":"Default","zh-CN":"默认","zh-TW":"預設"}'::jsonb,'groups','[]'::jsonb),
			jsonb_build_object('code',code,'names',jsonb_build_object('en-US',case code
				when 'mob_effect' then 'Potion effect' when 'entity' then 'Entity' when 'enchantment' then 'Enchantment'
				when 'fluid' then 'Fluid' when 'skill' then 'Skill' when 'chemical' then 'Chemical' else initcap(replace(code,'_',' ')) end,
				'zh-CN',case code when 'mob_effect' then '药水效果' when 'entity' then '生物' when 'enchantment' then '附魔'
				when 'fluid' then '流体' when 'skill' then '技能' when 'chemical' then '化学品' else i18n_key end,
				'zh-TW',case code when 'mob_effect' then '藥水效果' when 'entity' then '生物' when 'enchantment' then '附魔'
				when 'fluid' then '流體' when 'skill' then '技能' when 'chemical' then '化學品' else i18n_key end),
				'groups',case when code='skill' then '[{"code":"skill","names":{"en-US":"Skill properties","zh-CN":"技能属性","zh-TW":"技能屬性"},"fields":[{"code":"cd","type":"number","names":{"en-US":"Cooldown","zh-CN":"冷却时间（CD）","zh-TW":"冷卻時間（CD）"},"paths":[["cd"],["skill","cd"]]}]}]'::jsonb
					when code='entity' then '[{"code":"entity","names":{"en-US":"Entity properties","zh-CN":"生物属性","zh-TW":"生物屬性"},"fields":[{"code":"maxHealth","type":"number","format":"health","names":{"en-US":"Maximum health","zh-CN":"最大生命值","zh-TW":"最大生命值"},"paths":[["max_health"],["attributes","max_health"]]},{"code":"armorValue","type":"number","format":"armor","names":{"en-US":"Armor value","zh-CN":"护甲值","zh-TW":"護甲值"},"paths":[["armor_value"],["attributes","armor"]]},{"code":"width","type":"number","names":{"en-US":"Width","zh-CN":"宽度","zh-TW":"寬度"},"paths":[["width"],["dimensions","width"]]},{"code":"height","type":"number","names":{"en-US":"Height","zh-CN":"高度","zh-TW":"高度"},"paths":[["height"],["dimensions","height"]]},{"code":"eyeHeight","type":"number","names":{"en-US":"Eye height","zh-CN":"眼睛高度","zh-TW":"眼睛高度"},"paths":[["eye_height"],["dimensions","eye_height"]]},{"code":"fireImmune","type":"boolean","names":{"en-US":"Fire immune","zh-CN":"免疫火焰","zh-TW":"免疫火焰"},"paths":[["fire_immune"],["properties","fire_immune"]]}]}]'::jsonb
					when code='mob_effect' then '[{"code":"effect","names":{"en-US":"Potion-effect properties","zh-CN":"药水效果属性","zh-TW":"藥水效果屬性"},"fields":[{"code":"category","type":"text","names":{"en-US":"Category","zh-CN":"效果类别","zh-TW":"效果類別"},"paths":[["category"],["effect","category"]]},{"code":"color","type":"text","names":{"en-US":"Color","zh-CN":"效果颜色","zh-TW":"效果顏色"},"paths":[["color"],["effect","color"]]},{"code":"beneficial","type":"boolean","names":{"en-US":"Beneficial","zh-CN":"正面效果","zh-TW":"正面效果"},"paths":[["beneficial"],["effect","beneficial"]]},{"code":"instant","type":"boolean","names":{"en-US":"Instant effect","zh-CN":"即时生效","zh-TW":"即時生效"},"paths":[["instant"],["effect","instant"]]}]}]'::jsonb
					when code='enchantment' then '[{"code":"enchantment","names":{"en-US":"Enchantment properties","zh-CN":"附魔属性","zh-TW":"附魔屬性"},"fields":[{"code":"minimumLevel","type":"number","names":{"en-US":"Minimum level","zh-CN":"最低等级","zh-TW":"最低等級"},"paths":[["min_level"],["minimum_level"],["enchantment","minimumLevel"]]},{"code":"maximumLevel","type":"number","names":{"en-US":"Maximum level","zh-CN":"最高等级","zh-TW":"最高等級"},"paths":[["max_level"],["maximum_level"],["enchantment","maximumLevel"]]},{"code":"rarity","type":"text","names":{"en-US":"Rarity","zh-CN":"稀有度","zh-TW":"稀有度"},"paths":[["rarity"],["enchantment","rarity"]]},{"code":"treasureOnly","type":"boolean","names":{"en-US":"Treasure only","zh-CN":"宝藏附魔","zh-TW":"寶藏附魔"},"paths":[["treasure_only"],["treasure"],["enchantment","treasureOnly"]]},{"code":"curse","type":"boolean","names":{"en-US":"Curse","zh-CN":"诅咒附魔","zh-TW":"詛咒附魔"},"paths":[["curse"],["enchantment","curse"]]},{"code":"tradeable","type":"boolean","names":{"en-US":"Tradeable","zh-CN":"可交易获得","zh-TW":"可交易取得"},"paths":[["tradeable"],["enchantment","tradeable"]]},{"code":"discoverable","type":"boolean","names":{"en-US":"Discoverable","zh-CN":"可探索获得","zh-TW":"可探索取得"},"paths":[["discoverable"],["enchantment","discoverable"]]}]}]'::jsonb
					when code='fluid' or code='chemical' then '[{"code":"material","names":{"en-US":"Material properties","zh-CN":"材料属性","zh-TW":"材料屬性"},"fields":[{"code":"density","type":"number","names":{"en-US":"Density","zh-CN":"密度","zh-TW":"密度"},"paths":[["density"],["fluid","density"],["chemical","density"]]},{"code":"temperature","type":"number","names":{"en-US":"Temperature","zh-CN":"温度","zh-TW":"溫度"},"paths":[["temperature"],["fluid","temperature"],["chemical","temperature"]]},{"code":"viscosity","type":"number","names":{"en-US":"Viscosity","zh-CN":"黏度","zh-TW":"黏度"},"paths":[["viscosity"],["fluid","viscosity"],["chemical","viscosity"]]},{"code":"luminosity","type":"number","names":{"en-US":"Luminosity","zh-CN":"发光等级","zh-TW":"發光等級"},"paths":[["luminosity"],["fluid","luminosity"],["chemical","luminosity"]]},{"code":"gaseous","type":"boolean","names":{"en-US":"Gaseous","zh-CN":"气态","zh-TW":"氣態"},"paths":[["gaseous"],["chemical","gaseous"]]}]}]'::jsonb
					else '[]'::jsonb end)
		)) where builtin and code<>'item_block'`,
		`update mod_content_templates set definition=jsonb_set(
			definition,
			'{entryTypes,1,groups,0,fields}',
			coalesce(definition#>'{entryTypes,1,groups,0,fields}','[]'::jsonb)||'` + builtinBlockCompatibilityFields + `'::jsonb
		) where code='item_block' and builtin`,
		`update mod_content_templates set definition=jsonb_set(
			definition,
			'{entryTypes,2,groups,0,fields}',
			coalesce(definition#>'{entryTypes,2,groups,0,fields}','[]'::jsonb)||'` + builtinItemCompatibilityFields + `'::jsonb
		) where code='item_block' and builtin`,
		`update mod_content_templates set definition=jsonb_set(
			definition,
			'{entryTypes,3,groups,0,fields}',
			coalesce(definition#>'{entryTypes,3,groups,0,fields}','[]'::jsonb)||'` + builtinToolCompatibilityFields + `'::jsonb
		) where code='item_block' and builtin`,
		`update mod_content_templates set definition=jsonb_set(
			definition,
			'{entryTypes,4,groups,0,fields}',
			coalesce(definition#>'{entryTypes,4,groups,0,fields}','[]'::jsonb)||'` + builtinEquipmentCompatibilityFields + `'::jsonb
		) where code='item_block' and builtin`,
		`update mod_content_templates set definition=jsonb_set(
			definition,
			'{entryTypes,1,groups,0,fields}',
			coalesce(definition#>'{entryTypes,1,groups,0,fields}','[]'::jsonb)||'` + builtinEntityCompatibilityFields + `'::jsonb
		) where code='entity' and builtin`,
		`create table mod_content_sections (
			id bigserial primary key,
			public_id text not null unique default new_public_id() check(public_id ~ '^[a-z0-9]{9}$'),
			mod_id bigint not null references mods(id) on delete cascade,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			template_id bigint not null references mod_content_templates(id) on delete restrict,
			parent_id bigint references mod_content_sections(id) on delete cascade,
			system_key text not null default '',
			default_locale text not null default 'en-US',
			display_mode text not null,
			definition_override jsonb,
			definition_version integer not null default 1 check(definition_version>0),
			ordinal integer not null default 0 check(ordinal>=0),
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			check(display_mode in ('compact','large')),
			check(definition_override is null or jsonb_typeof(definition_override)='object'),
			check(status in ('active','pending','archived')),
			unique(id,version_id),
			unique(version_id,parent_id,ordinal)
		)`,
		`create index idx_mod_content_sections_tree on mod_content_sections(version_id,parent_id,ordinal)`,
		`create unique index idx_mod_content_sections_system_key on mod_content_sections(version_id,parent_id,system_key)
			where system_key<>'' and status='active'`,
		`create table mod_content_section_localizations (
			section_id bigint not null references mod_content_sections(id) on delete cascade,
			locale text not null,
			name text not null default '',
			description text not null default '',
			primary key(section_id,locale),
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$')
		)`,
		`create table mod_content_section_resources (
			section_id bigint not null,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			resource_id bigint not null references game_resources(entity_id) on delete restrict,
			placement_identity_key text not null,
			similar_group_id text not null default '',
			ordinal integer not null default 0 check(ordinal>=0),
			placement_source text not null default 'manual',
			created_at timestamptz not null default now(),
			primary key(section_id,version_id,resource_id),
			foreign key(section_id,version_id) references mod_content_sections(id,version_id) on delete cascade,
			unique(section_id,version_id,ordinal),
			unique(version_id,resource_id),
			unique(version_id,placement_identity_key),
			check(similar_group_id='' or similar_group_id ~ '^[a-z0-9]{9}$'),
			check(placement_source in ('manual','import'))
		)`,
		`create index idx_mod_content_section_resources_resource on mod_content_section_resources(resource_id,version_id)`,
		`create index idx_mod_content_section_resources_similar_group
			on mod_content_section_resources(version_id,similar_group_id,ordinal) where similar_group_id<>''`,
		`create or replace function assign_mod_content_placement_identity() returns trigger as $$
		declare resource_kind text; resource_canonical_id text; block_representative_id bigint;
		begin
			select kind_code,canonical_id into strict resource_kind,resource_canonical_id
			from game_resources where entity_id=new.resource_id;
			if resource_kind in ('minecraft.item','minecraft.block') then
				select binding.block_resource_id into block_representative_id
				from game_resource_asset_bindings binding
				join resource_import_snapshots snapshot on snapshot.id=binding.snapshot_id
				join catalog_import_revisions revision on revision.id=snapshot.revision_id
				where revision.target_version_id=new.version_id and revision.is_active
				  and revision.status in ('ready','partial')
				  and binding.block_resource_id is not null
				  and new.resource_id in (binding.item_resource_id,binding.block_resource_id)
				order by coalesce(revision.activated_at,revision.created_at) desc,binding.block_resource_id
				limit 1;
				if block_representative_id is not null then
					new.placement_identity_key := 'item-block:resource:'||block_representative_id::text;
				else
					new.placement_identity_key := 'item-block:canonical:'||lower(resource_canonical_id);
				end if;
			else
				new.placement_identity_key := 'resource:'||new.resource_id::text;
			end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_placement_identity before insert or update of resource_id,version_id
			on mod_content_section_resources for each row execute function assign_mod_content_placement_identity()`,
		`create table mod_resource_bindings (
			resource_id bigint primary key references game_resources(entity_id) on delete cascade,
			mod_id bigint not null references mods(id) on delete cascade,
			created_at timestamptz not null default now()
		)`,
		`create index idx_mod_resource_bindings_mod on mod_resource_bindings(mod_id,resource_id)`,
		`create table mod_resource_version_details (
			resource_id bigint not null references mod_resource_bindings(resource_id) on delete cascade,
			version_id bigint not null references mod_content_versions(id) on delete cascade,
			entry_type_code text not null default 'default',
			definition_schema_version smallint not null default 1,
			default_locale text not null default 'en-US',
			definition jsonb not null default '{}'::jsonb,
			icon_small_file_id bigint references oss_files(id) on delete set null,
			icon_file_id bigint references oss_files(id) on delete set null,
			render_file_id bigint references oss_files(id) on delete set null,
			status text not null default 'active',
			published_revision_id bigint references content_revisions(id) on delete restrict,
			created_by bigint references users(id) on delete set null,
			updated_by bigint references users(id) on delete set null,
			created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),
			primary key(resource_id,version_id),
			check(status in ('active','pending','archived')),
			check(entry_type_code ~ '^[a-z][a-z0-9_]{1,63}$'),
			check(jsonb_typeof(definition)='object'),
			check(definition_schema_version>=1)
		)`,
		`create index idx_mod_resource_version_details_status on mod_resource_version_details(version_id,status,updated_at desc)`,
		`create table mod_resource_version_detail_localizations (
			resource_id bigint not null,
			version_id bigint not null,
			locale text not null,
			name text not null default '',
			summary text not null default '',
			content_markdown text not null default '',
			provenance text not null default 'human',
			primary key(resource_id,version_id,locale),
			foreign key(resource_id,version_id) references mod_resource_version_details(resource_id,version_id) on delete cascade,
			check(locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'),
			check(provenance in ('human','import','ai','human_corrected'))
		)`,
		`create or replace function validate_mod_content_section_tree() returns trigger as $$
		declare parent_mod bigint; parent_version bigint; parent_depth integer;
		begin
			if new.parent_id is null then return new; end if;
			with recursive parents as (
				select section.id,section.parent_id,section.mod_id,section.version_id,1 depth from mod_content_sections section where section.id=new.parent_id
				union all select section.id,section.parent_id,section.mod_id,section.version_id,parents.depth+1
				from mod_content_sections section join parents on section.id=parents.parent_id where parents.depth<6
			) select max(mod_id),max(version_id),max(depth) into parent_mod,parent_version,parent_depth from parents;
			if parent_mod is null or parent_mod<>new.mod_id then raise exception 'section parent must belong to the same mod'; end if;
			if parent_version<>new.version_id then raise exception 'section parent must belong to the same data version'; end if;
			if parent_depth>=5 then raise exception 'content category depth cannot exceed four levels'; end if;
			if new.parent_id=new.id then raise exception 'content section cannot be its own parent'; end if;
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_section_tree before insert or update of parent_id,mod_id,version_id on mod_content_sections
			for each row execute function validate_mod_content_section_tree()`,
		`create or replace function register_mod_content_public_route() returns trigger as $$
		begin
			insert into public_routes(public_id,entity_type,internal_id)
			values(new.public_id,TG_ARGV[0],new.id);
			return new;
		end;
		$$ language plpgsql`,
		`create trigger trg_mod_content_versions_public_route after insert on mod_content_versions
			for each row execute function register_mod_content_public_route('mod_content_version')`,
		`create trigger trg_mod_content_templates_public_route after insert on mod_content_templates
			for each row execute function register_mod_content_public_route('mod_content_template')`,
		`create trigger trg_mod_content_sections_public_route after insert on mod_content_sections
			for each row execute function register_mod_content_public_route('mod_content_section')`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_version',id from mod_content_versions on conflict do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_template',id from mod_content_templates on conflict do nothing`,
		`insert into public_routes(public_id,entity_type,internal_id)
			select public_id,'mod_content_section',id from mod_content_sections on conflict do nothing`,
	}
	return append(statements, modContentCanonicalDocumentSchemaStatements()...)
}
