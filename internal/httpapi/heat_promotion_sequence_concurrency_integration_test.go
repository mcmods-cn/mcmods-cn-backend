package httpapi

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHeatPromotionSequenceIsUniqueAcrossConcurrentTransactionsIntegration(t *testing.T) {
	requireOCT02DatabaseIntegration(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	base, err := pgxpool.New(ctx, os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer base.Close()
	schema := fmt.Sprintf("oct02_heat_counter_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, `create schema `+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		// Only this successfully created, random schema is disposable. The parent
		// database and all preexisting objects remain outside this cleanup target.
		if _, cleanupErr := base.Exec(cleanupContext, `drop schema `+identifier+` cascade`); cleanupErr != nil {
			t.Errorf("remove owned heat-counter fixture: %v", cleanupErr)
		}
	}()
	if _, err = base.Exec(ctx, `create table `+identifier+`.content_heat_promotion_counters(
		object_route_id bigint primary key,last_sequence_no bigint not null check(last_sequence_no>0))`); err != nil {
		t.Fatal(err)
	}
	poolConfig := base.Config()
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	const workers, rounds = 8, 8
	start := make(chan struct{})
	ready := make(chan struct{}, workers)
	values := make(chan int64, workers*rounds)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < rounds; round++ {
				tx, beginErr := pool.Begin(ctx)
				if beginErr != nil {
					errors <- beginErr
					if round == 0 {
						ready <- struct{}{}
					}
					return
				}
				if round == 0 {
					ready <- struct{}{}
					select {
					case <-start:
					case <-ctx.Done():
						_ = tx.Rollback(context.Background())
						errors <- ctx.Err()
						return
					}
				}
				sequence, sequenceErr := nextHeatPromotionSequenceTx(ctx, tx, 42)
				if sequenceErr != nil {
					_ = tx.Rollback(context.Background())
					errors <- sequenceErr
					return
				}
				if commitErr := tx.Commit(ctx); commitErr != nil {
					errors <- commitErr
					return
				}
				values <- sequence
			}
		}()
	}
	for worker := 0; worker < workers; worker++ {
		<-ready
	}
	close(start)
	group.Wait()
	close(errors)
	for sequenceErr := range errors {
		t.Fatal(sequenceErr)
	}
	close(values)
	actual := make([]int64, 0, workers*rounds)
	for value := range values {
		actual = append(actual, value)
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
	if len(actual) != workers*rounds {
		t.Fatalf("committed sequences=%d, want %d", len(actual), workers*rounds)
	}
	for index, value := range actual {
		if value != int64(index+1) {
			t.Fatalf("sequence index=%d value=%d, want %d", index, value, index+1)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := nextHeatPromotionSequenceTx(ctx, tx, 42)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var persisted int64
	if err = pool.QueryRow(ctx, `select last_sequence_no from content_heat_promotion_counters where object_route_id=42`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if sequence != workers*rounds+1 || persisted != workers*rounds {
		t.Fatalf("rolled-back sequence=%d persisted=%d, want 65/64", sequence, persisted)
	}
}
