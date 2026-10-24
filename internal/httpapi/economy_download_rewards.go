package httpapi

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Preserve the original ownership, step and currency policy within the caller's
// transaction; capability disclosure, activity and rewards share one outcome.
func (s *Server) recordOwnedContentDownloadTx(ctx context.Context, tx pgx.Tx, objectType, objectPublicID string, ownerID, downloaderID int64) error {
	if ownerID <= 0 || ownerID == downloaderID {
		return nil
	}
	if err := lockEconomyConfigurationTx(ctx, tx); err != nil {
		return err
	}
	config, err := loadEconomyConfig(ctx, tx)
	if err != nil {
		return err
	}
	rules := make([]economyDownloadReward, 0)
	for _, rule := range config.DownloadRewards {
		if rule.ObjectType == objectType {
			rules = append(rules, rule)
		}
	}
	var objectRouteID int64
	if err = tx.QueryRow(ctx, `select id from public_routes where entity_type=$1 and public_id=$2`,
		objectType, objectPublicID).Scan(&objectRouteID); err != nil {
		return err
	}
	var downloads int64
	if err = tx.QueryRow(ctx, `insert into content_download_counters(
		object_route_id,owner_id,downloads,last_download_at
	) values($1,$2,1,now())
		on conflict(object_route_id,owner_id) do update
		set downloads=content_download_counters.downloads+1,last_download_at=now()
		where content_download_counters.downloads<$3
		returning downloads`, objectRouteID, ownerID, maxCurrencyBalance).Scan(&downloads); err != nil {
		return err
	}
	for _, rule := range rules {
		var currencyID int64
		if err = tx.QueryRow(ctx, `select id from currencies where code=$1 and status='active'`, rule.Currency).
			Scan(&currencyID); err != nil {
			return err
		}
		var rewardedSteps int64
		if err = tx.QueryRow(ctx, `insert into content_download_reward_counters(
			object_route_id,owner_id,currency_id,rewarded_steps
		) values($1,$2,$3,0)
		on conflict(object_route_id,owner_id,currency_id) do update
		set updated_at=content_download_reward_counters.updated_at
		returning rewarded_steps`, objectRouteID, ownerID, currencyID).Scan(&rewardedSteps); err != nil {
			return err
		}
		targetSteps := downloads / rule.DownloadsPerReward
		if targetSteps <= rewardedSteps {
			continue
		}
		deltaSteps := targetSteps - rewardedSteps
		rewardAmount, amountErr := checkedNonNegativeCurrencyProduct(deltaSteps, rule.Amount)
		if amountErr != nil {
			return amountErr
		}
		if _, err = changeCurrencyBalanceByIDTx(
			ctx, tx, ownerID, currencyID, rewardAmount, "content_download_reward", nil,
			objectType, objectPublicID, map[string]any{"downloads": downloads, "steps": deltaSteps},
		); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update content_download_reward_counters set rewarded_steps=$4,updated_at=now()
			where object_route_id=$1 and owner_id=$2 and currency_id=$3`,
			objectRouteID, ownerID, currencyID, targetSteps); err != nil {
			return err
		}
	}
	return nil
}
