package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/progression"
)

var publicCardStatisticOptionKeys = []string{
	"comment_count", "reply_count", "received_likes", "mod_count", "plugin_count", "map_count",
	"server_count", "tutorial_count", "edit_count", "edit_bytes", "active_days", "accepted_answers",
	"favorite_count", "follower_count",
}

var publicCardStatisticKeys = stringSet(publicCardStatisticOptionKeys...)

type userLevelSummary struct {
	Level                 int     `json:"level"`
	Experience            int64   `json:"experience"`
	ExperienceInLevel     int64   `json:"experienceInLevel"`
	ExperienceToNextLevel int64   `json:"experienceToNextLevel"`
	LifetimeExperience    int64   `json:"lifetimeExperience"`
	ProgressPercent       float64 `json:"progressPercent"`
}

type publicCardStatistic struct {
	Key   string `json:"key"`
	Value int64  `json:"value"`
}

type publicUserCard struct {
	ID           string                 `json:"id"`
	Username     string                 `json:"username"`
	AvatarURL    string                 `json:"avatarUrl"`
	OnlineStatus publicOnlineStatus     `json:"onlineStatus"`
	Level        userLevelSummary       `json:"level"`
	Statistics   []*publicCardStatistic `json:"statistics"`
}

type cachedPublicUserCard struct {
	Card publicUserCard `json:"card"`
}

func publicUserCardCacheKey(userID int64) string {
	return fmt.Sprintf("user-card:public:%d", userID)
}

func normalizePublicCardSlots(values []string) ([]string, error) {
	if len(values) != 6 {
		return nil, errors.New("exactly six public card statistic slots are required")
	}
	result := make([]string, 6)
	seen := map[string]bool{}
	for index, value := range values {
		if value == "" {
			continue
		}
		if !publicCardStatisticKeys[value] {
			return nil, errors.New("public card statistic is not allowed")
		}
		if seen[value] {
			return nil, errors.New("public card statistics cannot be duplicated")
		}
		seen[value] = true
		result[index] = value
	}
	return result, nil
}

func (s *Server) userCard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var userID int64
	var status string
	var showOnline bool
	err := s.db.QueryRow(r.Context(), `select account.id,account.status,account.show_online_status
		from public_routes route join users account on account.id=route.internal_id
		where route.public_id=$1 and route.entity_type='user'`, publicID).Scan(&userID, &status, &showOnline)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "用户 ID 不正确")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user card")
		return
	}
	if status == "deleted" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	loader := func(ctx context.Context) ([]byte, error) {
		cached, loadErr := s.loadPublicUserCard(ctx, userID, publicID)
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(cached)
	}
	var raw []byte
	if s.cache.Config().UserCardCacheEnabled {
		raw, err = s.cache.GetOrLoadTTL(r.Context(), publicUserCardCacheKey(userID), s.cache.Config().UserCardTTL, loader)
	} else {
		raw, err = loader(r.Context())
	}
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user card")
		return
	}
	var cached cachedPublicUserCard
	if err = json.Unmarshal(raw, &cached); err != nil {
		s.cache.Delete(r.Context(), publicUserCardCacheKey(userID))
		writeError(w, http.StatusServiceUnavailable, "failed to decode user card cache")
		return
	}
	response := cached.Card
	onlineActive := false
	if showOnline {
		onlineActive = s.cache.UsersOnline(r.Context(), []int64{userID}, time.Now(), s.cache.Config().PresenceTTL)[userID]
	}
	response.OnlineStatus = mapPublicOnlineVisibility(showOnline, onlineActive)
	ossCfg := s.ossConfigFromSettings(r.Context())
	response.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, response.AvatarURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate user avatar URL")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadPublicUserCard(ctx context.Context, userID int64, publicID string) (cachedPublicUserCard, error) {
	var cached cachedPublicUserCard
	var slots []string
	cached.Card.ID = publicID
	err := s.db.QueryRow(ctx, `select username,avatar_url,public_card_stat_slots
		from users where id=$1 and status<>'deleted'`, userID).
		Scan(&cached.Card.Username, &cached.Card.AvatarURL, &slots)
	if err != nil {
		return cached, err
	}
	cached.Card.Level, err = s.loadUserLevelSummary(ctx, userID)
	if err != nil {
		return cached, err
	}
	values, err := s.loadPublicCardStatisticValues(ctx, userID)
	if err != nil {
		return cached, err
	}
	cached.Card.Statistics = make([]*publicCardStatistic, 6)
	for index := 0; index < len(cached.Card.Statistics) && index < len(slots); index++ {
		if slots[index] != "" {
			cached.Card.Statistics[index] = &publicCardStatistic{Key: slots[index], Value: values[slots[index]]}
		}
	}
	return cached, nil
}

func (s *Server) loadUserLevelSummary(ctx context.Context, userID int64) (userLevelSummary, error) {
	var result userLevelSummary
	var thresholds []int64
	err := s.db.QueryRow(ctx, `select coalesce(experience.experience,0),
		coalesce((select sum(greatest(amount_delta,0)) from experience_transactions where user_id=account.id),0),
		coalesce((select level_thresholds from level_system_config where singleton),'{}'::bigint[])
		from users account left join user_experience experience on experience.user_id=account.id where account.id=$1`, userID).
		Scan(&result.Experience, &result.LifetimeExperience, &thresholds)
	if err != nil {
		return result, err
	}
	result.Level = progression.LevelForExperience(result.Experience, thresholds)
	levelStart := int64(0)
	if result.Level > 0 && result.Level-1 < len(thresholds) {
		levelStart = thresholds[result.Level-1]
	}
	result.ExperienceInLevel = max(int64(0), result.Experience-levelStart)
	if result.Level < len(thresholds) {
		levelEnd := thresholds[result.Level]
		result.ExperienceToNextLevel = max(int64(0), levelEnd-result.Experience)
		span := levelEnd - levelStart
		if span > 0 {
			result.ProgressPercent = float64(result.ExperienceInLevel) * 100 / float64(span)
		}
	} else {
		result.ProgressPercent = 100
	}
	return result, nil
}

func (s *Server) loadPublicCardStatisticValues(ctx context.Context, userID int64) (map[string]int64, error) {
	keys := publicCardStatisticOptionKeys
	values := make([]int64, len(keys))
	err := s.db.QueryRow(ctx, `select
		(select count(*) from comments where author_id=$1 and status='published'),
		(select count(*) from comments where author_id=$1 and parent_id is not null and status='published'),
		(select count(*) from comment_reactions reaction join comments comment on comment.id=reaction.comment_id
			where comment.author_id=$1 and reaction.user_id<>$1 and reaction.reaction in ('thumbs_up','heart')),
		(select count(*) from user_content_creation_facts where user_id=$1 and content_type='mod' and current_exists and review_status='approved'),
		(select count(*) from user_content_creation_facts where user_id=$1 and content_type='plugin' and current_exists and review_status='approved'),
		(select count(*) from user_content_creation_facts where user_id=$1 and content_type='map' and current_exists and review_status='approved'),
		(select count(*) from user_content_creation_facts where user_id=$1 and content_type='server' and current_exists and review_status='approved'),
		(select count(*) from user_content_creation_facts where user_id=$1 and content_type='tutorial' and current_exists and review_status='approved'),
		coalesce((select edit_count from user_statistics_totals where user_id=$1),0),
		coalesce((select markdown_added_bytes+markdown_deleted_bytes from user_statistics_totals where user_id=$1),0),
		(select count(*) from user_statistics_daily where user_id=$1 and action_count>0),
		(select count(*) from community_posts post join comments comment on comment.id=post.accepted_comment_id where comment.author_id=$1),
		(select count(*) from favorite_collection_items item join favorite_collections collection on collection.id=item.collection_id where collection.user_id=$1),
		(select count(*) from user_follows where followed_id=$1)`, userID).Scan(
		&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6],
		&values[7], &values[8], &values[9], &values[10], &values[11], &values[12], &values[13])
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(keys))
	for index, key := range keys {
		result[key] = values[index]
	}
	return result, nil
}
