package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
)

const economyConfigSettingKey = "economy.config"

type economyCheckinConfig struct {
	Enabled      bool   `json:"enabled"`
	Currency     string `json:"currency"`
	Amount       int64  `json:"amount"`
	MinimumHours int    `json:"minimumHours"`
}

type economyDownloadReward struct {
	ObjectType         string `json:"objectType"`
	Currency           string `json:"currency"`
	DownloadsPerReward int64  `json:"downloadsPerReward"`
	Amount             int64  `json:"amount"`
}

type economyConfigPayload struct {
	Checkin         economyCheckinConfig    `json:"checkin"`
	DownloadRewards []economyDownloadReward `json:"downloadRewards"`
}

type currencyPayload struct {
	PublicID       string         `json:"publicId,omitempty"`
	Code           string         `json:"code"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Icon           string         `json:"icon"`
	Translations   map[string]any `json:"translations"`
	TransferTaxBPS int            `json:"transferTaxBps"`
	Status         string         `json:"status"`
	DisplayOrder   int            `json:"displayOrder"`
}

type shopItemPayload struct {
	PublicID           string         `json:"publicId,omitempty"`
	Code               string         `json:"code"`
	ItemType           string         `json:"itemType"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Icon               string         `json:"icon"`
	Translations       map[string]any `json:"translations"`
	PriceCurrency      string         `json:"priceCurrency"`
	PriceAmount        int64          `json:"priceAmount"`
	PurchasePermission string         `json:"purchasePermission"`
	UsePermission      string         `json:"usePermission"`
	Config             map[string]any `json:"config"`
	Status             string         `json:"status"`
}

func (s *Server) publicCurrencies(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadCurrencies(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load currencies")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) publicShopItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadShopItems(r.Context(), false, currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shop items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) userEconomyOverview(w http.ResponseWriter, r *http.Request) {
	userID := currentClaims(r).Subject
	rows, err := s.db.Query(r.Context(), `select currency.public_id,currency.code,currency.name,currency.icon,
		coalesce(balance.balance,0)
		from currencies currency
		left join user_currency_balances balance on balance.currency_id=currency.id and balance.user_id=$1
		where currency.status='active'
		order by currency.display_order,currency.id`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load balances")
		return
	}
	balances := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, name, icon string
		var balance int64
		if err = rows.Scan(&publicID, &code, &name, &icon, &balance); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to decode balances")
			return
		}
		balances = append(balances, map[string]any{
			"publicId": publicID, "code": code, "name": name, "icon": icon, "balance": balance,
		})
	}
	rows.Close()

	var experience int64
	var level int
	_ = s.db.QueryRow(r.Context(), `select experience,level from user_experience where user_id=$1`, userID).
		Scan(&experience, &level)
	var timezone string
	_ = s.db.QueryRow(r.Context(), `select timezone from users where id=$1`, userID).Scan(&timezone)
	config := s.economyConfigFromSettings(r.Context())
	eligibleAt, checkedInToday := s.nextCheckinState(r.Context(), userID, timezone, config.Checkin.MinimumHours)
	inventory, err := s.userInventory(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load inventory")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balances":   balances,
		"experience": experience,
		"level":      level,
		"timezone":   timezone,
		"checkin": map[string]any{
			"enabled": config.Checkin.Enabled, "currency": config.Checkin.Currency,
			"amount": config.Checkin.Amount, "checkedInToday": checkedInToday, "eligibleAt": eligibleAt,
		},
		"inventory": inventory,
	})
}

func (s *Server) userCurrencyTransactions(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	cursor, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("cursor")), 10, 64)
	rows, err := s.db.Query(r.Context(), `select entry.id,currency.public_id,currency.code,currency.name,currency.icon,
		entry.amount_delta,entry.balance_after,entry.transaction_type,
		coalesce(counterparty.public_id,''),coalesce(counterparty.username,''),
		entry.reference_type,entry.reference_key,entry.created_at
		from currency_transactions entry
		join currencies currency on currency.id=entry.currency_id
		left join users counterparty on counterparty.id=entry.counterparty_user_id
		where entry.user_id=$1 and ($2::bigint<=0 or entry.id<$2)
		order by entry.id desc limit $3`, currentClaims(r).Subject, cursor, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load currency transactions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	var nextCursor string
	var lastID int64
	for rows.Next() {
		var id, amountDelta, balanceAfter int64
		var currencyPublicID, code, name, icon, transactionType string
		var counterpartyPublicID, counterpartyName, referenceType, referenceKey string
		var createdAt time.Time
		if err = rows.Scan(&id, &currencyPublicID, &code, &name, &icon, &amountDelta, &balanceAfter,
			&transactionType, &counterpartyPublicID, &counterpartyName, &referenceType, &referenceKey, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode currency transactions")
			return
		}
		if len(items) == limit {
			nextCursor = strconv.FormatInt(lastID, 10)
			break
		}
		items = append(items, map[string]any{
			"currency":    map[string]any{"publicId": currencyPublicID, "code": code, "name": name, "icon": icon},
			"amountDelta": amountDelta, "balanceAfter": balanceAfter, "transactionType": transactionType,
			"counterparty":  map[string]any{"publicId": counterpartyPublicID, "username": counterpartyName},
			"referenceType": referenceType, "referenceKey": referenceKey, "createdAt": createdAt,
		})
		lastID = id
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load currency transactions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (s *Server) userExperienceTransactions(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	cursor, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("cursor")), 10, 64)
	rows, err := s.db.Query(r.Context(), `select id,amount_delta,experience_after,reason,reference_type,reference_key,created_at
		from experience_transactions
		where user_id=$1 and ($2::bigint<=0 or id<$2)
		order by id desc limit $3`, currentClaims(r).Subject, cursor, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load experience transactions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	var nextCursor string
	var lastID int64
	for rows.Next() {
		var id, amountDelta, experienceAfter int64
		var reason, referenceType, referenceKey string
		var createdAt time.Time
		if err = rows.Scan(&id, &amountDelta, &experienceAfter, &reason, &referenceType, &referenceKey, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode experience transactions")
			return
		}
		if len(items) == limit {
			nextCursor = strconv.FormatInt(lastID, 10)
			break
		}
		items = append(items, map[string]any{
			"amountDelta": amountDelta, "experienceAfter": experienceAfter, "reason": reason,
			"referenceType": referenceType, "referenceKey": referenceKey, "createdAt": createdAt,
		})
		lastID = id
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load experience transactions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "nextCursor": nextCursor})
}

func (s *Server) checkIn(w http.ResponseWriter, r *http.Request) {
	config := s.economyConfigFromSettings(r.Context())
	if !config.Checkin.Enabled {
		writeError(w, http.StatusConflict, "check-in is disabled")
		return
	}
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start check-in")
		return
	}
	defer tx.Rollback(r.Context())

	var timezone string
	if err = tx.QueryRow(r.Context(), `select timezone from users where id=$1 for update`, userID).Scan(&timezone); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user timezone")
		return
	}
	location := loadLocation(timezone)
	now := time.Now().UTC()
	localDate := now.In(location).Format("2006-01-02")
	var lastClaim *time.Time
	var lastLocalDate string
	err = tx.QueryRow(r.Context(), `select claimed_at,local_date::text from user_checkins
		where user_id=$1 order by claimed_at desc limit 1`, userID).Scan(&lastClaim, &lastLocalDate)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load check-in history")
		return
	}
	if lastLocalDate == localDate {
		writeError(w, http.StatusConflict, "already checked in for the current local date")
		return
	}
	minimum := time.Duration(config.Checkin.MinimumHours) * time.Hour
	if lastClaim != nil && now.Sub(lastClaim.UTC()) < minimum {
		writeError(w, http.StatusConflict, "check-in cooldown has not elapsed")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into user_checkins(user_id,local_date,timezone,claimed_at)
		values($1,$2::date,$3,$4)`, userID, localDate, timezone, now); err != nil {
		writeError(w, http.StatusConflict, "check-in could not be recorded")
		return
	}
	balance, err := changeCurrencyBalanceTx(
		r.Context(), tx, userID, config.Checkin.Currency, config.Checkin.Amount,
		"checkin", nil, "checkin", localDate, nil,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to grant check-in reward")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit check-in")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"localDate": localDate, "timezone": timezone, "currency": config.Checkin.Currency,
		"amount": config.Checkin.Amount, "balance": balance,
	})
}

func (s *Server) transferCurrency(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Recipient string `json:"recipient"`
		Currency  string `json:"currency"`
		Amount    int64  `json:"amount"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid transfer request")
		return
	}
	request.Recipient = strings.TrimSpace(request.Recipient)
	request.Currency = normalizeCode(request.Currency)
	if request.Recipient == "" || request.Currency == "" || request.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "recipient, currency and a positive amount are required")
		return
	}
	senderID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transfer")
		return
	}
	defer tx.Rollback(r.Context())

	recipientID, err := resolveTransferRecipient(r.Context(), tx, request.Recipient)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "recipient was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve recipient")
		return
	}
	if recipientID == senderID {
		writeError(w, http.StatusBadRequest, "cannot transfer currency to yourself")
		return
	}
	var currencyID int64
	var taxBPS int
	if err = tx.QueryRow(r.Context(), `select id,transfer_tax_bps from currencies where code=$1 and status='active'`,
		request.Currency).Scan(&currencyID, &taxBPS); err != nil {
		writeError(w, http.StatusBadRequest, "currency is unavailable")
		return
	}
	if err = lockCurrencyBalancesTx(r.Context(), tx, currencyID, senderID, recipientID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock balances")
		return
	}
	tax := int64(math.Ceil(float64(request.Amount*int64(taxBPS)) / 10000))
	received := request.Amount - tax
	if received <= 0 {
		writeError(w, http.StatusBadRequest, "transfer amount is too small after tax")
		return
	}
	referenceKey := fmt.Sprintf("%d:%d", senderID, time.Now().UTC().UnixNano())
	senderBalance, err := changeCurrencyBalanceByIDTx(
		r.Context(), tx, senderID, currencyID, -request.Amount, "transfer_out",
		&recipientID, "transfer", referenceKey, map[string]any{"tax": tax, "received": received},
	)
	if err != nil {
		if errors.Is(err, errInsufficientBalance) {
			writeError(w, http.StatusConflict, "insufficient balance")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to debit sender")
		return
	}
	recipientBalance, err := changeCurrencyBalanceByIDTx(
		r.Context(), tx, recipientID, currencyID, received, "transfer_in",
		&senderID, "transfer", referenceKey, map[string]any{"tax": tax, "sent": request.Amount},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to credit recipient")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transfer")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"currency": request.Currency, "amount": request.Amount, "tax": tax, "received": received,
		"senderBalance": senderBalance, "recipientBalance": recipientBalance,
	})
}

func (s *Server) purchaseShopItem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ItemCode string `json:"itemCode"`
		Quantity int    `json:"quantity"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid purchase request")
		return
	}
	request.ItemCode = normalizeCode(request.ItemCode)
	if request.Quantity <= 0 {
		request.Quantity = 1
	}
	if request.Quantity > 100 {
		writeError(w, http.StatusBadRequest, "quantity is too large")
		return
	}
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start purchase")
		return
	}
	defer tx.Rollback(r.Context())
	var itemID, currencyID, unitPrice int64
	var publicID, permission string
	err = tx.QueryRow(r.Context(), `select item.id,item.public_id,item.price_currency_id,item.price_amount,item.purchase_permission
		from shop_items item where item.code=$1 and item.status='active' for update`,
		request.ItemCode).Scan(&itemID, &publicID, &currencyID, &unitPrice, &permission)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shop item was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shop item")
		return
	}
	if permission != "" && !s.userHasPermission(r.Context(), userID, permission) {
		writeError(w, http.StatusForbidden, "missing permission to purchase this item")
		return
	}
	total := unitPrice * int64(request.Quantity)
	if _, err = lockCurrencyBalanceTx(r.Context(), tx, userID, currencyID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock balance")
		return
	}
	balance, err := changeCurrencyBalanceByIDTx(
		r.Context(), tx, userID, currencyID, -total, "shop_purchase", nil,
		"shop_item", publicID, map[string]any{"quantity": request.Quantity, "unitPrice": unitPrice},
	)
	if errors.Is(err, errInsufficientBalance) {
		writeError(w, http.StatusConflict, "insufficient balance")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to debit balance")
		return
	}
	var inventoryQuantity int
	if err = tx.QueryRow(r.Context(), `insert into user_inventory(user_id,shop_item_id,quantity,updated_at)
		values($1,$2,$3,now())
		on conflict(user_id,shop_item_id) do update
		set quantity=user_inventory.quantity+excluded.quantity,updated_at=now()
		returning quantity`, userID, itemID, request.Quantity).Scan(&inventoryQuantity); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update inventory")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into shop_purchases(
		user_id,shop_item_id,currency_id,quantity,unit_price,total_price
	) values($1,$2,$3,$4,$5,$6)`, userID, itemID, currencyID, request.Quantity, unitPrice, total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record purchase")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit purchase")
		return
	}
	annotateActivity(r, activity.ActionCreate, activity.ObjectShopItem, publicID, 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"itemCode": request.ItemCode, "quantity": inventoryQuantity, "balance": balance,
	})
}

func (s *Server) useShopItem(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ItemCode   string `json:"itemCode"`
		FileID     string `json:"fileId"`
		TargetType string `json:"targetType"`
		TargetID   string `json:"targetId"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid item use request")
		return
	}
	request.ItemCode = normalizeCode(request.ItemCode)
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start item use")
		return
	}
	defer tx.Rollback(r.Context())
	var itemID int64
	var publicID, itemType, permission string
	var rawConfig []byte
	err = tx.QueryRow(r.Context(), `select id,public_id,item_type,use_permission,config from shop_items
		where code=$1 and status='active' for update`, request.ItemCode).
		Scan(&itemID, &publicID, &itemType, &permission, &rawConfig)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shop item was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shop item")
		return
	}
	if permission != "" && !s.userHasPermission(r.Context(), userID, permission) {
		writeError(w, http.StatusForbidden, "missing permission to use this item")
		return
	}
	var quantity int
	if err = tx.QueryRow(r.Context(), `select quantity from user_inventory
		where user_id=$1 and shop_item_id=$2 for update`, userID, itemID).Scan(&quantity); err != nil || quantity <= 0 {
		writeError(w, http.StatusConflict, "item is not available in inventory")
		return
	}
	switch itemType {
	case "profile_background":
		file, resolveErr := resolveTrustedRasterOSSFilePublicID(r.Context(), tx, request.FileID, ossRasterBindingScope{UploaderID: userID})
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, "profile background file is required")
			return
		}
		backgroundURL := ossStoredObjectURL(s.ossConfigFromSettings(r.Context()), file.ObjectKey)
		if _, err = tx.Exec(r.Context(), `update users set profile_background_file_id=$2,
			profile_background_url=$3,updated_at=now() where id=$1`, userID, file.ID, backgroundURL); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update profile background")
			return
		}
	case "project_heat_boost", "server_heat_boost":
		targetType := normalizeRatingTargetType(request.TargetType)
		if itemType == "server_heat_boost" && targetType != "minecraft_server" {
			writeError(w, http.StatusBadRequest, "server heat boosts can only target a Minecraft server")
			return
		}
		if itemType == "project_heat_boost" && (targetType == "" || targetType == "minecraft_server") {
			writeError(w, http.StatusBadRequest, "project heat boosts cannot target a Minecraft server")
			return
		}
		request.TargetID = strings.ToLower(strings.TrimSpace(request.TargetID))
		if !validCatalogPublicID(request.TargetID) {
			writeError(w, http.StatusBadRequest, "a collected project target is required")
			return
		}
		target, resolveErr := s.resolveRateableTarget(r.Context(), targetType, request.TargetID)
		if resolveErr != nil {
			writeError(w, http.StatusNotFound, "heat boost target was not found")
			return
		}
		routeID, internalID := target.RouteID, target.InternalID
		if !canEditReviewTarget(r.Context(), tx, currentClaims(r), targetType, internalID, request.TargetID) {
			writeError(w, http.StatusForbidden, "only a project editor can apply a heat boost")
			return
		}
		config := struct {
			Power         float64 `json:"power"`
			HalfLifeHours int     `json:"halfLifeHours"`
			DurationHours int     `json:"durationHours"`
		}{Power: 1, HalfLifeHours: 72, DurationHours: 432}
		if len(rawConfig) > 0 {
			_ = json.Unmarshal(rawConfig, &config)
		}
		if config.Power <= 0 || config.Power > 100 {
			config.Power = 1
		}
		if config.HalfLifeHours < 1 || config.HalfLifeHours > 8760 {
			config.HalfLifeHours = 72
		}
		if config.DurationHours < 1 || config.DurationHours > 87600 {
			config.DurationHours = 432
		}
		var usedCount int
		if err = tx.QueryRow(r.Context(), `select count(*) from content_heat_promotions where object_route_id=$1`, routeID).
			Scan(&usedCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect existing heat boosts")
			return
		}
		effectivePower := effectivePromotionPower(config.Power, usedCount)
		promotionKind := "project"
		if itemType == "server_heat_boost" {
			promotionKind = "server"
		}
		if _, err = tx.Exec(r.Context(), `insert into content_heat_promotions(
			object_route_id,shop_item_id,applied_by,promotion_kind,base_power,effective_power,
			half_life_hours,sequence_no,expires_at
		) values($1,$2,$3,$4,$5,$6,$7,$8,now()+make_interval(hours=>$9))`,
			routeID, itemID, userID, promotionKind, config.Power, effectivePower,
			config.HalfLifeHours, usedCount+1, config.DurationHours); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply heat boost")
			return
		}
		if _, err = tx.Exec(r.Context(), `select enqueue_content_stats_refresh($1,true,true)`, routeID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to refresh project heat")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "shop item type is not supported")
		return
	}
	if _, err = tx.Exec(r.Context(), `update user_inventory set quantity=quantity-1,updated_at=now()
		where user_id=$1 and shop_item_id=$2`, userID, itemID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to consume item")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit item use")
		return
	}
	annotateActivity(r, activity.ActionUse, activity.ObjectShopItem, publicID, 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"itemCode": request.ItemCode, "remaining": quantity - 1, "itemPublicId": publicID,
	})
}

func effectivePromotionPower(basePower float64, previousUseCount int) float64 {
	return basePower / (1 + float64(max(0, previousUseCount))*0.2)
}

func (s *Server) adminEconomyConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.economyConfigFromSettings(r.Context()))
}

func (s *Server) updateEconomyConfig(w http.ResponseWriter, r *http.Request) {
	var payload economyConfigPayload
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid economy configuration")
		return
	}
	payload, err := normalizeEconomyConfig(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, code := range append([]string{payload.Checkin.Currency}, rewardCurrencyCodes(payload.DownloadRewards)...) {
		if code == "" {
			continue
		}
		var exists bool
		if err = s.db.QueryRow(r.Context(), `select exists(select 1 from currencies where code=$1)`, code).Scan(&exists); err != nil || !exists {
			writeError(w, http.StatusBadRequest, "economy configuration references an unknown currency")
			return
		}
	}
	raw, _ := json.Marshal(payload)
	if _, err = s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now())
		on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		economyConfigSettingKey, raw, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save economy configuration")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) adminCurrencies(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadCurrencies(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load currencies")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCurrency(w http.ResponseWriter, r *http.Request) {
	s.saveCurrency(w, r, "")
}

func (s *Server) updateCurrency(w http.ResponseWriter, r *http.Request) {
	s.saveCurrency(w, r, strings.ToLower(strings.TrimSpace(r.PathValue("publicId"))))
}

func (s *Server) saveCurrency(w http.ResponseWriter, r *http.Request, currentPublicID string) {
	var payload currencyPayload
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid currency")
		return
	}
	payload.Code = normalizeCode(payload.Code)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Icon = strings.TrimSpace(payload.Icon)
	payload.Status = strings.ToLower(strings.TrimSpace(payload.Status))
	if payload.Status == "" {
		payload.Status = "active"
	}
	if payload.Code == "" || payload.Name == "" || (payload.Status != "active" && payload.Status != "disabled") ||
		payload.TransferTaxBPS < 0 || payload.TransferTaxBPS > 10000 {
		writeError(w, http.StatusBadRequest, "currency fields are invalid")
		return
	}
	translations, _ := json.Marshal(payload.Translations)
	var publicID string
	var err error
	if currentPublicID == "" {
		err = s.db.QueryRow(r.Context(), `insert into currencies(
			code,name,description,icon,translations,transfer_tax_bps,status,display_order
		) values($1,$2,$3,$4,$5,$6,$7,$8) returning public_id`,
			payload.Code, payload.Name, payload.Description, payload.Icon, translations,
			payload.TransferTaxBPS, payload.Status, payload.DisplayOrder).Scan(&publicID)
	} else {
		err = s.db.QueryRow(r.Context(), `update currencies set code=$2,name=$3,description=$4,icon=$5,
			translations=$6,transfer_tax_bps=$7,status=$8,display_order=$9,updated_at=now()
			where public_id=$1 returning public_id`,
			currentPublicID, payload.Code, payload.Name, payload.Description, payload.Icon, translations,
			payload.TransferTaxBPS, payload.Status, payload.DisplayOrder).Scan(&publicID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "currency was not found")
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "currency code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save currency")
		return
	}
	payload.PublicID = publicID
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[currentPublicID == ""], payload)
}

func (s *Server) adminShopItems(w http.ResponseWriter, r *http.Request) {
	items, err := s.loadShopItems(r.Context(), true, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shop items")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createShopItem(w http.ResponseWriter, r *http.Request) {
	s.saveShopItem(w, r, "")
}

func (s *Server) updateShopItem(w http.ResponseWriter, r *http.Request) {
	s.saveShopItem(w, r, strings.ToLower(strings.TrimSpace(r.PathValue("publicId"))))
}

func (s *Server) saveShopItem(w http.ResponseWriter, r *http.Request, currentPublicID string) {
	var payload shopItemPayload
	if decodeJSON(r, &payload) != nil {
		writeError(w, http.StatusBadRequest, "invalid shop item")
		return
	}
	payload.Code = normalizeCode(payload.Code)
	payload.ItemType = normalizeCode(payload.ItemType)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	payload.Icon = strings.TrimSpace(payload.Icon)
	payload.PriceCurrency = normalizeCode(payload.PriceCurrency)
	payload.PurchasePermission = normalizeCode(payload.PurchasePermission)
	payload.UsePermission = normalizeCode(payload.UsePermission)
	payload.Status = strings.ToLower(strings.TrimSpace(payload.Status))
	if payload.Status == "" {
		payload.Status = "active"
	}
	if payload.Code == "" || payload.ItemType == "" || payload.Name == "" || payload.PriceCurrency == "" ||
		payload.PriceAmount < 0 || (payload.Status != "active" && payload.Status != "disabled") {
		writeError(w, http.StatusBadRequest, "shop item fields are invalid")
		return
	}
	for _, permission := range []string{payload.PurchasePermission, payload.UsePermission} {
		if permission != "" {
			s.registerDeclaredPermission(permission)
		}
	}
	translations, _ := json.Marshal(payload.Translations)
	config, _ := json.Marshal(payload.Config)
	var publicID string
	var err error
	if currentPublicID == "" {
		err = s.db.QueryRow(r.Context(), `insert into shop_items(
			code,item_type,name,description,icon,translations,price_currency_id,price_amount,
			purchase_permission,use_permission,config,status
		) select $1,$2,$3,$4,$5,$6,currency.id,$7,$8,$9,$10,$11
		  from currencies currency where currency.code=$12
		returning public_id`, payload.Code, payload.ItemType, payload.Name, payload.Description, payload.Icon,
			translations, payload.PriceAmount, payload.PurchasePermission, payload.UsePermission, config,
			payload.Status, payload.PriceCurrency).Scan(&publicID)
	} else {
		err = s.db.QueryRow(r.Context(), `update shop_items item set
			code=$2,item_type=$3,name=$4,description=$5,icon=$6,translations=$7,
			price_currency_id=currency.id,price_amount=$8,purchase_permission=$9,use_permission=$10,
			config=$11,status=$12,updated_at=now()
			from currencies currency where item.public_id=$1 and currency.code=$13
			returning item.public_id`, currentPublicID, payload.Code, payload.ItemType, payload.Name,
			payload.Description, payload.Icon, translations, payload.PriceAmount, payload.PurchasePermission,
			payload.UsePermission, config, payload.Status, payload.PriceCurrency).Scan(&publicID)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "shop item or currency was not found")
		return
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "shop item code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save shop item")
		return
	}
	payload.PublicID = publicID
	writeJSON(w, map[bool]int{true: http.StatusCreated, false: http.StatusOK}[currentPublicID == ""], payload)
}

func (s *Server) loadCurrencies(ctx context.Context, includeDisabled bool) ([]currencyPayload, error) {
	where := "where status='active'"
	if includeDisabled {
		where = ""
	}
	rows, err := s.db.Query(ctx, `select public_id,code,name,description,icon,translations,
		transfer_tax_bps,status,display_order from currencies `+where+` order by display_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]currencyPayload, 0)
	for rows.Next() {
		var item currencyPayload
		var translations []byte
		if err = rows.Scan(&item.PublicID, &item.Code, &item.Name, &item.Description, &item.Icon,
			&translations, &item.TransferTaxBPS, &item.Status, &item.DisplayOrder); err != nil {
			return nil, err
		}
		item.Translations = map[string]any{}
		_ = json.Unmarshal(translations, &item.Translations)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadShopItems(ctx context.Context, includeDisabled bool, userID int64) ([]map[string]any, error) {
	where := "where item.status='active' and currency.status='active'"
	if includeDisabled {
		where = ""
	}
	rows, err := s.db.Query(ctx, `select item.public_id,item.code,item.item_type,item.name,item.description,
		item.icon,item.translations,currency.code,item.price_amount,item.purchase_permission,item.use_permission,
		item.config,item.status,coalesce(inventory.quantity,0)
		from shop_items item
		join currencies currency on currency.id=item.price_currency_id
		left join user_inventory inventory on inventory.shop_item_id=item.id and inventory.user_id=$1 `+where+`
		order by item.created_at,item.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var resolved []security.PermissionRule
	if userID > 0 {
		_, permissions, permissionErr := s.resolveUserRootPermissions(ctx, userID)
		if permissionErr != nil {
			return nil, permissionErr
		}
		resolved = permissions
	}
	allows := func(permission string) bool {
		if permission == "" {
			return true
		}
		return permissionRulesAllow(resolved, permission)
	}
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, itemType, name, description, icon, priceCurrency, purchasePermission, usePermission, status string
		var priceAmount int64
		var quantity int
		var translationsRaw, configRaw []byte
		if err = rows.Scan(&publicID, &code, &itemType, &name, &description, &icon, &translationsRaw,
			&priceCurrency, &priceAmount, &purchasePermission, &usePermission, &configRaw, &status, &quantity); err != nil {
			return nil, err
		}
		translations := map[string]any{}
		config := map[string]any{}
		_ = json.Unmarshal(translationsRaw, &translations)
		_ = json.Unmarshal(configRaw, &config)
		items = append(items, map[string]any{
			"publicId": publicID, "code": code, "itemType": itemType, "name": name,
			"description": description, "icon": icon, "translations": translations,
			"priceCurrency": priceCurrency, "priceAmount": priceAmount,
			"purchasePermission": purchasePermission, "usePermission": usePermission,
			"config": config, "status": status, "inventoryQuantity": quantity,
			"canPurchase": userID > 0 && allows(purchasePermission),
			"canUse":      userID > 0 && quantity > 0 && allows(usePermission),
		})
	}
	return items, rows.Err()
}

func (s *Server) userInventory(ctx context.Context, userID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `select item.public_id,item.code,item.item_type,item.name,item.icon,inventory.quantity
		from user_inventory inventory join shop_items item on item.id=inventory.shop_item_id
		where inventory.user_id=$1 and inventory.quantity>0 order by item.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, itemType, name, icon string
		var quantity int
		if err = rows.Scan(&publicID, &code, &itemType, &name, &icon, &quantity); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			"publicId": publicID, "code": code, "itemType": itemType,
			"name": name, "icon": icon, "quantity": quantity,
		})
	}
	return items, rows.Err()
}

func (s *Server) economyConfigFromSettings(ctx context.Context) economyConfigPayload {
	payload := defaultEconomyConfig()
	var raw []byte
	if err := s.db.QueryRow(ctx, `select value from system_settings where key=$1`, economyConfigSettingKey).
		Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	normalized, err := normalizeEconomyConfig(payload)
	if err != nil {
		return defaultEconomyConfig()
	}
	return normalized
}

func defaultEconomyConfig() economyConfigPayload {
	return economyConfigPayload{
		Checkin:         economyCheckinConfig{Enabled: true, Currency: "gold_nugget", Amount: 1, MinimumHours: 20},
		DownloadRewards: []economyDownloadReward{},
	}
}

func normalizeEconomyConfig(payload economyConfigPayload) (economyConfigPayload, error) {
	payload.Checkin.Currency = normalizeCode(payload.Checkin.Currency)
	if payload.Checkin.MinimumHours <= 0 {
		payload.Checkin.MinimumHours = 20
	}
	if payload.Checkin.MinimumHours > 72 || payload.Checkin.Amount < 0 {
		return payload, errors.New("check-in configuration is invalid")
	}
	if payload.Checkin.Enabled && (payload.Checkin.Currency == "" || payload.Checkin.Amount <= 0) {
		return payload, errors.New("enabled check-in requires a currency and positive amount")
	}
	seen := map[string]struct{}{}
	rewards := make([]economyDownloadReward, 0, len(payload.DownloadRewards))
	for _, reward := range payload.DownloadRewards {
		reward.ObjectType = normalizeCode(reward.ObjectType)
		reward.Currency = normalizeCode(reward.Currency)
		if reward.ObjectType == "" || reward.Currency == "" || reward.DownloadsPerReward <= 0 || reward.Amount <= 0 {
			return payload, errors.New("download reward configuration is invalid")
		}
		key := reward.ObjectType + "\x00" + reward.Currency
		if _, exists := seen[key]; exists {
			return payload, errors.New("duplicate download reward rule")
		}
		seen[key] = struct{}{}
		rewards = append(rewards, reward)
	}
	payload.DownloadRewards = rewards
	return payload, nil
}

func rewardCurrencyCodes(rewards []economyDownloadReward) []string {
	result := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		result = append(result, reward.Currency)
	}
	return result
}

func loadLocation(name string) *time.Location {
	location, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return time.UTC
	}
	return location
}

func (s *Server) nextCheckinState(ctx context.Context, userID int64, timezone string, minimumHours int) (*time.Time, bool) {
	var claimedAt time.Time
	var localDate string
	err := s.db.QueryRow(ctx, `select claimed_at,local_date::text from user_checkins
		where user_id=$1 order by claimed_at desc limit 1`, userID).Scan(&claimedAt, &localDate)
	if err != nil {
		return nil, false
	}
	now := time.Now().UTC()
	checkedInToday := localDate == now.In(loadLocation(timezone)).Format("2006-01-02")
	eligible := claimedAt.Add(time.Duration(minimumHours) * time.Hour)
	return &eligible, checkedInToday
}

var errInsufficientBalance = errors.New("insufficient balance")

func resolveTransferRecipient(ctx context.Context, tx pgx.Tx, value string) (int64, error) {
	if id, err := strconv.ParseInt(value, 10, 64); err == nil && id > 0 {
		var userID int64
		err = tx.QueryRow(ctx, `select id from users where id=$1 and status='active'`, id).Scan(&userID)
		return userID, err
	}
	var userID int64
	err := tx.QueryRow(ctx, `select id from users where status='active' and
		(lower(username)=lower($1) or lower(email)=lower($1) or public_id=lower($1))`, value).Scan(&userID)
	return userID, err
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
	next := current + delta
	if next < 0 {
		return current, errInsufficientBalance
	}
	if _, err := tx.Exec(ctx, `update user_currency_balances set balance=$3,updated_at=now()
		where user_id=$1 and currency_id=$2`, userID, currencyID, next); err != nil {
		return 0, err
	}
	raw, _ := json.Marshal(metadata)
	if _, err := tx.Exec(ctx, `insert into currency_transactions(
		user_id,currency_id,amount_delta,balance_after,transaction_type,counterparty_user_id,
		reference_type,reference_key,metadata
	) values($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		userID, currencyID, delta, next, transactionType, counterparty, referenceType, referenceKey, raw); err != nil {
		return 0, err
	}
	return next, nil
}

func (s *Server) recordOwnedContentDownload(
	ctx context.Context,
	objectType, objectPublicID string,
	ownerID, downloaderID int64,
) error {
	if ownerID <= 0 || ownerID == downloaderID {
		return nil
	}
	config := s.economyConfigFromSettings(ctx)
	rules := make([]economyDownloadReward, 0)
	for _, rule := range config.DownloadRewards {
		if rule.ObjectType == objectType {
			rules = append(rules, rule)
		}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
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
		returning downloads`, objectRouteID, ownerID).Scan(&downloads); err != nil {
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
		rewardAmount := deltaSteps * rule.Amount
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
	return tx.Commit(ctx)
}
