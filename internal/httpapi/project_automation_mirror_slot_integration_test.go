package httpapi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectAutomationMirrorSlotsAreGlobalAcrossWorkerPoolsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate global project mirror slots")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connectionString := config.Load().DB.ConnString()
	firstConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	firstConfig.MaxConns = int32(projectAutomationMirrorGlobalConcurrency)
	firstConfig.MinConns = int32(projectAutomationMirrorGlobalConcurrency)
	first, err := pgxpool.NewWithConfig(ctx, firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	secondConfig, err := pgxpool.ParseConfig(connectionString)
	if err != nil {
		t.Fatal(err)
	}
	secondConfig.MaxConns = 1
	secondConfig.MinConns = 1
	second, err := pgxpool.NewWithConfig(ctx, secondConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	slots := make([]*projectAutomationMirrorSlot, 0, projectAutomationMirrorGlobalConcurrency)
	for index := 0; index < projectAutomationMirrorGlobalConcurrency; index++ {
		slot, acquireErr := acquireProjectAutomationMirrorSlot(ctx, first)
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		slots = append(slots, slot)
	}
	defer func() {
		for _, slot := range slots {
			slot.Release()
		}
	}()

	blockedCtx, blockedCancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer blockedCancel()
	if slot, acquireErr := acquireProjectAutomationMirrorSlot(blockedCtx, second); !errors.Is(acquireErr, context.DeadlineExceeded) {
		if slot != nil {
			slot.Release()
		}
		t.Fatalf("fifth global mirror slot err=%v", acquireErr)
	}

	slots[0].Release()
	replacementCtx, replacementCancel := context.WithTimeout(ctx, time.Second)
	defer replacementCancel()
	replacement, err := acquireProjectAutomationMirrorSlot(replacementCtx, second)
	if err != nil {
		t.Fatal(err)
	}
	replacement.Release()
}
