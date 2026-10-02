package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

const ratingMessageLimit = 2000

type ratingDimensionDefinition struct {
	Code string `json:"code"`
}

type ratingDimensionSummary struct {
	Code    string  `json:"code"`
	Average float64 `json:"average"`
	Count   int64   `json:"count"`
}

type ratingEngagementSummary struct {
	Views     int64 `json:"views"`
	Downloads int64 `json:"downloads"`
	Favorites int64 `json:"favorites"`
	Comments  int64 `json:"comments"`
}

type ratingItemResponse struct {
	ID           int64          `json:"-"`
	PublicID     string         `json:"id"`
	AuthorID     string         `json:"authorId,omitempty"`
	AuthorName   string         `json:"authorName,omitempty"`
	AuthorAvatar string         `json:"authorAvatar,omitempty"`
	OverallScore int            `json:"overallScore"`
	Scores       map[string]int `json:"scores"`
	Message      string         `json:"message"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type ratingSummaryResponse struct {
	TargetType     string                   `json:"targetType"`
	TargetID       string                   `json:"targetId"`
	OverallAverage float64                  `json:"overallAverage"`
	RatingCount    int64                    `json:"ratingCount"`
	Dimensions     []ratingDimensionSummary `json:"dimensions"`
	HeatScore      float64                  `json:"heatScore"`
	HeatComponents ratingHeatComponents     `json:"heatComponents"`
	Engagement     ratingEngagementSummary  `json:"engagement"`
	CanRate        bool                     `json:"canRate"`
	CanViewReviews bool                     `json:"canViewReviews"`
	MyRating       *ratingItemResponse      `json:"myRating,omitempty"`
}

type ratingHeatComponents struct {
	LongTerm      float64 `json:"longTerm"`
	Trend         float64 `json:"trend"`
	EffectiveView float64 `json:"effectiveView"`
	Promotion     float64 `json:"promotion"`
	Quality       float64 `json:"quality"`
	NewProject    float64 `json:"newProject"`
}

type ratingListResponse struct {
	Items      []ratingItemResponse `json:"items"`
	Limit      int                  `json:"limit"`
	HasMore    bool                 `json:"hasMore"`
	NextCursor string               `json:"nextCursor"`
}

type ratingUpsertRequest struct {
	OverallScore int            `json:"overallScore"`
	Scores       map[string]int `json:"scores"`
	Message      string         `json:"message"`
}

type rateableTarget struct {
	RouteID    int64
	InternalID int64
	Type       string
	PublicID   string
}

var ratingDimensions = map[string][]ratingDimensionDefinition{
	"mod":              dimensions("content", "fun", "utility", "balance", "stability", "compatibility", "usability", "completeness"),
	"modpack":          dimensions("content_richness", "gameplay_design", "quest_design", "balance", "difficulty_fairness", "performance_optimization", "stability", "completeness"),
	"plugin":           dimensions("utility", "functionality", "usability", "stability", "compatibility", "performance", "configurability", "completeness"),
	"addon":            dimensions("content_richness", "production_quality", "style_consistency", "utility", "balance", "compatibility", "performance", "completeness"),
	"shader_pack":      dimensions("visual_presentation", "style", "performance", "compatibility", "stability", "configurability", "scene_adaptability"),
	"resource_pack":    dimensions("visual_quality", "style", "style_consistency", "coverage", "compatibility", "performance", "utility"),
	"datapack":         dimensions("gameplay", "creativity", "utility", "balance", "stability", "compatibility", "performance", "completeness"),
	"map":              dimensions("gameplay_design", "scene_design", "art_quality", "creativity", "difficulty_fairness", "guidance", "stability", "completeness"),
	"minecraft_server": dimensions("gameplay_quality", "content_richness", "stability", "smoothness", "fairness", "community", "management", "operations"),
}

func dimensions(codes ...string) []ratingDimensionDefinition {
	result := make([]ratingDimensionDefinition, 0, len(codes))
	for _, code := range codes {
		result = append(result, ratingDimensionDefinition{Code: code})
	}
	return result
}

func normalizeRatingTargetType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if _, ok := ratingDimensions[value]; ok {
		return value
	}
	return ""
}

func (s *Server) ratingSummary(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveRateableTarget(r.Context(), r.PathValue("targetType"), r.PathValue("publicId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rating target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve rating target")
		return
	}
	if err = s.ensurePopularityStats(r.Context(), target.RouteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare rating summary")
		return
	}
	response, err := s.loadRatingSummary(r.Context(), target, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load rating summary")
		return
	}
	claims := currentClaims(r)
	response.CanRate = claimsAllow(claims, "rating.create")
	response.CanViewReviews = claimsAllow(claims, "rating.read")
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) ratingItem(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveRateableTarget(r.Context(), r.PathValue("targetType"), r.PathValue("publicId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rating target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve rating target")
		return
	}
	if r.Method == http.MethodDelete {
		s.deleteRating(w, r, target)
		return
	}
	var request ratingUpsertRequest
	if err = decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "rating payload is invalid")
		return
	}
	request.Message = strings.TrimSpace(request.Message)
	if err = validateRatingRequest(target.Type, request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin rating update")
		return
	}
	defer tx.Rollback(r.Context())
	var existingID int64
	var item ratingItemResponse
	err = tx.QueryRow(r.Context(), `update content_ratings set overall_score=$3,message=$4,status='published',updated_at=now()
		where object_route_id=$1 and author_id=$2
		returning id,public_id,overall_score,message,created_at,updated_at`, target.RouteID, currentClaims(r).Subject,
		request.OverallScore, request.Message).Scan(&item.ID, &item.PublicID, &item.OverallScore, &item.Message, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		existingID = item.ID
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(r.Context(), `insert into content_ratings(object_route_id,author_id,overall_score,message)
			values($1,$2,$3,$4)
			on conflict(object_route_id,author_id) do update set
				overall_score=excluded.overall_score,message=excluded.message,status='published',updated_at=now()
			returning id,public_id,overall_score,message,created_at,updated_at`, target.RouteID, currentClaims(r).Subject,
			request.OverallScore, request.Message).Scan(&item.ID, &item.PublicID, &item.OverallScore, &item.Message, &item.CreatedAt, &item.UpdatedAt)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save rating")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from content_rating_scores where rating_id=$1`, item.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace rating dimensions")
		return
	}
	for _, dimension := range ratingDimensions[target.Type] {
		if _, err = tx.Exec(r.Context(), `insert into content_rating_scores(rating_id,dimension_code,score) values($1,$2,$3)`,
			item.ID, dimension.Code, request.Scores[dimension.Code]); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save rating dimensions")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit rating")
		return
	}
	item.Scores = request.Scores
	actionID := activity.ActionCreate
	if existingID > 0 {
		actionID = activity.ActionEdit
	}
	annotateActivity(r, actionID, activity.ObjectRating, target.PublicID, 0)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) deleteRating(w http.ResponseWriter, r *http.Request, target rateableTarget) {
	result, err := s.db.Exec(r.Context(), `delete from content_ratings where object_route_id=$1 and author_id=$2`,
		target.RouteID, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete rating")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "rating not found")
		return
	}
	annotateActivity(r, activity.ActionDelete, activity.ObjectRating, target.PublicID, 0)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ratingReviews(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveRateableTarget(r.Context(), r.PathValue("targetType"), r.PathValue("publicId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rating target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve rating target")
		return
	}
	page, err := parseRatingReviewPageRequest(r.URL.Query(), target)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, arguments := ratingReviewPageSQL(page)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ratings")
		return
	}
	defer rows.Close()
	pageRows := make([]ratingReviewPageRow, 0, page.Limit+1)
	for rows.Next() {
		var row ratingReviewPageRow
		if err = rows.Scan(&row.ID, &row.PublicID, &row.AuthorID, &row.AuthorName, &row.AuthorAvatar,
			&row.OverallScore, &row.Message, &row.CreatedAt, &row.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode ratings")
			return
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load ratings")
		return
	}
	rows.Close()
	hasMore := len(pageRows) > page.Limit
	if hasMore {
		pageRows = pageRows[:page.Limit]
	}
	items := make([]ratingItemResponse, 0, len(pageRows))
	ids := make([]int64, 0, len(pageRows))
	for _, row := range pageRows {
		ids = append(ids, row.ID)
		items = append(items, row.response())
	}
	if err = s.loadRatingScores(r.Context(), items, ids); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load rating dimensions")
		return
	}
	ossConfig := s.ossConfigFromSettings(r.Context())
	avatarURLs := make([]string, len(items))
	for index := range items {
		avatarURLs[index] = items[index].AuthorAvatar
	}
	avatarURLs, err = s.resolveStoredOSSImageURLsWithConfig(r.Context(), ossConfig, avatarURLs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve rating author avatar")
		return
	}
	for index := range items {
		items[index].AuthorAvatar = avatarURLs[index]
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		nextCursor = ratingReviewNextCursor(page, pageRows[len(pageRows)-1])
	}
	// The bounded response exposes "hasMore" and "nextCursor" instead of a synchronous exact count.
	writeJSON(w, http.StatusOK, ratingListResponse{
		Items: items, Limit: page.Limit, HasMore: hasMore, NextCursor: nextCursor,
	})
}

func (s *Server) resolveRateableTarget(ctx context.Context, rawType, rawPublicID string) (rateableTarget, error) {
	return resolveRateableTargetWithQueryer(ctx, s.db, rawType, rawPublicID)
}

func resolveRateableTargetWithQueryer(ctx context.Context, query revisionQuery, rawType, rawPublicID string) (rateableTarget, error) {
	targetType := normalizeRatingTargetType(rawType)
	publicID := strings.ToLower(strings.TrimSpace(rawPublicID))
	if targetType == "" || !validCatalogPublicID(publicID) {
		return rateableTarget{}, pgx.ErrNoRows
	}
	var target rateableTarget
	target.Type = targetType
	target.PublicID = publicID
	if err := query.QueryRow(ctx, `select id,internal_id from public_routes where entity_type=$1 and public_id=$2`,
		targetType, publicID).Scan(&target.RouteID, &target.InternalID); err != nil {
		return rateableTarget{}, err
	}
	var status string
	var err error
	switch targetType {
	case "mod":
		err = query.QueryRow(ctx, `select review_status from mods where id=$1`, target.InternalID).Scan(&status)
	case "modpack":
		err = query.QueryRow(ctx, `select review_status from modpacks where id=$1`, target.InternalID).Scan(&status)
	case "minecraft_server":
		err = query.QueryRow(ctx, `select review_status from minecraft_servers where id=$1`, target.InternalID).Scan(&status)
	default:
		err = query.QueryRow(ctx, `select review_status from simple_projects where id=$1 and project_type=$2`,
			target.InternalID, targetType).Scan(&status)
	}
	if err != nil {
		return rateableTarget{}, err
	}
	if status != "approved" {
		return rateableTarget{}, pgx.ErrNoRows
	}
	return target, nil
}

func (s *Server) ensurePopularityStats(ctx context.Context, routeID int64) error {
	result, err := s.db.Exec(ctx, `insert into content_popularity_stats(object_route_id) values($1)
		on conflict(object_route_id) do nothing`, routeID)
	if err != nil || result.RowsAffected() == 0 {
		return err
	}
	_, err = s.db.Exec(ctx, `select enqueue_content_stats_refresh($1,true,true)`, routeID)
	return err
}

func (s *Server) loadRatingSummary(ctx context.Context, target rateableTarget, viewerID int64) (ratingSummaryResponse, error) {
	response := ratingSummaryResponse{TargetType: target.Type, TargetID: target.PublicID}
	var rawAverages []byte
	err := s.db.QueryRow(ctx, `select view_count,download_count,favorite_count,comment_count,rating_count,
		rating_average,dimension_averages,heat_score,long_term_score,trend_score,effective_view_score,
		promotion_score,quality_modifier,new_project_boost
		from content_popularity_stats where object_route_id=$1`, target.RouteID).
		Scan(&response.Engagement.Views, &response.Engagement.Downloads, &response.Engagement.Favorites,
			&response.Engagement.Comments, &response.RatingCount, &response.OverallAverage, &rawAverages, &response.HeatScore,
			&response.HeatComponents.LongTerm, &response.HeatComponents.Trend, &response.HeatComponents.EffectiveView,
			&response.HeatComponents.Promotion, &response.HeatComponents.Quality, &response.HeatComponents.NewProject)
	if err != nil {
		return response, err
	}
	averages := make(map[string]float64)
	if len(rawAverages) > 0 {
		if err = json.Unmarshal(rawAverages, &averages); err != nil {
			return response, err
		}
	}
	response.Dimensions = make([]ratingDimensionSummary, 0, len(ratingDimensions[target.Type]))
	for _, dimension := range ratingDimensions[target.Type] {
		response.Dimensions = append(response.Dimensions, ratingDimensionSummary{
			Code: dimension.Code, Average: averages[dimension.Code], Count: response.RatingCount,
		})
	}
	if viewerID > 0 {
		response.MyRating, err = s.loadOwnRating(ctx, target.RouteID, viewerID)
		if errors.Is(err, pgx.ErrNoRows) {
			response.MyRating, err = nil, nil
		}
	}
	return response, err
}

func (s *Server) loadOwnRating(ctx context.Context, routeID, userID int64) (*ratingItemResponse, error) {
	var item ratingItemResponse
	err := s.db.QueryRow(ctx, `select id,public_id,overall_score,message,created_at,updated_at from content_ratings
		where object_route_id=$1 and author_id=$2`, routeID, userID).
		Scan(&item.ID, &item.PublicID, &item.OverallScore, &item.Message, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	items := []ratingItemResponse{item}
	if err = s.loadRatingScores(ctx, items, []int64{item.ID}); err != nil {
		return nil, err
	}
	return &items[0], nil
}

func (s *Server) loadRatingScores(ctx context.Context, items []ratingItemResponse, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	index := make(map[int64]int, len(items))
	for itemIndex := range items {
		items[itemIndex].Scores = make(map[string]int)
		index[items[itemIndex].ID] = itemIndex
	}
	rows, err := s.db.Query(ctx, `select rating_id,dimension_code,score from content_rating_scores
		where rating_id=any($1::bigint[]) order by rating_id,dimension_code`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ratingID int64
		var code string
		var score int
		if err = rows.Scan(&ratingID, &code, &score); err != nil {
			return err
		}
		if itemIndex, ok := index[ratingID]; ok {
			items[itemIndex].Scores[code] = score
		}
	}
	return rows.Err()
}

func validateRatingRequest(targetType string, request ratingUpsertRequest) error {
	dimensions, ok := ratingDimensions[targetType]
	if !ok {
		return errors.New("rating target type is invalid")
	}
	if request.OverallScore < 1 || request.OverallScore > 5 {
		return errors.New("overall rating must be between 1 and 5")
	}
	if utf8.RuneCountInString(request.Message) > ratingMessageLimit {
		return errors.New("rating message is too long")
	}
	if len(request.Scores) != len(dimensions) {
		return errors.New("every rating dimension must be scored")
	}
	expected := make(map[string]struct{}, len(dimensions))
	for _, dimension := range dimensions {
		expected[dimension.Code] = struct{}{}
		value, exists := request.Scores[dimension.Code]
		if !exists || value < 1 || value > 5 {
			return errors.New("rating dimension scores must be between 1 and 5")
		}
	}
	for code := range request.Scores {
		if _, exists := expected[code]; !exists {
			return errors.New("rating contains an unsupported dimension")
		}
	}
	return nil
}
