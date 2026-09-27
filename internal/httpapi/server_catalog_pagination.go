package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/searchindex"
)

const (
	serverCatalogCursorVersion     = 1
	serverCatalogCursorIndexKeyset = "index_keyset"
	serverCatalogCursorSQL         = "sql"
)

type serverCatalogPageRequest struct {
	Query         string
	ExcludeSiteID string
	Tag           string
	Language      string
	Versions      []string
	VersionMode   string
	ModFilters    []string
	Modded        string
	Online        string
	Whitelist     string
	OnlineMode    string
	Sort          catalogSort
	Direction     catalogSortDirection
	Limit         int
	Scope         string
	Cursor        *serverCatalogPageCursor
}

type serverCatalogPageCursor struct {
	Version   int                  `json:"v"`
	Scope     string               `json:"s"`
	Sort      catalogSort          `json:"sort"`
	Direction catalogSortDirection `json:"direction"`
	Mode      string               `json:"mode"`
	ID        int64                `json:"id,omitempty"`
	Name      string               `json:"name,omitempty"`
	Primary   int64                `json:"primary,omitempty"`
	Secondary int64                `json:"secondary,omitempty"`
	CreatedAt time.Time            `json:"createdAt,omitempty"`
	UpdatedAt time.Time            `json:"updatedAt,omitempty"`
}

func parseServerCatalogPageRequest(values url.Values) (serverCatalogPageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return serverCatalogPageRequest{}, errors.New("server catalog page and offset pagination are not supported")
	}
	query, validQuery := parseCatalogQuery(values.Get("q"))
	excludeSiteID, exclusionErr := parseCatalogPublicIDExclusion(values)
	tag, validTag := parseCatalogScalar(strings.ToLower(values.Get("tag")))
	language, validLanguage := parseCatalogScalar(values.Get("language"))
	versions, validVersions := parseCatalogList(values.Get("version"), 20)
	versionMode, validVersionMode := parseCatalogVersionMode(values.Get("versionMode"))
	modFilters := parseCatalogModFilters(values.Get("mods"))
	modded, validModded := parseCatalogBoolean(values.Get("modded"))
	online, validOnline := parseCatalogBoolean(values.Get("online"))
	whitelist, validWhitelist := parseCatalogBoolean(values.Get("whitelist"))
	onlineMode, validOnlineMode := parseCatalogBoolean(values.Get("onlineMode"))
	if !validQuery || exclusionErr != nil || !validTag || !validLanguage || !validVersions || !validVersionMode ||
		!validModded || !validOnline || !validWhitelist || !validOnlineMode {
		return serverCatalogPageRequest{}, errors.New("invalid server catalog filter")
	}
	rawSort := values.Get("sort")
	if strings.TrimSpace(rawSort) == "" {
		rawSort = string(catalogSortHeat)
	}
	catalogSortField, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(values.Get("order"), catalogSortField)
	// Typesense cannot continue a relevance score with a stable filter. Server
	// search remains useful with every deterministic catalog sort, including
	// the default heat order, so this endpoint deliberately excludes relevance.
	if !validSort || !validDirection || catalogSortField == catalogSortRelevance {
		return serverCatalogPageRequest{}, errors.New("invalid server catalog sort")
	}
	sort.Strings(versions)
	sort.Strings(modFilters)
	limit := boundedLimit(values.Get("limit"), 20, 60)
	request := serverCatalogPageRequest{
		Query: query, ExcludeSiteID: excludeSiteID, Tag: tag, Language: language,
		Versions: versions, VersionMode: versionMode, ModFilters: modFilters,
		Modded: modded, Online: online, Whitelist: whitelist, OnlineMode: onlineMode,
		Sort: catalogSortField, Direction: direction, Limit: limit,
	}
	request.Scope = serverCatalogPageScope(request)
	cursor, err := decodeServerCatalogPageCursor(values.Get("cursor"), request)
	if err != nil {
		return serverCatalogPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func serverCatalogPageScope(request serverCatalogPageRequest) string {
	material, _ := json.Marshal(struct {
		Version       int                  `json:"version"`
		Query         string               `json:"query"`
		ExcludeSiteID string               `json:"excludeSiteId"`
		Tag           string               `json:"tag"`
		Language      string               `json:"language"`
		Versions      []string             `json:"versions"`
		VersionMode   string               `json:"versionMode"`
		ModFilters    []string             `json:"modFilters"`
		Modded        string               `json:"modded"`
		Online        string               `json:"online"`
		Whitelist     string               `json:"whitelist"`
		OnlineMode    string               `json:"onlineMode"`
		Sort          catalogSort          `json:"sort"`
		Direction     catalogSortDirection `json:"direction"`
		Limit         int                  `json:"limit"`
	}{
		serverCatalogCursorVersion, request.Query, request.ExcludeSiteID, request.Tag, request.Language,
		request.Versions, request.VersionMode, request.ModFilters, request.Modded, request.Online,
		request.Whitelist, request.OnlineMode, request.Sort, request.Direction, request.Limit,
	})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeServerCatalogPageCursor(cursor serverCatalogPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeServerCatalogPageCursor(raw string, request serverCatalogPageRequest) (*serverCatalogPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid server catalog cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid server catalog cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor serverCatalogPageCursor
	if err = decoder.Decode(&cursor); err != nil || requireServerCatalogCursorEOF(decoder) != nil ||
		cursor.Version != serverCatalogCursorVersion || cursor.Scope != request.Scope ||
		cursor.Sort != request.Sort || cursor.Direction != request.Direction {
		return nil, errors.New("invalid server catalog cursor")
	}
	if cursor.Mode == serverCatalogCursorSQL {
		if cursor.ID <= 0 || cursor.Primary < 0 || cursor.Secondary < 0 || !validServerCatalogSQLCursor(cursor) {
			return nil, errors.New("invalid server catalog cursor")
		}
		return &cursor, nil
	}
	if cursor.Mode != serverCatalogCursorIndexKeyset || cursor.ID <= 0 || cursor.Primary < 0 || cursor.Secondary < 0 {
		return nil, errors.New("invalid server catalog cursor")
	}
	if request.Sort == catalogSortName {
		return nil, errors.New("invalid server catalog cursor")
	}
	switch request.Sort {
	case catalogSortPublished:
		if cursor.Primary <= 0 || cursor.Secondary <= 0 {
			return nil, errors.New("invalid server catalog cursor")
		}
	case catalogSortUpdated, catalogSortCollected:
		if cursor.Primary <= 0 {
			return nil, errors.New("invalid server catalog cursor")
		}
	}
	return &cursor, nil
}

func validServerCatalogSQLCursor(cursor serverCatalogPageCursor) bool {
	switch cursor.Sort {
	case catalogSortName:
		return strings.TrimSpace(cursor.Name) != ""
	case catalogSortPublished:
		return !cursor.CreatedAt.IsZero() && !cursor.UpdatedAt.IsZero()
	case catalogSortUpdated, catalogSortCollected:
		return !cursor.UpdatedAt.IsZero()
	case catalogSortRating:
		return true
	default:
		return !cursor.UpdatedAt.IsZero()
	}
}

func requireServerCatalogCursorEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if errors.Is(decoder.Decode(&extra), io.EOF) {
		return nil
	}
	return errors.New("invalid server catalog cursor")
}

func serverCatalogNextCursor(request serverCatalogPageRequest, hit searchindex.SearchHit) string {
	cursor := serverCatalogPageCursor{
		Version: serverCatalogCursorVersion, Scope: request.Scope, Sort: request.Sort,
		Direction: request.Direction, Mode: serverCatalogCursorIndexKeyset, ID: hit.InternalID,
	}
	switch request.Sort {
	case catalogSortPublished:
		cursor.Primary, cursor.Secondary = hit.CreatedAt, hit.UpdatedAt
	case catalogSortUpdated, catalogSortCollected:
		cursor.Primary = hit.UpdatedAt
	case catalogSortDownloads:
		cursor.Primary, cursor.Secondary = hit.DownloadCount, hit.UpdatedAt
	case catalogSortFavorites:
		cursor.Primary, cursor.Secondary = hit.FavoriteCount, hit.UpdatedAt
	case catalogSortRating:
		cursor.Primary, cursor.Secondary = hit.RatingScore, hit.RatingCount
	case catalogSortViews:
		cursor.Primary, cursor.Secondary = hit.ViewCount, hit.UpdatedAt
	case catalogSortComments:
		cursor.Primary, cursor.Secondary = hit.CommentCount, hit.UpdatedAt
	default:
		if request.Direction == catalogSortAscending {
			cursor.Primary = hit.HeatSortAsc
		} else {
			cursor.Primary = hit.HeatSortDesc
		}
		cursor.Secondary = hit.UpdatedAt
	}
	return encodeServerCatalogPageCursor(cursor)
}

type serverCatalogCursorField struct {
	Name  string
	Value int64
}

func serverCatalogCursorFilter(request serverCatalogPageRequest) string {
	if request.Cursor == nil || request.Cursor.Mode != serverCatalogCursorIndexKeyset {
		return ""
	}
	fields := make([]serverCatalogCursorField, 0, 3)
	switch request.Sort {
	case catalogSortPublished:
		fields = append(fields,
			serverCatalogCursorField{"created_at", request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	case catalogSortUpdated, catalogSortCollected:
		fields = append(fields, serverCatalogCursorField{"updated_at", request.Cursor.Primary})
	case catalogSortDownloads:
		fields = append(fields, serverCatalogCursorField{"download_count", request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	case catalogSortFavorites:
		fields = append(fields, serverCatalogCursorField{"favorite_count", request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	case catalogSortRating:
		fields = append(fields, serverCatalogCursorField{"rating_score", request.Cursor.Primary},
			serverCatalogCursorField{"rating_count", request.Cursor.Secondary})
	case catalogSortViews:
		fields = append(fields, serverCatalogCursorField{"view_count", request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	case catalogSortComments:
		fields = append(fields, serverCatalogCursorField{"comment_count", request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	default:
		field := "heat_sort_desc"
		if request.Direction == catalogSortAscending {
			field = "heat_sort_asc"
		}
		fields = append(fields, serverCatalogCursorField{field, request.Cursor.Primary},
			serverCatalogCursorField{"updated_at", request.Cursor.Secondary})
	}
	fields = append(fields, serverCatalogCursorField{"internal_id", request.Cursor.ID})
	operator := ">"
	if request.Direction == catalogSortDescending {
		operator = "<"
	}
	clauses := make([]string, 0, len(fields))
	for index, field := range fields {
		parts := make([]string, 0, index+1)
		for prefix := 0; prefix < index; prefix++ {
			parts = append(parts, fields[prefix].Name+":="+strconv.FormatInt(fields[prefix].Value, 10))
		}
		parts = append(parts, field.Name+":"+operator+strconv.FormatInt(field.Value, 10))
		clauses = append(clauses, "("+strings.Join(parts, " && ")+")")
	}
	return "(" + strings.Join(clauses, " || ") + ")"
}

func serverCatalogIndexSort(request serverCatalogPageRequest) string {
	direction := string(request.Direction)
	stable := func(primary string) string {
		return fmt.Sprintf("%s:%s,updated_at:%s,internal_id:%s", primary, direction, direction, direction)
	}
	switch request.Sort {
	case catalogSortPublished:
		return stable("created_at")
	case catalogSortUpdated, catalogSortCollected:
		return fmt.Sprintf("updated_at:%s,internal_id:%s", direction, direction)
	case catalogSortDownloads:
		return stable("download_count")
	case catalogSortFavorites:
		return stable("favorite_count")
	case catalogSortRating:
		return fmt.Sprintf("rating_score:%s,rating_count:%s,internal_id:%s", direction, direction, direction)
	case catalogSortViews:
		return stable("view_count")
	case catalogSortComments:
		return stable("comment_count")
	default:
		field := "heat_sort_desc"
		if request.Direction == catalogSortAscending {
			field = "heat_sort_asc"
		}
		return stable(field)
	}
}

func serverCatalogIndexFilter(request serverCatalogPageRequest) string {
	filters := []string{"review_status:=approved"}
	if request.ExcludeSiteID != "" {
		filters = append(filters, "public_id:!="+typesenseFilterValue(request.ExcludeSiteID))
	}
	if request.Tag != "" {
		filters = append(filters, "primary_tag:="+typesenseFilterValue(request.Tag))
	}
	if request.Language != "" {
		filters = append(filters, "languages:="+typesenseFilterValue(request.Language))
	}
	if len(request.Versions) > 0 {
		if request.VersionMode == "all" {
			for _, version := range request.Versions {
				filters = append(filters, "minecraft_versions:="+typesenseFilterValue(version))
			}
		} else {
			values := make([]string, 0, len(request.Versions))
			for _, version := range request.Versions {
				values = append(values, typesenseFilterValue(version))
			}
			filters = append(filters, "minecraft_versions:=["+strings.Join(values, ",")+"]")
		}
	}
	for _, mod := range request.ModFilters {
		filters = append(filters, "mods:="+typesenseFilterValue(mod))
	}
	for _, filter := range []struct{ field, value string }{
		{"modded", request.Modded}, {"online", request.Online}, {"whitelist", request.Whitelist},
		{"online_mode", request.OnlineMode},
	} {
		if filter.value == "true" || filter.value == "false" {
			filters = append(filters, filter.field+":="+filter.value)
		}
	}
	if cursorFilter := serverCatalogCursorFilter(request); cursorFilter != "" {
		filters = append(filters, cursorFilter)
	}
	return strings.Join(filters, " && ")
}
