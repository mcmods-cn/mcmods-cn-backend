package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

type adminBalanceAdjustmentRequest struct {
	CurrencyCode string `json:"currencyCode"`
	Mode         string `json:"mode"`
	Amount       int64  `json:"amount"`
	Reason       string `json:"reason"`
}

func (s *Server) adminUserBalances(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user")
		return
	}
	rows, err := s.db.Query(r.Context(), `select currency.public_id,currency.code,currency.name,currency.icon,
		coalesce(balance.balance,0),currency.status
		from currencies currency left join user_currency_balances balance
		on balance.currency_id=currency.id and balance.user_id=$1
		where currency.status='active' order by currency.display_order,currency.id`, identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user balances")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, name, icon, status string
		var balance int64
		if err = rows.Scan(&publicID, &code, &name, &icon, &balance, &status); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode user balances")
			return
		}
		items = append(items, map[string]any{
			"publicId": publicID, "code": code, "name": name, "icon": icon,
			"balance": balance, "status": status,
		})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user balances")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adjustAdminUserBalance(w http.ResponseWriter, r *http.Request) {
	identity, err := s.resolvePublicIdentity(r.Context(), r.PathValue("id"), "user")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user")
		return
	}
	var request adminBalanceAdjustmentRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid balance adjustment")
		return
	}
	request.CurrencyCode = normalizeCode(request.CurrencyCode)
	request.Mode = strings.ToLower(strings.TrimSpace(request.Mode))
	request.Reason = strings.TrimSpace(request.Reason)
	if request.Mode == "" {
		request.Mode = "set"
	}
	if request.CurrencyCode == "" || (request.Mode != "set" && request.Mode != "adjust") ||
		(request.Mode == "set" && request.Amount < 0) || (request.Mode == "adjust" && request.Amount == 0) ||
		request.Reason == "" || len(request.Reason) > 500 {
		writeError(w, http.StatusBadRequest, "invalid balance adjustment")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin balance adjustment")
		return
	}
	defer tx.Rollback(r.Context())
	var currencyID int64
	if err = tx.QueryRow(r.Context(), `select id from currencies where code=$1 and status='active'`, request.CurrencyCode).Scan(&currencyID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "currency not found or disabled")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve currency")
		return
	}
	current, err := lockCurrencyBalanceTx(r.Context(), tx, identity.InternalID, currencyID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock user balance")
		return
	}
	delta := request.Amount
	if request.Mode == "set" {
		delta = request.Amount - current
	}
	if delta == 0 {
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit user balance")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"currencyCode": request.CurrencyCode, "previousBalance": current,
			"balance": current, "amountDelta": int64(0),
		})
		return
	}
	next, err := applyLockedCurrencyBalanceChangeTx(r.Context(), tx, identity.InternalID, currencyID, current, delta,
		"admin_adjustment", nil, "admin_adjustment", "", map[string]any{
			"reason": request.Reason, "administratorId": currentClaims(r).Subject,
		})
	if errors.Is(err, errInsufficientBalance) {
		writeError(w, http.StatusConflict, "balance cannot be negative")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to adjust user balance")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit user balance")
		return
	}
	annotateActivityID(r, activity.ActionEdit, activity.ObjectEconomy, "user", identity.InternalID, 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"currencyCode": request.CurrencyCode, "previousBalance": current,
		"balance": next, "amountDelta": delta,
	})
}
