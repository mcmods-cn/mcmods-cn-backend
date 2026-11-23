package httpapi

import (
	"context"
	"sync"
	"testing"

	"mcmods-cn-backend/internal/serverprobe"
)

func TestTEST015SchedulerIgnoresSupersededObservationsIntegration(t *testing.T) {
	for _, name := range []string{"moderated", "claimed_again"} {
		t.Run(name, func(t *testing.T) {
			f := newTEST015ServerFixture(t)
			id := f.seedServer(t, "owned-late-probe.invalid:25565", "approved")
			started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			releaseProbe := sync.OnceFunc(func() { close(release) })
			go func() {
				defer close(done)
				probeDueMinecraftServersWithProbe(f.ctx, f.db, func(ctx context.Context, address string) (serverprobe.Result, error) {
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
						return serverprobe.Result{}, ctx.Err()
					}
					result := test015ProbeResult(address)
					result.PlayersOnline = 3
					return result, nil
				})
			}()
			defer func() { releaseProbe(); <-done }()
			select {
			case <-started:
			case <-done:
				t.Fatal("scheduler finished without probing the owned test server")
			case <-f.ctx.Done():
				t.Fatal(f.ctx.Err())
			}
			wantSamples, wantPlayers := 0, 0
			switch name {
			case "moderated":
				if _, err := f.db.Exec(f.ctx, `update minecraft_servers set review_status='rejected' where id=$1`, id); err != nil {
					t.Fatal(err)
				}
			case "claimed_again":
				if _, err := f.db.Exec(f.ctx, `update minecraft_servers set next_probe_at=now()-interval '1 second' where id=$1`, id); err != nil {
					t.Fatal(err)
				}
				probeDueMinecraftServersWithProbe(f.ctx, f.db, func(_ context.Context, address string) (serverprobe.Result, error) {
					result := test015ProbeResult(address)
					result.PlayersOnline = 19
					return result, nil
				})
				wantSamples, wantPlayers = 1, 19
			}
			releaseProbe()
			<-done
			var samples, players int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from minecraft_server_status_samples where server_id=$1),
				last_players_online from minecraft_servers where id=$1`, id).Scan(&samples, &players); err != nil {
				t.Fatal(err)
			}
			if samples != wantSamples || players != wantPlayers {
				t.Fatalf("samples=%d players=%d; want %d/%d after the old observation returned", samples, players, wantSamples, wantPlayers)
			}
		})
	}
}
