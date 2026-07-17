package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	modExportBlockEntityIndexPath = "block_entities/index.json"
	blockEntityIndexSchema        = "mcmods-block-entity-models/v1"
	blockEntityMeshSchema         = "mcmods-uv-quad-mesh/v1"
)

type exportBlockEntityIndex struct {
	SchemaVersion   string            `json:"schema_version"`
	CoordinateSpace string            `json:"coordinate_space"`
	Count           int               `json:"count"`
	BlockEntities   []json.RawMessage `json:"block_entities"`
}

type exportBlockEntityModel struct {
	BlockID         string            `json:"block_id"`
	BlockEntityType string            `json:"block_entity_type_id"`
	ModelSource     string            `json:"model_source"`
	VariantCount    int               `json:"variant_count"`
	ModelAvailable  bool              `json:"model_available"`
	Variants        []json.RawMessage `json:"variants"`
}

type exportBlockEntityVariant struct {
	VariantID    string          `json:"variant_id"`
	OBJPath      string          `json:"obj"`
	MeshPath     string          `json:"mesh"`
	VertexCount  int             `json:"vertex_count"`
	QuadCount    int             `json:"quad_count"`
	UVSpace      string          `json:"uv_space"`
	UVComplete   bool            `json:"uv_complete"`
	TextureCount int             `json:"texture_count"`
	Textures     json.RawMessage `json:"textures"`
}

type exportBlockEntityTexture struct {
	Path string `json:"path"`
}

type exportUVQuadMesh struct {
	SchemaVersion   string             `json:"schema_version"`
	CoordinateSpace string             `json:"coordinate_space"`
	UVSpace         string             `json:"uv_space"`
	UVOrigin        string             `json:"uv_origin"`
	VertexCount     int                `json:"vertex_count"`
	QuadCount       int                `json:"quad_count"`
	Faces           []exportUVQuadFace `json:"faces"`
}

type exportUVQuadFace struct {
	Vertices []exportUVQuadVertex `json:"vertices"`
}

type exportUVQuadVertex struct {
	Position []float64 `json:"position"`
	UV       []float64 `json:"uv"`
	Normal   []float64 `json:"normal"`
}

type modExportBlockEntityModelRow struct {
	ID                 string
	ResourceSnapshotID string
	RevisionID         string
	BlockResourceID    string
	BlockID            string
	BlockEntityTypeID  string
	ModelSource        string
	ModelAvailable     bool
	VariantCount       int
	Data               string
	Variants           []modExportBlockEntityVariantRow
}

type modExportBlockEntityVariantRow struct {
	ID              string
	ModelSnapshotID string
	VariantID       string
	OBJPath         string
	MeshPath        string
	VertexCount     int
	QuadCount       int
	CoordinateSpace string
	UVSpace         string
	UVOrigin        string
	UVComplete      bool
	Textures        string
	MeshData        string
	Data            string
}

func deriveModExportBlockEntityModels(files map[string]*zip.File, revisions map[string]string) ([]modExportBlockEntityModelRow, error) {
	indexFile := files[modExportBlockEntityIndexPath]
	if indexFile == nil {
		return nil, nil
	}
	raw, err := readExportZIPFile(indexFile, maxExportJSONSize)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", modExportBlockEntityIndexPath, err)
	}
	var index exportBlockEntityIndex
	if err = json.Unmarshal(raw, &index); err != nil {
		return nil, fmt.Errorf("decode %s: %w", modExportBlockEntityIndexPath, err)
	}
	if index.SchemaVersion != blockEntityIndexSchema {
		return nil, fmt.Errorf("unsupported block entity model schema: %s", index.SchemaVersion)
	}
	if index.CoordinateSpace != "block_units" || index.Count != len(index.BlockEntities) {
		return nil, fmt.Errorf("invalid block entity model index metadata")
	}
	models := make([]modExportBlockEntityModelRow, 0, len(index.BlockEntities))
	seenBlocks := make(map[string]struct{}, len(index.BlockEntities))
	for _, modelRaw := range index.BlockEntities {
		var model exportBlockEntityModel
		if err = json.Unmarshal(modelRaw, &model); err != nil {
			return nil, fmt.Errorf("decode block entity model: %w", err)
		}
		model.BlockID = strings.TrimSpace(model.BlockID)
		model.BlockEntityType = strings.TrimSpace(model.BlockEntityType)
		if model.BlockID == "" || model.BlockEntityType == "" || model.VariantCount != len(model.Variants) {
			return nil, fmt.Errorf("invalid block entity model %q", model.BlockID)
		}
		if _, duplicate := seenBlocks[model.BlockID]; duplicate {
			return nil, fmt.Errorf("duplicate block entity model %s", model.BlockID)
		}
		seenBlocks[model.BlockID] = struct{}{}
		namespace, _ := resourceParts(model.BlockID)
		revisionID := revisions[strings.ToLower(namespace)]
		if revisionID == "" {
			continue
		}
		resource := resourceIdentity(resourceKindForRegistry("blocks"), model.BlockID)
		modelID := catalogSnapshotID("block-entity-model", revisionID, resource.ID, "")
		row := modExportBlockEntityModelRow{
			ID: modelID, ResourceSnapshotID: catalogSnapshotID("resource", revisionID, resource.ID, ""),
			RevisionID: revisionID, BlockResourceID: resource.ID, BlockID: model.BlockID,
			BlockEntityTypeID: model.BlockEntityType, ModelSource: strings.TrimSpace(model.ModelSource),
			ModelAvailable: model.ModelAvailable, VariantCount: model.VariantCount, Data: string(modelRaw),
			Variants: make([]modExportBlockEntityVariantRow, 0, len(model.Variants)),
		}
		seenVariants := make(map[string]struct{}, len(model.Variants))
		for _, variantRaw := range model.Variants {
			variant, meshRaw, mesh, decodeErr := decodeExportBlockEntityVariant(files, model.BlockID, variantRaw)
			if decodeErr != nil {
				return nil, decodeErr
			}
			if _, duplicate := seenVariants[variant.VariantID]; duplicate {
				return nil, fmt.Errorf("duplicate block entity variant %s/%s", model.BlockID, variant.VariantID)
			}
			seenVariants[variant.VariantID] = struct{}{}
			row.Variants = append(row.Variants, modExportBlockEntityVariantRow{
				ID:              catalogSnapshotID("block-entity-variant", modelID, variant.VariantID, ""),
				ModelSnapshotID: modelID, VariantID: variant.VariantID, OBJPath: variant.OBJPath,
				MeshPath: variant.MeshPath, VertexCount: mesh.VertexCount, QuadCount: mesh.QuadCount,
				CoordinateSpace: mesh.CoordinateSpace, UVSpace: mesh.UVSpace, UVOrigin: mesh.UVOrigin,
				UVComplete: variant.UVComplete, Textures: nonEmptyJSON(variant.Textures, `[]`),
				MeshData: string(meshRaw), Data: string(variantRaw),
			})
		}
		models = append(models, row)
	}
	return models, nil
}

func decodeExportBlockEntityVariant(files map[string]*zip.File, blockID string, raw json.RawMessage) (exportBlockEntityVariant, []byte, exportUVQuadMesh, error) {
	var variant exportBlockEntityVariant
	var mesh exportUVQuadMesh
	if err := json.Unmarshal(raw, &variant); err != nil {
		return variant, nil, mesh, fmt.Errorf("decode block entity variant for %s: %w", blockID, err)
	}
	variant.VariantID = strings.TrimSpace(variant.VariantID)
	if variant.VariantID == "" || !validExportPath(variant.OBJPath, "block_entities/models/", ".obj") ||
		!validExportPath(variant.MeshPath, "block_entities/models/", ".mesh.json") {
		return variant, nil, mesh, fmt.Errorf("invalid block entity variant for %s", blockID)
	}
	if files[variant.OBJPath] == nil || files[variant.MeshPath] == nil {
		return variant, nil, mesh, fmt.Errorf("block entity variant %s/%s references a missing model asset", blockID, variant.VariantID)
	}
	var textures []exportBlockEntityTexture
	if err := json.Unmarshal([]byte(nonEmptyJSON(variant.Textures, `[]`)), &textures); err != nil || variant.TextureCount != len(textures) {
		return variant, nil, mesh, fmt.Errorf("invalid block entity textures for %s/%s", blockID, variant.VariantID)
	}
	for _, texture := range textures {
		if !validExportPath(texture.Path, "assets/", ".png") || files[texture.Path] == nil {
			return variant, nil, mesh, fmt.Errorf("block entity variant %s/%s references a missing texture", blockID, variant.VariantID)
		}
	}
	meshRaw, err := readExportZIPFile(files[variant.MeshPath], maxExportJSONSize)
	if err != nil {
		return variant, nil, mesh, fmt.Errorf("read block entity mesh %s: %w", variant.MeshPath, err)
	}
	if err = json.Unmarshal(meshRaw, &mesh); err != nil {
		return variant, nil, mesh, fmt.Errorf("decode block entity mesh %s: %w", variant.MeshPath, err)
	}
	if mesh.SchemaVersion != blockEntityMeshSchema || mesh.CoordinateSpace != "block_units" ||
		mesh.QuadCount != len(mesh.Faces) || mesh.VertexCount != mesh.QuadCount*4 ||
		mesh.VertexCount != variant.VertexCount || mesh.QuadCount != variant.QuadCount || mesh.UVSpace != variant.UVSpace {
		return variant, nil, mesh, fmt.Errorf("invalid block entity mesh metadata: %s", variant.MeshPath)
	}
	for _, face := range mesh.Faces {
		if len(face.Vertices) != 4 {
			return variant, nil, mesh, fmt.Errorf("block entity mesh %s contains a non-quad face", variant.MeshPath)
		}
		for _, vertex := range face.Vertices {
			if len(vertex.Position) != 3 || len(vertex.UV) != 2 || len(vertex.Normal) != 3 {
				return variant, nil, mesh, fmt.Errorf("block entity mesh %s contains an invalid vertex", variant.MeshPath)
			}
		}
	}
	return variant, meshRaw, mesh, nil
}

func persistModExportBlockEntityModels(ctx context.Context, tx pgx.Tx, models []modExportBlockEntityModelRow) error {
	if len(models) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `create temporary table import_block_entity_model_stage(
		id text,resource_snapshot_id text,revision_id text,block_resource_id text,block_id text,
		block_entity_type_id text,model_source text,model_available boolean,variant_count integer,data jsonb
	) on commit drop`); err != nil {
		return err
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"import_block_entity_model_stage"},
		[]string{"id", "resource_snapshot_id", "revision_id", "block_resource_id", "block_id", "block_entity_type_id", "model_source", "model_available", "variant_count", "data"},
		pgx.CopyFromSlice(len(models), func(index int) ([]any, error) {
			row := models[index]
			return []any{row.ID, row.ResourceSnapshotID, row.RevisionID, row.BlockResourceID, row.BlockID,
				row.BlockEntityTypeID, row.ModelSource, row.ModelAvailable, row.VariantCount, row.Data}, nil
		})); err != nil {
		return fmt.Errorf("copy block entity models: %w", err)
	}
	tag, err := tx.Exec(ctx, `insert into block_entity_model_snapshots(id,resource_snapshot_id,revision_id,block_resource_id,
		block_id,block_entity_type_id,model_source,model_available,variant_count,data)
		select id,resource_snapshot_id,revision_id,block_resource_id,block_id,block_entity_type_id,model_source,
		model_available,variant_count,data from import_block_entity_model_stage
		on conflict(revision_id,block_id) do update set block_entity_type_id=excluded.block_entity_type_id,
		model_source=excluded.model_source,model_available=excluded.model_available,variant_count=excluded.variant_count,data=excluded.data`)
	if err != nil {
		return fmt.Errorf("persist block entity models: %w", err)
	}
	if tag.RowsAffected() != int64(len(models)) {
		return fmt.Errorf("persist block entity models: wrote %d of %d rows", tag.RowsAffected(), len(models))
	}
	variants := make([]modExportBlockEntityVariantRow, 0)
	for _, model := range models {
		variants = append(variants, model.Variants...)
	}
	if len(variants) == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `create temporary table import_block_entity_variant_stage(
		id text,model_snapshot_id text,variant_id text,obj_path text,mesh_path text,vertex_count integer,quad_count integer,
		coordinate_space text,uv_space text,uv_origin text,uv_complete boolean,textures jsonb,mesh_data jsonb,data jsonb
	) on commit drop`); err != nil {
		return err
	}
	if _, err = tx.CopyFrom(ctx, pgx.Identifier{"import_block_entity_variant_stage"},
		[]string{"id", "model_snapshot_id", "variant_id", "obj_path", "mesh_path", "vertex_count", "quad_count", "coordinate_space", "uv_space", "uv_origin", "uv_complete", "textures", "mesh_data", "data"},
		pgx.CopyFromSlice(len(variants), func(index int) ([]any, error) {
			row := variants[index]
			return []any{row.ID, row.ModelSnapshotID, row.VariantID, row.OBJPath, row.MeshPath, row.VertexCount,
				row.QuadCount, row.CoordinateSpace, row.UVSpace, row.UVOrigin, row.UVComplete, row.Textures, row.MeshData, row.Data}, nil
		})); err != nil {
		return fmt.Errorf("copy block entity model variants: %w", err)
	}
	_, err = tx.Exec(ctx, `insert into block_entity_model_variants(id,model_snapshot_id,variant_id,obj_path,mesh_path,
		vertex_count,quad_count,coordinate_space,uv_space,uv_origin,uv_complete,textures,mesh_data,data)
		select id,model_snapshot_id,variant_id,obj_path,mesh_path,vertex_count,quad_count,coordinate_space,uv_space,
		uv_origin,uv_complete,textures,mesh_data,data from import_block_entity_variant_stage
		on conflict(model_snapshot_id,variant_id) do update set obj_path=excluded.obj_path,mesh_path=excluded.mesh_path,
		vertex_count=excluded.vertex_count,quad_count=excluded.quad_count,coordinate_space=excluded.coordinate_space,
		uv_space=excluded.uv_space,uv_origin=excluded.uv_origin,uv_complete=excluded.uv_complete,
		textures=excluded.textures,mesh_data=excluded.mesh_data,data=excluded.data`)
	return err
}
