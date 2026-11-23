package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestUserStatisticsUseUTCActivityDaysIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	ctx := t.Context()
	db := newTEST018IsolatedDatabase(t, ctx)

	now := time.Now().UTC()
	today := now.Truncate(24 * time.Hour)
	offsetZone := "Pacific/Kiritimati"
	if now.Hour() < 12 {
		offsetZone = "Etc/GMT+12"
	}
	connection := db.Config()
	connection.ConnConfig.RuntimeParams["timezone"] = offsetZone
	localDB, err := pgxpool.NewWithConfig(ctx, connection)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(localDB.Close)

	var sessionDay time.Time
	if err = localDB.QueryRow(ctx, "select current_date").Scan(&sessionDay); err != nil {
		t.Fatal(err)
	}
	if sessionDay.Equal(today) {
		t.Fatalf("fixture timezone %s does not cross the UTC day boundary", offsetZone)
	}

	cfg := config.Load()
	cfg.JWTSecret = "test-statistics-calendar-signing-key"
	cfg.Redis.Enabled, cfg.AntiAbuse.Enabled = false, false
	userID, publicID, token := createTEST044User(t, ctx, db, cfg, "statistics-calendar", offsetZone)
	if _, err = db.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at)
		select $1,$2,$3,$4::timestamptz-days*interval '24 hours'
		from unnest($5::int[]) as offsets(days)`, userID, activityActionIDs["view"], activityObjectTypeIDs["user"],
		today, []int{0, 6, 7, 29, 30}); err != nil {
		t.Fatal(err)
	}

	server := NewServer(ctx, cfg, localDB, nil, nil, nil, nil)
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			t.Error(err)
		}
		server.cache.Close()
	})
	for _, tc := range []struct {
		query        string
		selectedDays int64
	}{{"all", 5}, {"7d", 2}, {"30d", 4}} {
		t.Run(tc.query, func(t *testing.T) {
			response := performTEST044Request(ctx, server.ServeHTTP, token, http.MethodGet,
				"/api/v1/users/me/statistics?range="+tc.query, "", nil)
			if response.Code != http.StatusOK {
				t.Fatalf("statistics status=%d body=%s", response.Code, response.Body)
			}
			got := decodeTEST022Data[userStatisticsResponse](t, response.Body.Bytes())
			if !got.Full || got.UserID != publicID || got.Range != tc.query || got.TotalActiveDays != 5 ||
				got.Recent7ActiveDays != 2 || got.Recent30ActiveDays != 4 ||
				got.CurrentActiveStreak != 1 || got.LongestActiveStreak != 2 {
				t.Fatalf("UTC statistics: full=%v id=%q range=%q days=%d recent=%d/%d streak=%d/%d",
					got.Full, got.UserID, got.Range, got.TotalActiveDays, got.Recent7ActiveDays,
					got.Recent30ActiveDays, got.CurrentActiveStreak, got.LongestActiveStreak)
			}
			if got.Cumulative == nil || got.Cumulative.ActionCount != 5 || got.SelectedRange == nil ||
				got.SelectedRange.ActiveDays != tc.selectedDays || got.SelectedRange.ActionCounts["view"] != tc.selectedDays {
				t.Fatalf("cumulative=%+v selected=%+v", got.Cumulative, got.SelectedRange)
			}
		})
	}
}
