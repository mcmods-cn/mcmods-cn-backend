package httpapi

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/serverprobe"
)

const minecraftServerProbeInterval = 5 * time.Minute

// StartMinecraftServerProbeScheduler records one status sample every five
// minutes for approved servers. Work is claimed with SKIP LOCKED so multiple
// application instances do not probe the same server in the same interval.
func StartMinecraftServerProbeScheduler(ctx context.Context, db *pgxpool.Pool) {
	go func() {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		ticker := time.NewTicker(minecraftServerProbeInterval)
		defer ticker.Stop()
		cycles := 0
		for {
			probeDueMinecraftServers(ctx, db)
			cycles++
			if cycles%12 == 0 {
				cleanupMinecraftServerSamples(ctx, db)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func probeDueMinecraftServers(ctx context.Context, db *pgxpool.Pool) {
	tx, err := db.Begin(ctx)
	if err != nil {
		log.Printf("begin Minecraft server probe claims: %v", err)
		return
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `with due as (
		select id,address from minecraft_servers
		where review_status='approved' and next_probe_at<=now()
		order by next_probe_at,id
		for update skip locked limit 1000
	)
	update minecraft_servers server
	set next_probe_at=now()+interval '5 minutes'
	from due where server.id=due.id
	returning server.id,due.address`)
	if err != nil {
		log.Printf("claim Minecraft server probes: %v", err)
		return
	}
	type target struct {
		id      int64
		address string
	}
	targets := make([]target, 0)
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.id, &item.address); err != nil {
			rows.Close()
			log.Printf("read Minecraft server probe claims: %v", err)
			return
		}
		targets = append(targets, item)
	}
	if err = finishRows(rows); err != nil {
		log.Printf("read Minecraft server probe claims: %v", err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		log.Printf("commit Minecraft server probe claims: %v", err)
		return
	}

	semaphore := make(chan struct{}, 32)
	var waitGroup sync.WaitGroup
	for _, item := range targets {
		if ctx.Err() != nil {
			break
		}
		waitGroup.Add(1)
		go func(item target) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-semaphore }()
			result, probeErr := serverprobe.Probe(ctx, item.address)
			if persistErr := persistMinecraftServerProbe(ctx, db, item.id, result, probeErr); persistErr != nil {
				log.Printf("persist Minecraft server %d probe: %v", item.id, persistErr)
			}
		}(item)
	}
	waitGroup.Wait()
}

func persistMinecraftServerProbe(ctx context.Context, db *pgxpool.Pool, serverID int64, result serverprobe.Result, probeErr error) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if probeErr != nil {
		message := probeErr.Error()
		if len(message) > 2000 {
			message = message[:2000]
		}
		if _, err = tx.Exec(ctx, `insert into minecraft_server_status_samples(
			server_id,online,error
		) values($1,false,$2)`, serverID, message); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update minecraft_servers set last_online=false,
			last_latency_ms=null,last_checked_at=now(),last_error=$2,updated_at=now()
			where id=$1`, serverID, message); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `insert into minecraft_server_status_samples(
		server_id,online,latency_ms,players_online,players_max,minecraft_version,protocol
	) values($1,true,$2,$3,$4,$5,$6)`, serverID, result.LatencyMS,
		result.PlayersOnline, result.PlayersMax, result.MinecraftVersion, result.Protocol); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update minecraft_servers set
		last_online=true,last_latency_ms=$2,last_players_online=$3,last_players_max=$4,
		last_motd=$5,last_minecraft_version=$6,last_protocol=$7,last_checked_at=now(),
		last_error='',icon_data_uri=case when $8<>'' then $8 else icon_data_uri end,
		modded=modded or $9,loader=case when $10<>'' then $10 else loader end,
		mod_list_complete=mod_list_complete or $11,updated_at=now()
		where id=$1`, serverID, result.LatencyMS, result.PlayersOnline, result.PlayersMax,
		result.MOTD, result.MinecraftVersion, result.Protocol, result.IconDataURI,
		result.Modded, result.Loader, result.ModListComplete); err != nil {
		return err
	}
	if err = reconcileMinecraftServerProbeMods(ctx, tx, serverID, result.Mods, result.ModListComplete); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func cleanupMinecraftServerSamples(ctx context.Context, db *pgxpool.Pool) {
	settings := loadServerCatalogSettings(ctx, db)
	cutoff := time.Now().Add(-time.Duration(settings.HistoryDays) * 24 * time.Hour)
	if _, err := db.Exec(ctx, `delete from minecraft_server_status_samples
		where checked_at<$1`, cutoff); err != nil {
		log.Printf("cleanup Minecraft server samples: %v", err)
	}
}
