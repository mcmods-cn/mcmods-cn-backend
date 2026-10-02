package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type modExportBlockBinding struct {
	RevisionID              string
	BlockID                 string
	ItemID                  string
	BlockResourceSnapshotID string
	ItemResourceSnapshotID  string
	Blockstate              string
	ItemModel               string
	ModelPaths              []string
	TexturePaths            []string
}

type exportRegistryLinkEntry struct {
	ID        string `json:"id"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
	Item      string `json:"item"`
	Block     string `json:"block"`
}

type exportRegistryLinkDocument struct {
	Registry string                    `json:"registry"`
	Entries  []exportRegistryLinkEntry `json:"entries"`
}

type exportResourcePathIndex struct {
	files      map[string]*zip.File
	models     map[string]map[string]string
	textures   map[string]map[string]string
	jsonCache  map[string]map[string]any
	jsonErrors map[string]error
}

func deriveModExportBlockBindings(files map[string]*zip.File, resolver catalogResourceIdentityResolver, revisions map[string]string) ([]modExportBlockBinding, error) {
	blocks, err := readExportRegistryLinks(files["registries/blocks.json"])
	if err != nil {
		return nil, fmt.Errorf("derive block bindings: %w", err)
	}
	items, err := readExportRegistryLinks(files["registries/items.json"])
	if err != nil {
		return nil, fmt.Errorf("derive item bindings: %w", err)
	}
	itemsByBlock := make(map[string]string, len(items))
	for _, item := range items {
		if item.Block != "" {
			itemsByBlock[item.Block] = item.ID
		}
	}
	index := newExportResourcePathIndex(files, revisions)
	result := make([]modExportBlockBinding, 0, len(blocks))
	for _, block := range blocks {
		revisionID, revisionErr := exportRevisionForNamespace(revisions, block.Namespace)
		if revisionErr != nil {
			return nil, fmt.Errorf("block binding %q: %w", block.ID, revisionErr)
		}
		itemID := strings.TrimSpace(block.Item)
		if itemID == "" {
			itemID = itemsByBlock[block.ID]
		}
		blockstate := exportBlockstatePath(block)
		if files[blockstate] == nil {
			blockstate = ""
		}
		itemModel := exportItemModelPath(itemID)
		if files[itemModel] == nil {
			itemModel = ""
		}
		modelPaths, texturePaths, collectErr := index.collect(revisionID, block.Namespace, blockstate, itemModel)
		if collectErr != nil {
			return nil, fmt.Errorf("derive block resources %s: %w", block.ID, collectErr)
		}
		blockResource := resolver.resolve("minecraft.block", block.ID)
		itemResourceSnapshotID := ""
		if itemID != "" {
			itemResource := resolver.resolve("minecraft.item", itemID)
			itemResourceSnapshotID = catalogSnapshotID("resource", revisionID, itemResource.ID, "")
		}
		result = append(result, modExportBlockBinding{
			RevisionID: revisionID, BlockID: block.ID, ItemID: itemID,
			BlockResourceSnapshotID: catalogSnapshotID("resource", revisionID, blockResource.ID, ""),
			ItemResourceSnapshotID:  itemResourceSnapshotID,
			Blockstate:              blockstate, ItemModel: itemModel,
			ModelPaths: modelPaths, TexturePaths: texturePaths,
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].RevisionID == result[right].RevisionID {
			return result[left].BlockID < result[right].BlockID
		}
		return result[left].RevisionID < result[right].RevisionID
	})
	return result, nil
}

func readExportRegistryLinks(file *zip.File) ([]exportRegistryLinkEntry, error) {
	if file == nil {
		return nil, nil
	}
	raw, err := readExportZIPFile(file, maxExportSingleFileSize)
	if err != nil {
		return nil, err
	}
	var document exportRegistryLinkDocument
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return document.Entries, nil
}

func newExportResourcePathIndex(files map[string]*zip.File, revisions map[string]string) *exportResourcePathIndex {
	index := &exportResourcePathIndex{
		files: files, models: make(map[string]map[string]string), textures: make(map[string]map[string]string),
		jsonCache: make(map[string]map[string]any), jsonErrors: make(map[string]error),
	}
	for assetPath := range files {
		revisionID := exportRevisionForPath(revisions, assetPath)
		if revisionID == "" {
			continue
		}
		if resourceID, aliases, ok := exportAssetResourceIDs(assetPath, "models"); ok {
			if index.models[revisionID] == nil {
				index.models[revisionID] = make(map[string]string)
			}
			index.models[revisionID][resourceID] = assetPath
			for _, alias := range aliases {
				index.models[revisionID][alias] = assetPath
			}
		}
		if resourceID, aliases, ok := exportAssetResourceIDs(assetPath, "textures"); ok {
			if index.textures[revisionID] == nil {
				index.textures[revisionID] = make(map[string]string)
			}
			index.textures[revisionID][resourceID] = assetPath
			for _, alias := range aliases {
				index.textures[revisionID][alias] = assetPath
			}
		}
	}
	return index
}

func (index *exportResourcePathIndex) collect(revisionID, defaultNamespace string, roots ...string) ([]string, []string, error) {
	models := make(map[string]struct{})
	textures := make(map[string]struct{})
	visited := make(map[string]struct{})
	queue := make([]string, 0, len(roots)+8)
	for _, root := range roots {
		if root != "" {
			queue = append(queue, root)
		}
	}
	for len(queue) > 0 {
		assetPath := queue[0]
		queue = queue[1:]
		if _, exists := visited[assetPath]; exists {
			continue
		}
		visited[assetPath] = struct{}{}
		if strings.Contains(assetPath, "/models/") {
			models[assetPath] = struct{}{}
		}
		if !strings.HasSuffix(strings.ToLower(assetPath), ".json") {
			continue
		}
		document, err := index.json(assetPath)
		if err != nil {
			return nil, nil, err
		}
		namespace := exportAssetNamespace(assetPath)
		if namespace == "" {
			namespace = defaultNamespace
		}
		visitExportStrings(document, func(value string) {
			for _, candidate := range exportResourceReferenceCandidates(value, namespace) {
				if modelPath := index.models[revisionID][candidate]; modelPath != "" {
					queue = append(queue, modelPath)
				}
				if texturePath := index.textures[revisionID][candidate]; texturePath != "" {
					textures[texturePath] = struct{}{}
				}
			}
		})
	}
	return sortedExportPaths(models), sortedExportPaths(textures), nil
}

func (index *exportResourcePathIndex) json(assetPath string) (map[string]any, error) {
	if cached, exists := index.jsonCache[assetPath]; exists {
		return cached, nil
	}
	if err, exists := index.jsonErrors[assetPath]; exists {
		return nil, err
	}
	file := index.files[assetPath]
	if file == nil {
		return nil, fmt.Errorf("asset is missing: %s", assetPath)
	}
	raw, err := readExportZIPFile(file, maxExportSingleFileSize)
	if err == nil {
		var document map[string]any
		err = json.Unmarshal(raw, &document)
		if err == nil {
			index.jsonCache[assetPath] = document
			return document, nil
		}
	}
	index.jsonErrors[assetPath] = err
	return nil, err
}

func persistModExportBlockBindings(ctx context.Context, tx pgx.Tx, bindings []modExportBlockBinding) error {
	if len(bindings) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, binding := range bindings {
		var itemSnapshotID any
		if binding.ItemResourceSnapshotID != "" {
			itemSnapshotID = binding.ItemResourceSnapshotID
		}
		batch.Queue(`insert into game_resource_asset_bindings(snapshot_id,item_resource_id,block_resource_id,blockstate_path,item_model_path,model_paths,texture_paths)
			select snapshot.id,item_snapshot.resource_id,snapshot.resource_id,$3,$4,$5,$6
			from resource_import_snapshots snapshot
			left join resource_import_snapshots item_snapshot
				on item_snapshot.id=$1 and item_snapshot.revision_id=snapshot.revision_id
			where snapshot.id=$2 and snapshot.revision_id=$7
			on conflict(snapshot_id) do update set item_resource_id=excluded.item_resource_id,block_resource_id=excluded.block_resource_id,
			blockstate_path=excluded.blockstate_path,item_model_path=excluded.item_model_path,
			model_paths=excluded.model_paths,texture_paths=excluded.texture_paths`, itemSnapshotID, binding.BlockResourceSnapshotID,
			binding.Blockstate, binding.ItemModel, binding.ModelPaths, binding.TexturePaths, binding.RevisionID)
	}
	results := tx.SendBatch(ctx, batch)
	defer results.Close()
	for index := range bindings {
		tag, err := results.Exec()
		if err != nil {
			return fmt.Errorf("persist block asset binding: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("persist block asset binding %s: matching resource snapshot not found", bindings[index].BlockID)
		}
	}
	return results.Close()
}

func exportBlockstatePath(entry exportRegistryLinkEntry) string {
	if entry.Namespace == "" || entry.Path == "" {
		return ""
	}
	return "assets/" + strings.ToLower(entry.Namespace) + "/blockstates/" + entry.Path + ".json"
}

func exportItemModelPath(itemID string) string {
	namespace, objectPath, ok := strings.Cut(strings.TrimSpace(itemID), ":")
	if !ok || namespace == "" || objectPath == "" {
		return ""
	}
	return "assets/" + strings.ToLower(namespace) + "/models/item/" + objectPath + ".json"
}

func exportAssetResourceIDs(assetPath, directory string) (string, []string, bool) {
	parts := strings.Split(assetPath, "/")
	if len(parts) < 5 || parts[0] != "assets" || parts[2] != directory {
		return "", nil, false
	}
	namespace := strings.ToLower(parts[1])
	relative := strings.Join(parts[3:], "/")
	extension := path.Ext(relative)
	if extension == "" {
		return "", nil, false
	}
	withoutExtension := strings.TrimSuffix(relative, extension)
	resourceID := namespace + ":" + withoutExtension
	aliases := []string{namespace + ":" + directory + "/" + relative, namespace + ":" + relative}
	return resourceID, aliases, true
}

func exportAssetNamespace(assetPath string) string {
	parts := strings.Split(assetPath, "/")
	if len(parts) >= 3 && parts[0] == "assets" {
		return strings.ToLower(parts[1])
	}
	return ""
}

func exportResourceReferenceCandidates(value, defaultNamespace string) []string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "#") || strings.ContainsAny(value, " \t\r\n") {
		return nil
	}
	if strings.HasPrefix(value, "assets/") {
		return []string{value}
	}
	if !strings.Contains(value, ":") {
		value = defaultNamespace + ":" + value
	}
	withoutExtension := strings.TrimSuffix(value, path.Ext(value))
	if withoutExtension == value {
		return []string{value}
	}
	return []string{value, withoutExtension}
}

func visitExportStrings(value any, visit func(string)) {
	switch typed := value.(type) {
	case string:
		visit(typed)
	case []any:
		for _, item := range typed {
			visitExportStrings(item, visit)
		}
	case map[string]any:
		for _, item := range typed {
			visitExportStrings(item, visit)
		}
	}
}

func sortedExportPaths(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
