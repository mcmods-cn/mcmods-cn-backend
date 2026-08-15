package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type userStatisticsRange struct {
	Code      string
	StartDate *time.Time
}

type userStatisticsActivity struct {
	ActionCount          int64            `json:"actionCount"`
	ViewCount            int64            `json:"viewCount"`
	EditCount            int64            `json:"editCount"`
	CreateCount          int64            `json:"createCount"`
	DeleteCount          int64            `json:"deleteCount"`
	MarkdownAddedBytes   int64            `json:"markdownAddedBytes"`
	MarkdownDeletedBytes int64            `json:"markdownDeletedBytes"`
	MarkdownChangedBytes int64            `json:"markdownChangedBytes"`
	MarkdownNetBytes     int64            `json:"markdownNetBytes"`
	ActiveDays           int64            `json:"activeDays"`
	ActionCounts         map[string]int64 `json:"actionCounts"`
	FirstActivityAt      *time.Time       `json:"firstActivityAt,omitempty"`
	LastActivityAt       *time.Time       `json:"lastActivityAt,omitempty"`
	LastEditAt           *time.Time       `json:"lastEditAt,omitempty"`
	LastCommentAt        *time.Time       `json:"lastCommentAt,omitempty"`
}

type userContentCreationStatistics struct {
	ContentType string `json:"contentType"`
	Total       int64  `json:"total"`
	Existing    int64  `json:"existing"`
	Approved    int64  `json:"approved"`
	Pending     int64  `json:"pending"`
	Rejected    int64  `json:"rejected"`
}

type userEditReviewStatistics struct {
	Total             int64      `json:"total"`
	Approved          int64      `json:"approved"`
	Pending           int64      `json:"pending"`
	Rejected          int64      `json:"rejected"`
	Other             int64      `json:"other"`
	EditedObjectCount int64      `json:"editedObjectCount"`
	RevisionCount     int64      `json:"revisionCount"`
	LastEditAt        *time.Time `json:"lastEditAt,omitempty"`
}

type userCommunityStatistics struct {
	Comments                int64      `json:"comments"`
	Replies                 int64      `json:"replies"`
	AcceptedAnswers         int64      `json:"acceptedAnswers"`
	LikesGiven              int64      `json:"likesGiven"`
	LikesReceived           int64      `json:"likesReceived"`
	Favorites               int64      `json:"favorites"`
	Followers               int64      `json:"followers"`
	Following               int64      `json:"following"`
	Reports                 int64      `json:"reports"`
	EffectiveReports        int64      `json:"effectiveReports"`
	HiddenOrDeletedComments int64      `json:"hiddenOrDeletedComments"`
	LastCommentAt           *time.Time `json:"lastCommentAt,omitempty"`
}

type userStatisticsResponse struct {
	UserID              string                          `json:"userId"`
	Range               string                          `json:"range"`
	Full                bool                            `json:"full"`
	RegisteredDays      int64                           `json:"registeredDays"`
	LastLoginAt         *time.Time                      `json:"lastLoginAt,omitempty"`
	TotalActiveDays     int64                           `json:"totalActiveDays"`
	Recent7ActiveDays   int64                           `json:"recent7ActiveDays"`
	Recent30ActiveDays  int64                           `json:"recent30ActiveDays"`
	CurrentActiveStreak int                             `json:"currentActiveStreak"`
	LongestActiveStreak int                             `json:"longestActiveStreak"`
	Cumulative          *userStatisticsActivity         `json:"cumulative,omitempty"`
	SelectedRange       *userStatisticsActivity         `json:"selectedRange,omitempty"`
	ContentCreation     []userContentCreationStatistics `json:"contentCreation"`
	EditReviews         *userEditReviewStatistics       `json:"editReviews,omitempty"`
	Community           userCommunityStatistics         `json:"community"`
	Level               userLevelSummary                `json:"level"`
	ExperienceSources   map[string]int64                `json:"experienceSources,omitempty"`
}

func parseUserStatisticsRange(value string, now time.Time) (userStatisticsRange, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = "all"
	}
	days := 0
	switch value {
	case "all":
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	case "1y":
		days = 365
	default:
		return userStatisticsRange{}, errors.New("statistics range is not allowed")
	}
	result := userStatisticsRange{Code: value}
	if days > 0 {
		start := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -(days - 1))
		result.StartDate = &start
	}
	return result, nil
}

func activeStreaks(dates []time.Time, now time.Time) (current int, longest int) {
	if len(dates) == 0 {
		return 0, 0
	}
	unique := make(map[int64]time.Time, len(dates))
	for _, value := range dates {
		day := time.Date(value.UTC().Year(), value.UTC().Month(), value.UTC().Day(), 0, 0, 0, 0, time.UTC)
		unique[day.Unix()] = day
	}
	dates = dates[:0]
	for _, value := range unique {
		dates = append(dates, value)
	}
	sort.Slice(dates, func(first, second int) bool { return dates[first].Before(dates[second]) })
	run := 0
	for index, value := range dates {
		if index == 0 || value.Sub(dates[index-1]) != 24*time.Hour {
			run = 1
		} else {
			run++
		}
		if run > longest {
			longest = run
		}
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	last := dates[len(dates)-1]
	if last.Equal(today) || last.Equal(today.AddDate(0, 0, -1)) {
		current = 1
		for index := len(dates) - 1; index > 0 && dates[index].Sub(dates[index-1]) == 24*time.Hour; index-- {
			current++
		}
	}
	return current, longest
}

func (s *Server) myUserStatistics(w http.ResponseWriter, r *http.Request) {
	s.writeUserStatistics(w, r, currentClaims(r).Subject, currentClaims(r).PublicSubject, true)
}

func (s *Server) publicUserStatistics(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	full := claims.Subject == identity.InternalID || claimsAllow(claims, "user.read") || claimsAllow(claims, "admin.*")
	s.writeUserStatistics(w, r, identity.InternalID, identity.PublicID, full)
}

func (s *Server) writeUserStatistics(w http.ResponseWriter, r *http.Request, userID int64, publicID string, full bool) {
	statisticsRange, err := parseUserStatisticsRange(r.URL.Query().Get("range"), time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var status string
	var createdAt time.Time
	var lastLoginAt *time.Time
	if err = s.db.QueryRow(r.Context(), `select status,created_at,last_login_at from users where id=$1`, userID).Scan(&status, &createdAt, &lastLoginAt); err == pgx.ErrNoRows || status == "deleted" {
		writeError(w, http.StatusNotFound, "user not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user statistics")
		return
	}
	response := userStatisticsResponse{
		UserID: publicID, Range: statisticsRange.Code, Full: full,
		RegisteredDays:  max(int64(1), int64(time.Since(createdAt).Hours()/24)+1),
		ContentCreation: []userContentCreationStatistics{}, ExperienceSources: map[string]int64{},
	}
	response.Level, err = s.loadUserLevelSummary(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user level")
		return
	}
	if err = s.loadUserPublicStatistics(r, userID, full, &response); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load public user statistics")
		return
	}
	if !full {
		response.RegisteredDays = 0
		response.LastLoginAt = nil
		response.ExperienceSources = nil
		response.Community.HiddenOrDeletedComments = 0
		response.Community.LastCommentAt = nil
		writeJSON(w, http.StatusOK, response)
		return
	}
	response.LastLoginAt = lastLoginAt
	if err = s.loadUserPrivateStatistics(r, userID, statisticsRange, &response); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load private user statistics")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadUserPublicStatistics(r *http.Request, userID int64, full bool, response *userStatisticsResponse) error {
	visibility := ""
	if !full {
		visibility = " and current_exists and review_status='approved'"
	}
	rows, err := s.db.Query(r.Context(), `select content_type,count(*),count(*) filter(where current_exists),
		count(*) filter(where review_status='approved'),count(*) filter(where review_status='pending'),count(*) filter(where review_status='rejected')
		from user_content_creation_facts where user_id=$1`+visibility+` group by content_type order by content_type`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item userContentCreationStatistics
		if err = rows.Scan(&item.ContentType, &item.Total, &item.Existing, &item.Approved, &item.Pending, &item.Rejected); err != nil {
			rows.Close()
			return err
		}
		response.ContentCreation = append(response.ContentCreation, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return s.db.QueryRow(r.Context(), `select
		count(*) filter(where comment.status='published' and comment.parent_id is null),
		count(*) filter(where comment.status='published' and comment.parent_id is not null),
		count(*) filter(where comment.status in ('hidden','deleted','spam')),
		max(comment.created_at) filter(where comment.status='published'),
		(select count(*) from community_posts post join comments answer on answer.id=post.accepted_comment_id where answer.author_id=$1),
		(select count(*) from comment_reactions where user_id=$1),
		(select count(*) from comment_reactions reaction join comments received on received.id=reaction.comment_id where received.author_id=$1 and reaction.user_id<>$1),
		(select count(*) from favorite_collection_items item join favorite_collections collection on collection.id=item.collection_id where collection.user_id=$1),
		(select count(*) from user_follows where followed_id=$1),
		(select count(*) from user_follows where follower_id=$1)
		from comments comment where comment.author_id=$1`, userID).Scan(
		&response.Community.Comments, &response.Community.Replies, &response.Community.HiddenOrDeletedComments,
		&response.Community.LastCommentAt, &response.Community.AcceptedAnswers, &response.Community.LikesGiven,
		&response.Community.LikesReceived, &response.Community.Favorites, &response.Community.Followers, &response.Community.Following)
}

func (s *Server) loadUserPrivateStatistics(r *http.Request, userID int64, statisticsRange userStatisticsRange, response *userStatisticsResponse) error {
	cumulative, err := s.queryUserActivityStatistics(r, userID, nil)
	if err != nil {
		return err
	}
	selected, err := s.queryUserActivityStatistics(r, userID, statisticsRange.StartDate)
	if err != nil {
		return err
	}
	response.Cumulative = &cumulative
	response.SelectedRange = &selected
	response.TotalActiveDays = cumulative.ActiveDays
	var recent7, recent30 int64
	if err = s.db.QueryRow(r.Context(), `select count(*) filter(where stat_date>=current_date-6),count(*) filter(where stat_date>=current_date-29)
		from user_statistics_daily where user_id=$1 and action_count>0`, userID).Scan(&recent7, &recent30); err != nil {
		return err
	}
	response.Recent7ActiveDays, response.Recent30ActiveDays = recent7, recent30
	rows, err := s.db.Query(r.Context(), `select stat_date from user_statistics_daily where user_id=$1 and action_count>0 order by stat_date`, userID)
	if err != nil {
		return err
	}
	dates := make([]time.Time, 0)
	for rows.Next() {
		var value time.Time
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		dates = append(dates, value)
	}
	rows.Close()
	response.CurrentActiveStreak, response.LongestActiveStreak = activeStreaks(dates, time.Now().UTC())
	editReviews := &userEditReviewStatistics{}
	err = s.db.QueryRow(r.Context(), `select count(*),count(*) filter(where status='approved'),count(*) filter(where status='pending'),
		count(*) filter(where status='rejected'),count(*) filter(where status in ('conflicted','withdrawn')),
		count(distinct (entity_type,entity_id)) filter(where entity_id is not null),count(distinct proposed_revision_id),max(submitted_at)
		from change_requests where submitted_by=$1`, userID).Scan(&editReviews.Total, &editReviews.Approved, &editReviews.Pending,
		&editReviews.Rejected, &editReviews.Other, &editReviews.EditedObjectCount, &editReviews.RevisionCount, &editReviews.LastEditAt)
	if err != nil {
		return err
	}
	response.EditReviews = editReviews
	if err = s.db.QueryRow(r.Context(), `select count(*),count(*) filter(where status='resolved') from comment_reports where reporter_id=$1`, userID).
		Scan(&response.Community.Reports, &response.Community.EffectiveReports); err != nil {
		return err
	}
	rows, err = s.db.Query(r.Context(), `select reason,coalesce(sum(amount_delta),0) from experience_transactions where user_id=$1 group by reason order by reason`, userID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var reason string
		var amount int64
		if err = rows.Scan(&reason, &amount); err != nil {
			rows.Close()
			return err
		}
		response.ExperienceSources[reason] = amount
	}
	rows.Close()
	return rows.Err()
}

func (s *Server) queryUserActivityStatistics(r *http.Request, userID int64, startDate *time.Time) (userStatisticsActivity, error) {
	result := userStatisticsActivity{ActionCounts: map[string]int64{}}
	err := s.db.QueryRow(r.Context(), `select coalesce(sum(action_count),0),coalesce(sum(view_count),0),coalesce(sum(edit_count),0),
		coalesce(sum(create_count),0),coalesce(sum(delete_count),0),coalesce(sum(markdown_added_bytes),0),
		coalesce(sum(markdown_deleted_bytes),0),count(*) filter(where action_count>0),
		min(first_activity_at),max(last_activity_at)
		from user_statistics_daily daily
		where user_id=$1 and ($2::date is null or stat_date>=$2::date)`, userID, startDate).Scan(
		&result.ActionCount, &result.ViewCount, &result.EditCount, &result.CreateCount, &result.DeleteCount,
		&result.MarkdownAddedBytes, &result.MarkdownDeletedBytes, &result.ActiveDays,
		&result.FirstActivityAt, &result.LastActivityAt)
	if err != nil {
		return result, err
	}
	rows, err := s.db.Query(r.Context(), `select action.key,sum(action.value::bigint)
		from user_statistics_daily daily cross join lateral jsonb_each_text(daily.action_counts) action
		where user_id=$1 and ($2::date is null or stat_date>=$2::date) group by action.key order by action.key`, userID, startDate)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var key string
		var value int64
		if err = rows.Scan(&key, &value); err != nil {
			rows.Close()
			return result, err
		}
		result.ActionCounts[key] = value
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	result.MarkdownChangedBytes = result.MarkdownAddedBytes + result.MarkdownDeletedBytes
	result.MarkdownNetBytes = result.MarkdownAddedBytes - result.MarkdownDeletedBytes
	if startDate == nil {
		_ = s.db.QueryRow(r.Context(), `select last_edit_at,last_comment_at from user_statistics_totals where user_id=$1`, userID).
			Scan(&result.LastEditAt, &result.LastCommentAt)
	}
	return result, nil
}
