package httpapi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type serverCatalogDatabaseRow struct {
	Item          minecraftServerListItem
	InternalID    int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SortName      string
	HeatSortAsc   int64
	HeatSortDesc  int64
	DownloadCount int64
	FavoriteCount int64
	RatingScore   int64
	RatingCount   int64
	ViewCount     int64
	CommentCount  int64
}

func (s *Server) databaseServerCatalogPage(ctx context.Context, request serverCatalogPageRequest) ([]serverCatalogDatabaseRow, bool, error) {
	where := []string{"server.review_status='approved'"}
	arguments := make([]any, 0, 16)
	add := func(format string, value any) {
		arguments = append(arguments, value)
		where = append(where, fmt.Sprintf(format, len(arguments)))
	}
	if request.Query != "" {
		add(`to_tsvector('simple',server.name||' '||server.short_description||' '||server.body_markdown)
			@@ plainto_tsquery('simple',$%d)`, request.Query)
	}
	if request.Tag != "" {
		add("server.primary_tag=$%d", request.Tag)
	}
	if request.Language != "" {
		add("$%d=any(server.languages)", request.Language)
	}
	if len(request.Versions) > 0 {
		if request.VersionMode == "all" {
			add("server.minecraft_versions @> $%d::text[]", request.Versions)
		} else {
			add("server.minecraft_versions && $%d::text[]", request.Versions)
		}
	}
	for _, modFilter := range request.ModFilters {
		arguments = append(arguments, modFilter)
		position := len(arguments)
		where = append(where, fmt.Sprintf(`exists(
			select 1 from minecraft_server_mods filter_mod
			left join mods collected_mod on collected_mod.id=filter_mod.mod_id
			where filter_mod.server_id=server.id and (
				lower(filter_mod.raw_mod_id)=lower($%d)
				or lower(coalesce(collected_mod.project_code,''))=lower($%d)
				or lower(coalesce(collected_mod.slug,''))=lower($%d)
				or exists(select 1 from mod_identifiers identifier
					where identifier.mod_id=filter_mod.mod_id and lower(identifier.identifier)=lower($%d))
			)
		)`, position, position, position, position))
	}
	for _, filter := range []struct {
		format string
		value  string
	}{
		{"server.modded=$%d", request.Modded}, {"server.last_online=$%d", request.Online},
		{"server.has_whitelist=$%d", request.Whitelist}, {"server.online_mode=$%d", request.OnlineMode},
	} {
		if filter.value == "true" || filter.value == "false" {
			add(filter.format, filter.value == "true")
		}
	}
	if request.ExcludeSiteID != "" {
		add("server.public_id<>$%d", request.ExcludeSiteID)
	}
	if predicate, cursorArguments := serverCatalogDatabaseCursorPredicate(request, len(arguments)+1); predicate != "" {
		where = append(where, predicate)
		arguments = append(arguments, cursorArguments...)
	}
	arguments = append(arguments, request.Limit+1)
	limitParameter := len(arguments)
	heatAsc := serverCatalogDatabaseHeatExpression(catalogSortAscending)
	heatDesc := serverCatalogDatabaseHeatExpression(catalogSortDescending)
	rows, err := s.db.Query(ctx, fmt.Sprintf(`select server.public_id,server.name,server.short_description,
		server.icon_data_uri,server.modded,server.loader,server.languages,server.primary_tag,
		server.minecraft_versions,server.last_online,coalesce(server.last_latency_ms,-1),
		server.last_players_online,server.last_players_max,server.last_checked_at,
		server.id,server.created_at,server.updated_at,lower(server.name),
		%s,%s,coalesce(popularity.download_count,0),coalesce(popularity.favorite_count,0),
		coalesce((popularity.bayesian_rating*10000)::bigint,0),coalesce(popularity.rating_count,0),
		coalesce(popularity.view_count,0),coalesce(popularity.comment_count,0)
		from minecraft_servers server
		left join public_routes popularity_route
			on popularity_route.entity_type='minecraft_server' and popularity_route.internal_id=server.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		where %s
		order by %s
		limit $%d`, heatAsc, heatDesc, strings.Join(where, " and "), serverCatalogDatabaseOrder(request), limitParameter), arguments...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	pageRows := make([]serverCatalogDatabaseRow, 0, request.Limit+1)
	for rows.Next() {
		var row serverCatalogDatabaseRow
		var latency int
		if err = rows.Scan(
			&row.Item.ID, &row.Item.Name, &row.Item.ShortDescription, &row.Item.IconDataURI,
			&row.Item.Modded, &row.Item.Loader, &row.Item.Languages, &row.Item.PrimaryTag,
			&row.Item.MinecraftVersions, &row.Item.Online, &latency, &row.Item.PlayersOnline,
			&row.Item.PlayersMax, &row.Item.LastCheckedAt, &row.InternalID, &row.CreatedAt, &row.UpdatedAt,
			&row.SortName, &row.HeatSortAsc, &row.HeatSortDesc, &row.DownloadCount, &row.FavoriteCount,
			&row.RatingScore, &row.RatingCount, &row.ViewCount, &row.CommentCount,
		); err != nil {
			return nil, false, err
		}
		if latency >= 0 {
			row.Item.LatencyMS = &latency
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	return pageRows, hasMore, nil
}

func serverCatalogDatabaseHeatExpression(direction catalogSortDirection) string {
	onlineValue := "case when server.last_online then 1 else 0 end"
	if direction == catalogSortAscending {
		onlineValue = "case when server.last_online then 0 else 1 end"
	}
	return "coalesce((popularity.heat_score*1000000)::bigint,0)*2+" + onlineValue
}

func serverCatalogDatabaseOrder(request serverCatalogPageRequest) string {
	direction := string(request.Direction)
	stable := func(primary string) string {
		return primary + " " + direction + ",server.updated_at " + direction + ",server.id " + direction
	}
	switch request.Sort {
	case catalogSortPublished:
		return "server.created_at " + direction + ",server.updated_at " + direction + ",server.id " + direction
	case catalogSortUpdated, catalogSortCollected:
		return "server.updated_at " + direction + ",server.id " + direction
	case catalogSortDownloads:
		return stable("coalesce(popularity.download_count,0)")
	case catalogSortFavorites:
		return stable("coalesce(popularity.favorite_count,0)")
	case catalogSortRating:
		return "coalesce((popularity.bayesian_rating*10000)::bigint,0) " + direction +
			",coalesce(popularity.rating_count,0) " + direction + ",server.id " + direction
	case catalogSortViews:
		return stable("coalesce(popularity.view_count,0)")
	case catalogSortComments:
		return stable("coalesce(popularity.comment_count,0)")
	case catalogSortName:
		return "lower(server.name) " + direction + ",server.id " + direction
	default:
		return stable(serverCatalogDatabaseHeatExpression(request.Direction))
	}
}

func serverCatalogDatabaseCursorPredicate(request serverCatalogPageRequest, startParameter int) (string, []any) {
	if request.Cursor == nil || request.Cursor.Mode != serverCatalogCursorSQL {
		return "", nil
	}
	operator := ">"
	if request.Direction == catalogSortDescending {
		operator = "<"
	}
	parameter := func(offset int) string { return "$" + strconv.Itoa(startParameter+offset) }
	switch request.Sort {
	case catalogSortName:
		return fmt.Sprintf("(lower(server.name),server.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{request.Cursor.Name, request.Cursor.ID}
	case catalogSortPublished:
		return fmt.Sprintf("(server.created_at,server.updated_at,server.id) %s (%s,%s,%s)", operator,
			parameter(0), parameter(1), parameter(2)), []any{request.Cursor.CreatedAt, request.Cursor.UpdatedAt, request.Cursor.ID}
	case catalogSortUpdated, catalogSortCollected:
		return fmt.Sprintf("(server.updated_at,server.id) %s (%s,%s)", operator, parameter(0), parameter(1)),
			[]any{request.Cursor.UpdatedAt, request.Cursor.ID}
	case catalogSortRating:
		return fmt.Sprintf("(coalesce((popularity.bayesian_rating*10000)::bigint,0),coalesce(popularity.rating_count,0),server.id) %s (%s,%s,%s)",
			operator, parameter(0), parameter(1), parameter(2)), []any{request.Cursor.Primary, request.Cursor.Secondary, request.Cursor.ID}
	}
	expression := serverCatalogDatabaseHeatExpression(request.Direction)
	switch request.Sort {
	case catalogSortDownloads:
		expression = "coalesce(popularity.download_count,0)"
	case catalogSortFavorites:
		expression = "coalesce(popularity.favorite_count,0)"
	case catalogSortViews:
		expression = "coalesce(popularity.view_count,0)"
	case catalogSortComments:
		expression = "coalesce(popularity.comment_count,0)"
	}
	return fmt.Sprintf("(%s,server.updated_at,server.id) %s (%s,%s,%s)", expression, operator,
		parameter(0), parameter(1), parameter(2)), []any{request.Cursor.Primary, request.Cursor.UpdatedAt, request.Cursor.ID}
}

func serverCatalogDatabaseNextCursor(request serverCatalogPageRequest, row serverCatalogDatabaseRow) string {
	cursor := serverCatalogPageCursor{
		Version: serverCatalogCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, Mode: serverCatalogCursorSQL, ID: row.InternalID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Name: row.SortName,
	}
	switch request.Sort {
	case catalogSortDownloads:
		cursor.Primary = row.DownloadCount
	case catalogSortFavorites:
		cursor.Primary = row.FavoriteCount
	case catalogSortRating:
		cursor.Primary, cursor.Secondary = row.RatingScore, row.RatingCount
	case catalogSortViews:
		cursor.Primary = row.ViewCount
	case catalogSortComments:
		cursor.Primary = row.CommentCount
	case catalogSortName, catalogSortPublished, catalogSortUpdated, catalogSortCollected:
	default:
		if request.Direction == catalogSortAscending {
			cursor.Primary = row.HeatSortAsc
		} else {
			cursor.Primary = row.HeatSortDesc
		}
	}
	return encodeServerCatalogPageCursor(cursor)
}
