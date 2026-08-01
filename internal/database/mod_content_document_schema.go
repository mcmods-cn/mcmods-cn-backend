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
			{"code":"parentId","type":"text","editable":false,"names":{"en-US":"Parent advancement","zh-CN":"父成就","zh-TW":"父成就"},"paths":[["parent"]]},
			{"code":"display","type":"json","editable":false,"names":{"en-US":"Display","zh-CN":"显示信息","zh-TW":"顯示資訊"},"paths":[["display"]]}
		]`),
		setEntryTypeStorageFieldsSQL("loot_table", 1, `[
			{"code":"category","type":"text","editable":false,"names":{"en-US":"Category","zh-CN":"战利品表分类","zh-TW":"戰利品表分類"},"paths":[["category"]]},
			{"code":"path","type":"text","editable":false,"names":{"en-US":"Path","zh-CN":"数据路径","zh-TW":"資料路徑"},"paths":[["path"]]},
			{"code":"definitionAvailable","type":"boolean","editable":false,"names":{"en-US":"Definition available","zh-CN":"定义可用","zh-TW":"定義可用"},"paths":[["definition_available"]]},
			{"code":"pools","type":"json","editable":false,"names":{"en-US":"Loot pools","zh-CN":"战利品池","zh-TW":"戰利品池"},"paths":[["pools"],["definition","pools"]]},
			{"code":"possibleItemIds","type":"list","editable":false,"names":{"en-US":"Possible items","zh-CN":"可能包含的物品","zh-TW":"可能包含的物品"},"paths":[["possible_item_ids"]]},
			{"code":"referencedLootTables","type":"list","editable":false,"names":{"en-US":"Referenced loot tables","zh-CN":"引用的战利品表","zh-TW":"引用的戰利品表"},"paths":[["referenced_loot_tables"]]}
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
			{"code":"resolvedBiomeIds","type":"list","editable":false,"names":{"en-US":"Resolved biomes","zh-CN":"匹配的群系","zh-TW":"匹配的生態域"},"paths":[["resolved_biome_ids"]]},
			{"code":"dimensionIds","type":"list","editable":false,"names":{"en-US":"Dimensions","zh-CN":"维度","zh-TW":"維度"},"paths":[["dimension_ids"]]},
			{"code":"normalizationStatus","type":"text","editable":false,"names":{"en-US":"Normalization status","zh-CN":"规范化状态","zh-TW":"規範化狀態"},"paths":[["normalization_status"]]}
		]`),
		setEntryTypeStorageFieldsSQL("world_structure", 1, `[
			{"code":"catalogKind","type":"text","editable":false,"names":{"en-US":"Catalog kind","zh-CN":"目录类型","zh-TW":"目錄類型"},"paths":[["catalog_kind"]]},
			{"code":"structureType","type":"text","editable":false,"names":{"en-US":"Structure type","zh-CN":"结构类型","zh-TW":"結構類型"},"paths":[["structure_type"]]},
			{"code":"biomes","type":"json","editable":false,"names":{"en-US":"Biomes","zh-CN":"生成群系","zh-TW":"生成生態域"},"paths":[["biomes"]]},
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
