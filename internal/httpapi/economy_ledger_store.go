package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var errAmbiguousTransferRecipient = errors.New("ambiguous transfer recipient")

// Public identities and human lookup values never denote an internal primary key.
// A legacy lookup that matches different accounts must be made explicit by the caller.
func resolveTransferRecipient(ctx context.Context, tx pgx.Tx, value string) (int64, error) {
	rows, err := tx.Query(ctx, `select id from users where status='active' and
  (lower(username)=lower($1) or lower(email)=lower($1) or public_id=lower($1)) limit 2`, value)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var userID int64
	count := 0
	for rows.Next() {
		if err = rows.Scan(&userID); err != nil {
			return 0, err
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, pgx.ErrNoRows
	}
	if count > 1 {
		return 0, errAmbiguousTransferRecipient
	}
	return userID, nil
}

func lockCurrencyBalancesTx(ctx context.Context, tx pgx.Tx, currencyID, firstUserID, secondUserID int64) error {
	if firstUserID > secondUserID {
		firstUserID, secondUserID = secondUserID, firstUserID
	}
	if _, err := lockCurrencyBalanceTx(ctx, tx, firstUserID, currencyID); err != nil {
		return err
	}
	_, err := lockCurrencyBalanceTx(ctx, tx, secondUserID, currencyID)
	return err
}

func lockCurrencyBalanceTx(ctx context.Context, tx pgx.Tx, userID, currencyID int64) (int64, error) {
	if _, err := tx.Exec(ctx, `insert into user_currency_balances(user_id,currency_id,balance)
		values($1,$2,0) on conflict do nothing`, userID, currencyID); err != nil {
		return 0, err
	}
	var balance int64
	err := tx.QueryRow(ctx, `select balance from user_currency_balances
		where user_id=$1 and currency_id=$2 for update`, userID, currencyID).Scan(&balance)
	return balance, err
}

func changeCurrencyBalanceTx(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	currencyCode string,
	delta int64,
	transactionType string,
	counterparty *int64,
	referenceType, referenceKey string,
	metadata map[string]any,
) (int64, error) {
	var currencyID int64
	if err := tx.QueryRow(ctx, `select id from currencies where code=$1 and status='active'`, currencyCode).
		Scan(&currencyID); err != nil {
		return 0, err
	}
	return changeCurrencyBalanceByIDTx(
		ctx, tx, userID, currencyID, delta, transactionType, counterparty, referenceType, referenceKey, metadata,
	)
}

func changeCurrencyBalanceByIDTx(
	ctx context.Context,
	tx pgx.Tx,
	userID, currencyID, delta int64,
	transactionType string,
	counterparty *int64,
	referenceType, referenceKey string,
	metadata map[string]any,
) (int64, error) {
	current, err := lockCurrencyBalanceTx(ctx, tx, userID, currencyID)
	if err != nil {
		return 0, err
	}
	return applyLockedCurrencyBalanceChangeTx(ctx, tx, userID, currencyID, current, delta,
		transactionType, counterparty, referenceType, referenceKey, metadata)
}

func applyLockedCurrencyBalanceChangeTx(
	ctx context.Context,
	tx pgx.Tx,
	userID, currencyID, current, delta int64,
	transactionType string,
	counterparty *int64,
	referenceType, referenceKey string,
	metadata map[string]any,
) (int64, error) {
	next, err := checkedCurrencyBalance(current, delta)
	if err != nil {
		return current, err
	}
	if _, err := tx.Exec(ctx, `update user_currency_balances set balance=$3,updated_at=now()
		where user_id=$1 and currency_id=$2`, userID, currencyID, next); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("encode currency transaction metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `insert into currency_transactions(
		user_id,currency_id,amount_delta,balance_after,transaction_type,counterparty_user_id,
		reference_type,reference_key,metadata
	) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		userID, currencyID, delta, next, transactionType, counterparty, referenceType, referenceKey, raw); err != nil {
		return 0, err
	}
	return next, nil
}
