package database

// modContentCanonicalDocumentSchemaStatements adds fields that are useful to
// the website but are not all intended to be edited as primitive form inputs.
// A field code is the canonical JSONB key. paths are importer aliases only;
// changing an exporter field name therefore does not change stored documents.
func modContentCanonicalDocumentSchemaStatements() []string {
	return []string{
		appendEntryTypeFieldsSQL("item_block", 1, `[
			{"code":"defaultState","type":"json","editable":false,"names":{"en-US":"Default block state","zh-CN":"默认方块状态","zh-TW":"預設方塊狀態"},"paths":[["default_state"]]}
		]`),
		appendEntryTypeFieldsSQL("item_block", 2, `[
			{"code":"attributeModifiers","type":"json","editable":false,"names":{"en-US":"Attribute modifiers","zh-CN":"属性修饰符","zh-TW":"屬性修飾符"},"paths":[["attribute_modifiers"]]}
		]`),
		appendEntryTypeFieldsSQL("item_block", 3, `[
			{"code":"miningLevel","type":"number","names":{"en-US":"Mining level","zh-CN":"挖掘等级","zh-TW":"挖掘等級"},"paths":[["tool","tier","mining_level"],["required_mining_level"]]},
			{"code":"enchantmentValue","type":"number","names":{"en-US":"Tier enchantment value","zh-CN":"材料附魔能力","zh-TW":"材料附魔能力"},"paths":[["tool","tier","enchantment_value"]]},
			{"code":"combatModifiers","type":"json","editable":false,"names":{"en-US":"Combat modifiers","zh-CN":"战斗属性修饰符","zh-TW":"戰鬥屬性修飾符"},"paths":[["combat","modifiers"],["attribute_modifiers"]]}
		]`),
		appendEntryTypeFieldsSQL("item_block", 4, `[
			{"code":"attributeModifiers","type":"json","editable":false,"names":{"en-US":"Attribute modifiers","zh-CN":"属性修饰符","zh-TW":"屬性修飾符"},"paths":[["attribute_modifiers"]]}
		]`),
		appendEntryTypeFieldsSQL("entity", 1, `[
			{"code":"defaultEquipment","type":"json","editable":false,"names":{"en-US":"Default equipment","zh-CN":"默认装备","zh-TW":"預設裝備"},"paths":[["default_equipment"]]}
		]`),
		setEntryTypeStorageFieldsSQL("advancement", 1, `[
			{"code":"parentId","type":"reference","referenceKind":"minecraft.advancement","editable":false,"names":{"en-US":"Parent advancement","zh-CN":"父成就","zh-TW":"父成就"},"paths":[["parent"]]},
			{"code":"childrenIds","type":"reference-list","referenceKind":"minecraft.advancement","editable":false,"names":{"en-US":"Child advancements","zh-CN":"子成就","zh-TW":"子成就"},"paths":[["children"]]},
			{"code":"iconItemId","type":"reference","referenceKind":"minecraft.item","editable":false,"names":{"en-US":"Icon item","zh-CN":"图标物品","zh-TW":"圖示物品"},"paths":[["display","icon","item"]]},
			{"code":"display","type":"json","editable":false,"names":{"en-US":"Display","zh-CN":"显示信息","zh-TW":"顯示資訊"},"paths":[["display"]]},
			{"code":"criteria","type":"list","editable":false,"names":{"en-US":"Criteria","zh-CN":"完成条件","zh-TW":"完成條件"},"paths":[["criteria"]]},
			{"code":"requirements","type":"json","editable":false,"names":{"en-US":"Requirement groups","zh-CN":"条件组合","zh-TW":"條件組合"},"paths":[["requirements"]]},
			{"code":"maximumCriteriaRequired","type":"number","editable":false,"names":{"en-US":"Maximum criteria required","zh-CN":"最多所需条件数","zh-TW":"最多所需條件數"},"paths":[["max_criteria_required"]]},
			{"code":"rewards","type":"json","editable":false,"names":{"en-US":"Rewards","zh-CN":"奖励","zh-TW":"獎勵"},"paths":[["rewards"]]},
			{"code":"sendsTelemetryEvent","type":"boolean","editable":false,"names":{"en-US":"Sends telemetry event","zh-CN":"发送遥测事件","zh-TW":"傳送遙測事件"},"paths":[["sends_telemetry_event"]]}
		]`),
		setEntryTypeStorageFieldsSQL("enchantment", 1, `[
			{"code":"minimumLevel","type":"number","names":{"en-US":"Minimum level","zh-CN":"最低等级","zh-TW":"最低等級"},"paths":[["min_level"],["minimum_level"]]},
			{"code":"maximumLevel","type":"number","names":{"en-US":"Maximum level","zh-CN":"最高等级","zh-TW":"最高等級"},"paths":[["max_level"],["maximum_level"]]},
			{"code":"rarity","type":"text","names":{"en-US":"Rarity","zh-CN":"稀有度","zh-TW":"稀有度"},"paths":[["rarity"]]},
			{"code":"rarityWeight","type":"number","names":{"en-US":"Rarity weight","zh-CN":"稀有度权重","zh-TW":"稀有度權重"},"paths":[["rarity_weight"]]},
			{"code":"anvilCost","type":"number","names":{"en-US":"Anvil cost","zh-CN":"铁砧消耗","zh-TW":"鐵砧消耗"},"paths":[["anvil_cost"]]},
			{"code":"treasureOnly","type":"boolean","names":{"en-US":"Treasure only","zh-CN":"宝藏附魔","zh-TW":"寶藏附魔"},"paths":[["treasure_only"],["treasure"]]},
			{"code":"curse","type":"boolean","names":{"en-US":"Curse","zh-CN":"诅咒附魔","zh-TW":"詛咒附魔"},"paths":[["curse"]]},
			{"code":"tradeable","type":"boolean","names":{"en-US":"Tradeable","zh-CN":"可交易获得","zh-TW":"可交易取得"},"paths":[["tradeable"]]},
			{"code":"discoverable","type":"boolean","names":{"en-US":"Discoverable","zh-CN":"可探索获得","zh-TW":"可探索取得"},"paths":[["discoverable"]]},
			{"code":"slots","type":"list","names":{"en-US":"Equipment slots","zh-CN":"适用装备槽位","zh-TW":"適用裝備欄位"},"paths":[["slots"]]},
			{"code":"supportedItemsTag","type":"reference","referenceKind":"tag","referenceRegistry":"minecraft:item","names":{"en-US":"Supported-items tag","zh-CN":"支持物品标签","zh-TW":"支援物品標籤"},"paths":[["supported_items_tag"]]},
			{"code":"supportedItems","type":"reference-list","referenceKind":"minecraft.item","names":{"en-US":"Supported items","zh-CN":"支持的物品","zh-TW":"支援的物品"},"paths":[["supported_items"]]},
			{"code":"exclusiveWith","type":"reference-list","referenceKind":"minecraft.enchantment","names":{"en-US":"Mutually exclusive enchantments","zh-CN":"互斥附魔","zh-TW":"互斥附魔"},"paths":[["exclusive_with"]]},
			{"code":"costs","type":"json","editable":false,"names":{"en-US":"Level costs","zh-CN":"等级消耗范围","zh-TW":"等級消耗範圍"},"paths":[["costs"]]},
			{"code":"effectComponentCount","type":"number","editable":false,"names":{"en-US":"Effect component count","zh-CN":"效果组件数量","zh-TW":"效果元件數量"},"paths":[["effect_component_count"]]}
		]`),
		setEntryTypeStorageFieldsSQL("mob_effect", 1, `[
			{"code":"category","type":"text","names":{"en-US":"Category","zh-CN":"效果类别","zh-TW":"效果類別"},"paths":[["category"]]},
			{"code":"colorRGB","type":"number","names":{"en-US":"Color (RGB)","zh-CN":"效果颜色（RGB）","zh-TW":"效果顏色（RGB）"},"paths":[["color_rgb"],["color"]]},
			{"code":"beneficial","type":"boolean","names":{"en-US":"Beneficial","zh-CN":"正面效果","zh-TW":"正面效果"},"paths":[["beneficial"]]},
			{"code":"instant","type":"boolean","names":{"en-US":"Instant effect","zh-CN":"即时生效","zh-TW":"即時生效"},"paths":[["instant"]]},
			{"code":"effectAttributeModifiers","type":"json","editable":false,"names":{"en-US":"Attribute modifiers","zh-CN":"属性修饰器","zh-TW":"屬性修飾器"},"paths":[["effect_attribute_modifiers"]]}
		]`),
		setEntryTypeStorageFieldsSQL("fluid", 1, `[
			{"code":"ingredientKind","type":"text","names":{"en-US":"Ingredient kind","zh-CN":"原料类型","zh-TW":"原料類型"},"paths":[["ingredient_kind"]]},
			{"code":"source","type":"boolean","names":{"en-US":"Source fluid","zh-CN":"源流体","zh-TW":"源流體"},"paths":[["source"]]},
			{"code":"amount","type":"number","names":{"en-US":"Standard amount","zh-CN":"标准数量","zh-TW":"標準數量"},"paths":[["amount"]]},
			{"code":"bucketItemId","type":"reference","referenceKind":"minecraft.item","names":{"en-US":"Bucket item","zh-CN":"桶物品","zh-TW":"桶物品"},"paths":[["bucket"]]},
			{"code":"fluidTags","type":"reference-list","referenceKind":"tag","referenceRegistry":"minecraft:fluid","names":{"en-US":"Fluid tags","zh-CN":"流体标签","zh-TW":"流體標籤"},"paths":[["tags"]]},
			{"code":"density","type":"number","names":{"en-US":"Density","zh-CN":"密度","zh-TW":"密度"},"paths":[["density"],["fluid","density"]]},
			{"code":"temperature","type":"number","names":{"en-US":"Temperature","zh-CN":"温度","zh-TW":"溫度"},"paths":[["temperature"],["fluid","temperature"]]},
			{"code":"viscosity","type":"number","names":{"en-US":"Viscosity","zh-CN":"黏度","zh-TW":"黏度"},"paths":[["viscosity"],["fluid","viscosity"]]},
			{"code":"luminosity","type":"number","names":{"en-US":"Luminosity","zh-CN":"发光等级","zh-TW":"發光等級"},"paths":[["luminosity"],["fluid","luminosity"]]},
			{"code":"gaseous","type":"boolean","names":{"en-US":"Gaseous","zh-CN":"气态","zh-TW":"氣態"},"paths":[["gaseous"]]}
		]`),
		setEntryTypeStorageFieldsSQL("key_mapping", 1, `[
			{"code":"sourceModId","type":"text","editable":false,"names":{"en-US":"Source mod ID","zh-CN":"来源模组 ID","zh-TW":"來源模組 ID"},"paths":[["source_mod_id"]]},
			{"code":"sourceDetection","type":"text","editable":false,"names":{"en-US":"Detection source","zh-CN":"检测来源","zh-TW":"偵測來源"},"paths":[["source_detection"]]},
			{"code":"categoryTranslationKey","type":"text","names":{"en-US":"Category translation key","zh-CN":"分类翻译键","zh-TW":"分類翻譯鍵"},"paths":[["category_translation_key"]]},
			{"code":"categoryNames","type":"json","editable":false,"names":{"en-US":"Category names","zh-CN":"分类本地化名称","zh-TW":"分類本地化名稱"},"paths":[["category_names"]]},
			{"code":"defaultKey","type":"text","names":{"en-US":"Default key","zh-CN":"默认按键","zh-TW":"預設按鍵"},"paths":[["default_key"]]},
			{"code":"boundKey","type":"text","names":{"en-US":"Current bound key","zh-CN":"当前绑定按键","zh-TW":"目前綁定按鍵"},"paths":[["bound_key"]]},
			{"code":"defaultBinding","type":"boolean","names":{"en-US":"Uses default binding","zh-CN":"使用默认绑定","zh-TW":"使用預設綁定"},"paths":[["is_default"]]},
			{"code":"unbound","type":"boolean","names":{"en-US":"Unbound","zh-CN":"未绑定","zh-TW":"未綁定"},"paths":[["is_unbound"]]}
		]`),
		setEntryTypeStorageFieldsSQL("dimension", 1, `[
			{"code":"dimensionType","type":"text","editable":false,"names":{"en-US":"Dimension type","zh-CN":"维度类型","zh-TW":"維度類型"},"paths":[["runtime_definition","type"],["definition","type"]]},
			{"code":"generatorType","type":"text","editable":false,"names":{"en-US":"Generator type","zh-CN":"生成器类型","zh-TW":"生成器類型"},"paths":[["runtime_definition","generator","type"],["definition","generator","type"]]},
			{"code":"generatorSettings","type":"json","editable":false,"names":{"en-US":"Generator settings","zh-CN":"生成器设置","zh-TW":"生成器設定"},"paths":[["runtime_definition","generator","settings"],["definition","generator","settings"]]},
			{"code":"biomeSource","type":"json","editable":false,"names":{"en-US":"Biome source","zh-CN":"生物群系来源","zh-TW":"生態域來源"},"paths":[["runtime_definition","generator","biome_source"],["definition","generator","biome_source"]]},
			{"code":"biomeIds","type":"reference-list","referenceKind":"minecraft.biome","names":{"en-US":"Included biomes","zh-CN":"包含的生物群系","zh-TW":"包含的生態域"},"paths":[["biome_ids"]]},
			{"code":"ambientLight","type":"number","editable":false,"names":{"en-US":"Ambient light","zh-CN":"环境光照","zh-TW":"環境光照"},"paths":[["dimension_type_definition","ambient_light"]]},
			{"code":"coordinateScale","type":"number","editable":false,"names":{"en-US":"Coordinate scale","zh-CN":"坐标缩放","zh-TW":"座標縮放"},"paths":[["dimension_type_definition","coordinate_scale"]]},
			{"code":"minimumY","type":"number","editable":false,"names":{"en-US":"Minimum Y","zh-CN":"最低高度","zh-TW":"最低高度"},"paths":[["dimension_type_definition","min_y"]]},
			{"code":"height","type":"number","editable":false,"names":{"en-US":"Height","zh-CN":"高度","zh-TW":"高度"},"paths":[["dimension_type_definition","height"]]},
			{"code":"logicalHeight","type":"number","editable":false,"names":{"en-US":"Logical height","zh-CN":"逻辑高度","zh-TW":"邏輯高度"},"paths":[["dimension_type_definition","logical_height"]]},
			{"code":"natural","type":"boolean","editable":false,"names":{"en-US":"Natural","zh-CN":"自然维度","zh-TW":"自然維度"},"paths":[["dimension_type_definition","natural"]]},
			{"code":"ultrawarm","type":"boolean","editable":false,"names":{"en-US":"Ultrawarm","zh-CN":"超高温","zh-TW":"超高溫"},"paths":[["dimension_type_definition","ultrawarm"]]},
			{"code":"hasSkylight","type":"boolean","editable":false,"names":{"en-US":"Has skylight","zh-CN":"拥有天空光照","zh-TW":"擁有天空光照"},"paths":[["dimension_type_definition","has_skylight"]]},
			{"code":"hasCeiling","type":"boolean","editable":false,"names":{"en-US":"Has ceiling","zh-CN":"拥有顶部","zh-TW":"擁有頂部"},"paths":[["dimension_type_definition","has_ceiling"]]},
			{"code":"bedWorks","type":"boolean","editable":false,"names":{"en-US":"Beds work","zh-CN":"允许使用床","zh-TW":"允許使用床"},"paths":[["dimension_type_definition","bed_works"]]},
			{"code":"respawnAnchorWorks","type":"boolean","editable":false,"names":{"en-US":"Respawn anchors work","zh-CN":"允许使用重生锚","zh-TW":"允許使用重生錨"},"paths":[["dimension_type_definition","respawn_anchor_works"]]},
			{"code":"hasRaids","type":"boolean","editable":false,"names":{"en-US":"Has raids","zh-CN":"允许袭击","zh-TW":"允許突襲"},"paths":[["dimension_type_definition","has_raids"]]},
			{"code":"infiniburnTag","type":"reference","referenceKind":"tag","referenceRegistry":"minecraft:block","names":{"en-US":"Infiniburn tag","zh-CN":"无限燃烧方块标签","zh-TW":"無限燃燒方塊標籤"},"paths":[["dimension_type_definition","infiniburn"]]}
		]`),
		setEntryTypeStorageFieldsSQL("biome", 1, `[
			{"code":"hasPrecipitation","type":"boolean","names":{"en-US":"Has precipitation","zh-CN":"存在降水","zh-TW":"存在降水"},"paths":[["runtime_definition","has_precipitation"],["definition","has_precipitation"]]},
			{"code":"temperature","type":"number","names":{"en-US":"Temperature","zh-CN":"温度","zh-TW":"溫度"},"paths":[["runtime_definition","temperature"],["definition","temperature"]]},
			{"code":"downfall","type":"number","names":{"en-US":"Downfall","zh-CN":"降水量","zh-TW":"降水量"},"paths":[["runtime_definition","downfall"],["definition","downfall"]]},
			{"code":"creatureSpawnProbability","type":"number","names":{"en-US":"Creature spawn probability","zh-CN":"生物生成概率","zh-TW":"生物生成機率"},"paths":[["runtime_definition","creature_spawn_probability"],["definition","creature_spawn_probability"]]},
			{"code":"effects","type":"json","editable":false,"names":{"en-US":"Visual and sound effects","zh-CN":"视觉与声音效果","zh-TW":"視覺與聲音效果"},"paths":[["runtime_definition","effects"],["definition","effects"]]},
			{"code":"spawnData","type":"json","editable":false,"names":{"en-US":"Spawn data","zh-CN":"生物生成数据","zh-TW":"生物生成資料"},"paths":[["runtime_definition","spawners"],["definition","spawners"]]},
			{"code":"spawnedEntityIds","type":"reference-list","referenceKind":"minecraft.entity_type","names":{"en-US":"Spawnable entities","zh-CN":"可生成的生物","zh-TW":"可生成的生物"},"paths":[["spawned_entity_ids"]]},
			{"code":"featureIds","type":"reference-list","referenceKind":"minecraft.natural_generation","names":{"en-US":"Natural generation entries","zh-CN":"自然生成条目","zh-TW":"自然生成條目"},"paths":[["feature_ids"]]},
			{"code":"carverIds","type":"reference-list","referenceKind":"minecraft.natural_generation","names":{"en-US":"Carvers","zh-CN":"地形雕刻器","zh-TW":"地形雕刻器"},"paths":[["carver_ids"]]},
			{"code":"dimensionIds","type":"reference-list","referenceKind":"minecraft.dimension","names":{"en-US":"Dimensions","zh-CN":"所属维度","zh-TW":"所屬維度"},"paths":[["dimension_ids"]]}
		]`),
		setEntryTypeStorageFieldsSQL("loot_table", 1, `[
			{"code":"category","type":"text","editable":false,"names":{"en-US":"Category","zh-CN":"战利品表分类","zh-TW":"戰利品表分類"},"paths":[["category"]]},
			{"code":"path","type":"text","editable":false,"names":{"en-US":"Path","zh-CN":"数据路径","zh-TW":"資料路徑"},"paths":[["path"]]},
			{"code":"definitionAvailable","type":"boolean","editable":false,"names":{"en-US":"Definition available","zh-CN":"定义可用","zh-TW":"定義可用"},"paths":[["definition_available"]]},
			{"code":"pools","type":"json","editable":false,"names":{"en-US":"Loot pools","zh-CN":"战利品池","zh-TW":"戰利品池"},"paths":[["pools"],["definition","pools"]]},
			{"code":"possibleItemIds","type":"reference-list","referenceKind":"minecraft.item","editable":false,"names":{"en-US":"Possible items","zh-CN":"可能包含的物品","zh-TW":"可能包含的物品"},"paths":[["possible_item_ids"]]},
			{"code":"referencedLootTables","type":"reference-list","referenceKind":"minecraft.loot_table","editable":false,"names":{"en-US":"Referenced loot tables","zh-CN":"引用的战利品表","zh-TW":"引用的戰利品表"},"paths":[["referenced_loot_tables"]]}
		]`),
		setEntryTypeStorageFieldsSQL("natural_generation", 1, `[
			{"code":"entryKind","type":"text","editable":false,"names":{"en-US":"Entry kind","zh-CN":"条目类型","zh-TW":"條目類型"},"paths":[["entry_kind"]]},
			{"code":"category","type":"text","editable":false,"names":{"en-US":"Category","zh-CN":"分类","zh-TW":"分類"},"paths":[["category"]]},
			{"code":"featureType","type":"text","editable":false,"names":{"en-US":"Feature type","zh-CN":"地物类型","zh-TW":"地物類型"},"paths":[["feature_type"]]},
			{"code":"carverType","type":"text","editable":false,"names":{"en-US":"Carver type","zh-CN":"雕刻器类型","zh-TW":"雕刻器類型"},"paths":[["carver_type"]]},
			{"code":"outputs","type":"json","editable":false,"names":{"en-US":"Outputs","zh-CN":"生成内容","zh-TW":"生成內容"},"paths":[["outputs"]]},
			{"code":"targets","type":"json","editable":false,"names":{"en-US":"Targets","zh-CN":"目标方块","zh-TW":"目標方塊"},"paths":[["targets"]]},
			{"code":"size","type":"json","editable":false,"names":{"en-US":"Size","zh-CN":"规模","zh-TW":"規模"},"paths":[["size"]]},
			{"code":"placement","type":"json","editable":false,"names":{"en-US":"Placement","zh-CN":"放置规则","zh-TW":"放置規則"},"paths":[["placement"]]},
			{"code":"probability","type":"number","editable":false,"names":{"en-US":"Probability","zh-CN":"生成概率","zh-TW":"生成機率"},"paths":[["probability"]]},
			{"code":"generationSteps","type":"list","editable":false,"names":{"en-US":"Generation steps","zh-CN":"生成阶段","zh-TW":"生成階段"},"paths":[["generation_steps"]]},
			{"code":"biomeSelectors","type":"json","editable":false,"names":{"en-US":"Biome selectors","zh-CN":"群系选择器","zh-TW":"生態域選擇器"},"paths":[["biome_selectors"]]},
			{"code":"resolvedBiomeIds","type":"reference-list","referenceKind":"minecraft.biome","editable":false,"names":{"en-US":"Resolved biomes","zh-CN":"匹配的群系","zh-TW":"匹配的生態域"},"paths":[["resolved_biome_ids"]]},
			{"code":"dimensionIds","type":"reference-list","referenceKind":"minecraft.dimension","editable":false,"names":{"en-US":"Dimensions","zh-CN":"维度","zh-TW":"維度"},"paths":[["dimension_ids"]]},
			{"code":"normalizationStatus","type":"text","editable":false,"names":{"en-US":"Normalization status","zh-CN":"规范化状态","zh-TW":"規範化狀態"},"paths":[["normalization_status"]]}
		]`),
		setEntryTypeStorageFieldsSQL("world_structure", 1, `[
			{"code":"catalogKind","type":"text","editable":false,"names":{"en-US":"Catalog kind","zh-CN":"目录类型","zh-TW":"目錄類型"},"paths":[["catalog_kind"]]},
			{"code":"structureType","type":"text","editable":false,"names":{"en-US":"Structure type","zh-CN":"结构类型","zh-TW":"結構類型"},"paths":[["structure_type"]]},
			{"code":"biomeTag","type":"reference","referenceKind":"tag","referenceRegistry":"minecraft:worldgen/biome","names":{"en-US":"Biome tag","zh-CN":"生成群系标签","zh-TW":"生成生態域標籤"},"paths":[["biome_tag"]]},
			{"code":"biomeIds","type":"reference-list","referenceKind":"minecraft.biome","names":{"en-US":"Biomes","zh-CN":"生成群系","zh-TW":"生成生態域"},"paths":[["biome_ids"]]},
			{"code":"generationStep","type":"text","editable":false,"names":{"en-US":"Generation step","zh-CN":"生成阶段","zh-TW":"生成階段"},"paths":[["generation_step"]]},
			{"code":"terrainAdaptation","type":"text","editable":false,"names":{"en-US":"Terrain adaptation","zh-CN":"地形适配","zh-TW":"地形適配"},"paths":[["terrain_adaptation"]]},
			{"code":"startPool","type":"text","editable":false,"names":{"en-US":"Start pool","zh-CN":"起始池","zh-TW":"起始池"},"paths":[["start_pool"]]},
			{"code":"jigsawSize","type":"number","editable":false,"names":{"en-US":"Jigsaw size","zh-CN":"拼图规模","zh-TW":"拼圖規模"},"paths":[["jigsaw_size"]]},
			{"code":"startHeight","type":"json","editable":false,"names":{"en-US":"Start height","zh-CN":"起始高度","zh-TW":"起始高度"},"paths":[["start_height"]]},
			{"code":"maxDistanceFromCenter","type":"number","editable":false,"names":{"en-US":"Maximum distance from center","zh-CN":"距中心最大距离","zh-TW":"距中心最大距離"},"paths":[["max_distance_from_center"]]},
			{"code":"structureSetIds","type":"list","editable":false,"names":{"en-US":"Structure sets","zh-CN":"结构集","zh-TW":"結構集"},"paths":[["structure_set_ids"]]},
			{"code":"definitionSource","type":"text","editable":false,"names":{"en-US":"Definition source","zh-CN":"定义来源","zh-TW":"定義來源"},"paths":[["definition_source"]]}
		]`),
	}
}

func appendEntryTypeFieldsSQL(templateCode string, entryTypeIndex int, fields string) string {
	return `update mod_content_templates set definition=jsonb_set(
		definition,'{entryTypes,` + itoaSmall(entryTypeIndex) + `,groups,0,fields}',
		coalesce(definition#>'{entryTypes,` + itoaSmall(entryTypeIndex) + `,groups,0,fields}','[]'::jsonb)||'` + fields + `'::jsonb
	) where code='` + templateCode + `' and builtin`
}

func setEntryTypeStorageFieldsSQL(templateCode string, entryTypeIndex int, fields string) string {
	return `update mod_content_templates set definition=jsonb_set(
		definition,'{entryTypes,` + itoaSmall(entryTypeIndex) + `,groups}',
		'[{
			"code":"website_data",
			"names":{"en-US":"Website data","zh-CN":"网站资料","zh-TW":"網站資料"},
			"fields":` + fields + `
		}]'::jsonb
	) where code='` + templateCode + `' and builtin`
}

func itoaSmall(value int) string {
	if value >= 0 && value <= 9 {
		return string(rune('0' + value))
	}
	return "0"
}
