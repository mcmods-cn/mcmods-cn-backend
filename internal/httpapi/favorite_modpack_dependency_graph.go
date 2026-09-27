package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

const (
	maxFavoriteExportDependencyNodes = 1000
	maxFavoriteExportDependencyEdges = 4000
	maxFavoriteExportFileConcurrency = 8
)

var errFavoriteExportDependencyLimit = errors.New("favorite export dependency graph exceeds its resource limit")

const favoriteExportDependencyGraphSQL = `with recursive reachable(route_id) as (
	select seed.route_id from unnest($1::bigint[]) seed(route_id)
	union
	select dependent_route.id
	from reachable current
	join public_routes source_route on source_route.id=current.route_id and source_route.entity_type='mod'
	join mod_relationships relationship on relationship.mod_id=source_route.internal_id and relationship.relation_type='dependency'
	join mods dependent on dependent.id=relationship.related_mod_id and dependent.review_status='approved'
	join public_routes dependent_route on dependent_route.entity_type='mod' and dependent_route.internal_id=dependent.id
	left join mod_relationship_groups relation_group on relation_group.id=relationship.group_id
	where (relation_group.id is null or cardinality(relation_group.minecraft_versions)=0 or $2=any(relation_group.minecraft_versions))
	and (relation_group.id is null or relation_group.loader='' or lower(relation_group.loader)=lower($3))
), bounded_routes as materialized (
	select route_id from reachable order by route_id limit $4
), graph_nodes as (
	select route.id route_id,route.public_id,mod.primary_name,
		coalesce(external.external_project_id,'') modrinth_project_id
	from bounded_routes bounded
	join public_routes route on route.id=bounded.route_id and route.entity_type='mod'
	join mods mod on mod.id=route.internal_id
	left join project_external_sources external on external.project_route_id=route.id and external.source_type='modrinth'
), graph_edges as materialized (
	select source_route.id source_route_id,dependent_route.id target_route_id,
		relationship.display_order,relationship.id relationship_id
	from bounded_routes current
	join public_routes source_route on source_route.id=current.route_id and source_route.entity_type='mod'
	join mod_relationships relationship on relationship.mod_id=source_route.internal_id and relationship.relation_type='dependency'
	join mods dependent on dependent.id=relationship.related_mod_id and dependent.review_status='approved'
	join public_routes dependent_route on dependent_route.entity_type='mod' and dependent_route.internal_id=dependent.id
	left join mod_relationship_groups relation_group on relation_group.id=relationship.group_id
	where (relation_group.id is null or cardinality(relation_group.minecraft_versions)=0 or $2=any(relation_group.minecraft_versions))
	and (relation_group.id is null or relation_group.loader='' or lower(relation_group.loader)=lower($3))
	order by source_route.id,relationship.display_order,relationship.id
	limit $5
)
select 0::smallint kind,route_id,public_id,primary_name,modrinth_project_id,
	0::bigint source_route_id,0::bigint target_route_id,0::integer display_order,0::bigint relationship_id
from graph_nodes
union all
select 1::smallint,0::bigint,''::text,''::text,''::text,
	source_route_id,target_route_id,display_order,relationship_id
from graph_edges
order by 1,2,6,8,9`

type favoriteExportDependencyNode struct {
	RouteID           int64
	PublicID          string
	Name              string
	ModrinthProjectID string
}

type favoriteExportDependencyEdge struct {
	SourceRouteID  int64
	TargetRouteID  int64
	DisplayOrder   int
	RelationshipID int64
}

type favoriteExportDependencyGraph struct {
	Nodes map[int64]favoriteExportDependencyNode
	Edges map[int64][]favoriteExportDependencyEdge
}

func (s *Server) loadFavoriteExportDependencyGraph(ctx context.Context, seedRouteIDs []int64, request favoriteModpackExportRequest) (favoriteExportDependencyGraph, error) {
	graph := favoriteExportDependencyGraph{
		Nodes: make(map[int64]favoriteExportDependencyNode),
		Edges: make(map[int64][]favoriteExportDependencyEdge),
	}
	if len(seedRouteIDs) == 0 {
		return graph, nil
	}
	rows, err := s.db.Query(ctx, favoriteExportDependencyGraphSQL, seedRouteIDs, request.MinecraftVersion, request.Loader,
		maxFavoriteExportDependencyNodes+1, maxFavoriteExportDependencyEdges+1)
	if err != nil {
		return graph, fmt.Errorf("query favorite export dependency graph: %w", err)
	}
	defer rows.Close()
	nodeCount, edgeCount := 0, 0
	for rows.Next() {
		var kind int16
		var node favoriteExportDependencyNode
		var edge favoriteExportDependencyEdge
		if err = rows.Scan(&kind, &node.RouteID, &node.PublicID, &node.Name, &node.ModrinthProjectID,
			&edge.SourceRouteID, &edge.TargetRouteID, &edge.DisplayOrder, &edge.RelationshipID); err != nil {
			return graph, fmt.Errorf("scan favorite export dependency graph: %w", err)
		}
		switch kind {
		case 0:
			nodeCount++
			if nodeCount > maxFavoriteExportDependencyNodes {
				return graph, fmt.Errorf("%w: more than %d nodes", errFavoriteExportDependencyLimit, maxFavoriteExportDependencyNodes)
			}
			graph.Nodes[node.RouteID] = node
		case 1:
			edgeCount++
			if edgeCount > maxFavoriteExportDependencyEdges {
				return graph, fmt.Errorf("%w: more than %d edges", errFavoriteExportDependencyLimit, maxFavoriteExportDependencyEdges)
			}
			graph.Edges[edge.SourceRouteID] = append(graph.Edges[edge.SourceRouteID], edge)
		default:
			return graph, errors.New("favorite export dependency graph returned an unknown row kind")
		}
	}
	if err = rows.Err(); err != nil {
		return graph, fmt.Errorf("iterate favorite export dependency graph: %w", err)
	}
	for _, seedRouteID := range seedRouteIDs {
		if _, exists := graph.Nodes[seedRouteID]; !exists {
			return graph, errors.New("favorite export dependency seed disappeared during preflight")
		}
	}
	for routeID := range graph.Edges {
		sort.SliceStable(graph.Edges[routeID], func(left, right int) bool {
			leftEdge, rightEdge := graph.Edges[routeID][left], graph.Edges[routeID][right]
			if leftEdge.DisplayOrder != rightEdge.DisplayOrder {
				return leftEdge.DisplayOrder < rightEdge.DisplayOrder
			}
			return leftEdge.RelationshipID < rightEdge.RelationshipID
		})
	}
	return graph, nil
}

type favoriteExportFileCandidate struct {
	ItemIndex int
	RouteID   int64
	ProjectID string
}

type favoriteExportFileResolution struct {
	File   providerProjectFile
	Reason string
	Detail string
}

type favoriteExportFileResolver func(context.Context, favoriteExportFileCandidate) favoriteExportFileResolution

func resolveFavoriteExportFileCandidates(ctx context.Context, candidates []favoriteExportFileCandidate, resolver favoriteExportFileResolver) ([]favoriteExportFileResolution, error) {
	results := make([]favoriteExportFileResolution, len(candidates))
	if len(candidates) == 0 {
		return results, nil
	}
	workerCount := min(len(candidates), maxFavoriteExportFileConcurrency)
	jobs := make(chan int)
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			for index := range jobs {
				results[index] = resolver(ctx, candidates[index])
			}
		}()
	}
	feedComplete := true
sendJobs:
	for index := range candidates {
		select {
		case jobs <- index:
		case <-ctx.Done():
			feedComplete = false
			break sendJobs
		}
	}
	close(jobs)
	workers.Wait()
	if !feedComplete || ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

func (s *Server) appendFavoriteExportDependencies(ctx context.Context, items []favoriteModpackExportItem, request favoriteModpackExportRequest, cfg modImportConfig) ([]favoriteModpackExportItem, error) {
	knownRoutes := make(map[int64]int)
	seedRouteIDs := make([]int64, 0)
	for index := range items {
		if items[index].SourceProjectRouteID == nil || items[index].ResultType != "exported" {
			continue
		}
		routeID := *items[index].SourceProjectRouteID
		if _, exists := knownRoutes[routeID]; exists {
			continue
		}
		knownRoutes[routeID] = index
		seedRouteIDs = append(seedRouteIDs, routeID)
	}
	graph, err := s.loadFavoriteExportDependencyGraph(ctx, seedRouteIDs, request)
	if err != nil {
		return items, err
	}
	frontier := seedRouteIDs
	for len(frontier) != 0 {
		candidates := make([]favoriteExportFileCandidate, 0)
		candidateByRoute := make(map[int64]int)
		dependencyNames := make([][]string, 0)
		for _, sourceRouteID := range frontier {
			sourceIndex, exists := knownRoutes[sourceRouteID]
			if !exists {
				return items, errors.New("favorite export dependency source is missing from the traversal")
			}
			for _, edge := range graph.Edges[sourceRouteID] {
				if existingIndex, exists := knownRoutes[edge.TargetRouteID]; exists {
					items[existingIndex].DependencyOf = appendUniqueString(items[existingIndex].DependencyOf, items[sourceIndex].SourceProjectName)
					continue
				}
				candidateIndex, exists := candidateByRoute[edge.TargetRouteID]
				if !exists {
					node, nodeExists := graph.Nodes[edge.TargetRouteID]
					if !nodeExists {
						return items, errors.New("favorite export dependency target is missing from the graph")
					}
					candidateIndex = len(candidates)
					candidateByRoute[edge.TargetRouteID] = candidateIndex
					candidates = append(candidates, favoriteExportFileCandidate{RouteID: node.RouteID, ProjectID: node.ModrinthProjectID})
					dependencyNames = append(dependencyNames, nil)
				}
				dependencyNames[candidateIndex] = appendUniqueString(dependencyNames[candidateIndex], items[sourceIndex].SourceProjectName)
			}
		}
		resolutions, resolveErr := resolveFavoriteExportFileCandidates(ctx, candidates, func(resolveContext context.Context, candidate favoriteExportFileCandidate) favoriteExportFileResolution {
			if candidate.ProjectID == "" {
				return favoriteExportFileResolution{Reason: exportReasonNoModrinthSource}
			}
			file, reason, detail := s.resolveFavoriteModrinthProjectFile(resolveContext, candidate.ProjectID, request.MinecraftVersion, request.Loader, cfg)
			return favoriteExportFileResolution{File: file, Reason: reason, Detail: detail}
		})
		if resolveErr != nil {
			return items, resolveErr
		}
		nextFrontier := make([]int64, 0, len(candidates))
		for index, candidate := range candidates {
			node := graph.Nodes[candidate.RouteID]
			item := favoriteModpackExportItem{
				SourceProjectRouteID: int64Pointer(node.RouteID), SourceProjectID: node.PublicID,
				SourceProjectType: "mod", SourceProjectName: node.Name, DependencyOf: dependencyNames[index],
			}
			resolution := resolutions[index]
			if resolution.Reason != "" {
				item.ResultType = "failed"
				item.ReasonCode = exportReasonRequiredDependencyUnresolved
				item.ReasonDetail = firstNonEmpty(resolution.Detail, resolution.Reason)
			} else {
				populateFavoriteExportFile(&item, resolution.File, request)
				item.ResultType = "auto_dependency"
				nextFrontier = append(nextFrontier, node.RouteID)
			}
			knownRoutes[node.RouteID] = len(items)
			items = append(items, item)
		}
		frontier = nextFrontier
	}
	return items, nil
}

func favoriteExportHasRequiredDependencyFailure(items []favoriteModpackExportItem) bool {
	for _, item := range items {
		if item.ReasonCode == exportReasonRequiredDependencyUnresolved {
			return true
		}
	}
	return false
}
