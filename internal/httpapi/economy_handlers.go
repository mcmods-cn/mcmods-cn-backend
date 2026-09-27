package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
)

const economyConfigSettingKey = "economy.config"

const (
	maxCurrencyAmount       int64 = 1_000_000_000_000
	maxCurrencyBalance      int64 = maxCurrencyAmount * 100
	maxShopPurchaseQuantity       = 100
	maxInventoryQuantity          = 1_000_000
)

var errInactiveEconomyCurrency = errors.New("economy rule references a currency that is not active")

func isSupportedShopItemType(itemType string) bool {
	switch itemType {
	case "profile_background", "project_heat_boost", "server_heat_boost":
		return true
	default:
		return false
	}
}

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

func collectEconomyBalances(rows checkedRows) ([]map[string]any, error) {
	defer rows.Close()
	balances := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, code, name, icon string
		var balance int64
		if err := rows.Scan(&publicID, &code, &name, &icon, &balance); err != nil {
			return nil, fmt.Errorf("scan economy balance: %w", err)
		}
		balances = append(balances, map[string]any{
			"publicId": publicID, "code": code, "name": name, "icon": icon, "balance": balance,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate economy balances: %w", err)
	}
	return balances, nil
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
	balances, err := collectEconomyBalances(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load balances")
		return
	}

	var experience int64
	var level int
	err = s.db.QueryRow(r.Context(), `select experience,level from user_experience where user_id=$1`, userID).
		Scan(&experience, &level)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load user experience")
		return
	}
	var timezone string
	if err = s.db.QueryRow(r.Context(), `select timezone from users where id=$1`, userID).Scan(&timezone); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user timezone")
		return
	}
	config, err := s.economyConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load economy configuration")
		return
	}
	eligibleAt, checkedInToday, err := s.nextCheckinState(r.Context(), userID, timezone, config.Checkin.MinimumHours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load check-in history")
		return
	}
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
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start check-in")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockEconomyConfigurationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock economy configuration")
		return
	}
	config, err := loadEconomyConfig(r.Context(), tx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load economy configuration")
		return
	}
	if !config.Checkin.Enabled {
		writeError(w, http.StatusConflict, "check-in is disabled")
		return
	}

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
	if request.Recipient == "" || request.Currency == "" || request.Amount <= 0 || request.Amount > maxCurrencyAmount {
		writeError(w, http.StatusBadRequest, "recipient, currency and an amount within the supported range are required")
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
	tax, received, amountErr := calculateTransferAmounts(request.Amount, taxBPS)
	if amountErr != nil {
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
	if request.Quantity > maxShopPurchaseQuantity {
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
	var publicID, permission, itemType string
	err = tx.QueryRow(r.Context(), `select item.id,item.public_id,item.price_currency_id,item.price_amount,
		item.purchase_permission,item.item_type
		from shop_items item
		join currencies currency on currency.id=item.price_currency_id and currency.status='active'
		where item.code=$1 and item.status='active' for update of item`, request.ItemCode).
		Scan(&itemID, &publicID, &currencyID, &unitPrice, &permission, &itemType)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "shop item was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load shop item")
		return
	}
	if !isSupportedShopItemType(itemType) {
		writeError(w, http.StatusConflict, "shop item type is not supported")
		return
	}
	if permission != "" && !s.userHasPermission(r.Context(), userID, permission) {
		writeError(w, http.StatusForbidden, "missing permission to purchase this item")
		return
	}
	total, totalErr := calculateShopTotal(unitPrice, request.Quantity)
	if totalErr != nil {
		writeError(w, http.StatusBadRequest, "shop item price is outside the supported range")
		return
	}
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
		where user_inventory.quantity <= $4-excluded.quantity
		returning quantity`, userID, itemID, request.Quantity, maxInventoryQuantity).Scan(&inventoryQuantity); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "inventory limit would be exceeded")
		return
	} else if err != nil {
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
	err = tx.QueryRow(r.Context(), `select quantity from user_inventory
		where user_id=$1 and shop_item_id=$2 for update`, userID, itemID).Scan(&quantity)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "item is not available in inventory")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load inventory item")
		return
	}
	if quantity <= 0 {
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
			if _, decodeErr := decodeStoredJSONObject(rawConfig, "shop item configuration"); decodeErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to decode shop item configuration")
				return
			}
			if decodeErr := json.Unmarshal(rawConfig, &config); decodeErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to decode shop item configuration")
				return
			}
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
		sequenceNo, sequenceErr := nextHeatPromotionSequenceTx(r.Context(), tx, routeID)
		if sequenceErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to reserve heat boost sequence")
			return
		}
		effectivePower := effectivePromotionPower(config.Power, sequenceNo-1)
		promotionKind := "project"
		if itemType == "server_heat_boost" {
			promotionKind = "server"
		}
		if _, err = tx.Exec(r.Context(), `insert into content_heat_promotions(
			object_route_id,shop_item_id,applied_by,promotion_kind,base_power,effective_power,
			half_life_hours,sequence_no,expires_at
		) values($1,$2,$3,$4,$5,$6,$7,$8,now()+make_interval(hours=>$9))`,
			routeID, itemID, userID, promotionKind, config.Power, effectivePower,
			config.HalfLifeHours, sequenceNo, config.DurationHours); err != nil {
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

func nextHeatPromotionSequenceTx(ctx context.Context, tx pgx.Tx, objectRouteID int64) (int64, error) {
	var sequence int64
	err := tx.QueryRow(ctx, `insert into content_heat_promotion_counters(object_route_id,last_sequence_no)
		values($1,1)
		on conflict(object_route_id) do update
		set last_sequence_no=content_heat_promotion_counters.last_sequence_no+1
		returning last_sequence_no`, objectRouteID).Scan(&sequence)
	return sequence, err
}

func effectivePromotionPower(basePower float64, previousUseCount int64) float64 {
	return basePower / (1 + float64(max(0, previousUseCount))*0.2)
}

func (s *Server) adminEconomyConfig(w http.ResponseWriter, r *http.Request) {
	config, err := s.economyConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load economy configuration")
		return
	}
	writeJSON(w, http.StatusOK, config)
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start economy configuration update")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockEconomyConfigurationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock economy configuration")
		return
	}
	if err = validateActiveEconomyCurrencyReferencesTx(r.Context(), tx, payload); err != nil {
		if errors.Is(err, errInactiveEconomyCurrency) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to validate economy currencies")
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode economy configuration")
		return
	}
	if _, err = tx.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now())
		on conflict(key) do update set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		economyConfigSettingKey, raw, currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save economy configuration")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit economy configuration")
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start currency update")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockEconomyConfigurationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock economy configuration")
		return
	}
	translations, err := json.Marshal(payload.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "currency translations are invalid")
		return
	}
	var publicID string
	if currentPublicID == "" {
		err = tx.QueryRow(r.Context(), `insert into currencies(
			code,name,description,icon,translations,transfer_tax_bps,status,display_order
		) values($1,$2,$3,$4,$5,$6,$7,$8) returning public_id`,
			payload.Code, payload.Name, payload.Description, payload.Icon, translations,
			payload.TransferTaxBPS, payload.Status, payload.DisplayOrder).Scan(&publicID)
	} else {
		var currentCode string
		err = tx.QueryRow(r.Context(), `select code from currencies where public_id=$1 for update`, currentPublicID).
			Scan(&currentCode)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "currency was not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load currency")
			return
		}
		if currentCode != payload.Code || payload.Status != "active" {
			blockers, blockerErr := economyCurrencyReferenceBlockersTx(r.Context(), tx, currentCode)
			if blockerErr != nil {
				writeError(w, http.StatusInternalServerError, "failed to inspect currency references")
				return
			}
			if len(blockers) > 0 {
				writeError(w, http.StatusConflict, "currency is referenced by "+strings.Join(blockers, ", "))
				return
			}
		}
		err = tx.QueryRow(r.Context(), `update currencies set code=$2,name=$3,description=$4,icon=$5,
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
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit currency")
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
	if payload.Code == "" || !isSupportedShopItemType(payload.ItemType) || payload.Name == "" || payload.PriceCurrency == "" ||
		payload.PriceAmount < 0 || payload.PriceAmount > maxCurrencyAmount ||
		(payload.Status != "active" && payload.Status != "disabled") {
		writeError(w, http.StatusBadRequest, "shop item fields are invalid")
		return
	}
	for _, permission := range []string{payload.PurchasePermission, payload.UsePermission} {
		if permission != "" {
			s.registerDeclaredPermission(permission)
		}
	}
	translations, err := json.Marshal(payload.Translations)
	if err != nil {
		writeError(w, http.StatusBadRequest, "shop item translations are invalid")
		return
	}
	config, err := json.Marshal(payload.Config)
	if err != nil {
		writeError(w, http.StatusBadRequest, "shop item configuration is invalid")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start shop item update")
		return
	}
	defer tx.Rollback(r.Context())
	if err = lockEconomyConfigurationTx(r.Context(), tx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock economy configuration")
		return
	}
	var publicID string
	if currentPublicID == "" {
		err = tx.QueryRow(r.Context(), `insert into shop_items(
			code,item_type,name,description,icon,translations,price_currency_id,price_amount,
			purchase_permission,use_permission,config,status
		) select $1,$2,$3,$4,$5,$6,currency.id,$7,$8,$9,$10,$11
		  from currencies currency where currency.code=$12 and ($11='disabled' or currency.status='active')
		returning public_id`, payload.Code, payload.ItemType, payload.Name, payload.Description, payload.Icon,
			translations, payload.PriceAmount, payload.PurchasePermission, payload.UsePermission, config,
			payload.Status, payload.PriceCurrency).Scan(&publicID)
	} else {
		err = tx.QueryRow(r.Context(), `update shop_items item set
			code=$2,item_type=$3,name=$4,description=$5,icon=$6,translations=$7,
			price_currency_id=currency.id,price_amount=$8,purchase_permission=$9,use_permission=$10,
			config=$11,status=$12,updated_at=now()
			from currencies currency where item.public_id=$1 and currency.code=$13
				and ($12='disabled' or currency.status='active')
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
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit shop item")
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
		item.Translations, err = decodeStoredJSONObject(translations, "currency translations")
		if err != nil {
			return nil, fmt.Errorf("currency %s: %w", item.PublicID, err)
		}
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
		translations, decodeErr := decodeStoredJSONObject(translationsRaw, "shop item translations")
		if decodeErr != nil {
			return nil, fmt.Errorf("shop item %s: %w", publicID, decodeErr)
		}
		config, decodeErr := decodeStoredJSONObject(configRaw, "shop item configuration")
		if decodeErr != nil {
			return nil, fmt.Errorf("shop item %s: %w", publicID, decodeErr)
		}
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

type economyConfigReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadEconomyConfig(ctx context.Context, reader economyConfigReader) (economyConfigPayload, error) {
	payload := defaultEconomyConfig()
	var raw []byte
	err := reader.QueryRow(ctx, `select value from system_settings where key=$1`, economyConfigSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return payload, err
	}
	if _, err = decodeStoredJSONObject(raw, "economy configuration"); err != nil {
		return payload, err
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return payload, err
	}
	return normalizeEconomyConfig(payload)
}

func (s *Server) economyConfigFromSettings(ctx context.Context) (economyConfigPayload, error) {
	return loadEconomyConfig(ctx, s.db)
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
	if payload.Checkin.MinimumHours > 72 || payload.Checkin.Amount < 0 || payload.Checkin.Amount > maxCurrencyAmount {
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
		if reward.ObjectType == "" || reward.Currency == "" || reward.DownloadsPerReward <= 0 ||
			reward.DownloadsPerReward > maxCurrencyBalance || reward.Amount <= 0 || reward.Amount > maxCurrencyAmount {
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

func lockEconomyConfigurationTx(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext('mcmods-cn-economy-configuration'))`)
	return err
}

func referencedEconomyCurrencyCodes(payload economyConfigPayload) []string {
	result := make([]string, 0, 1+len(payload.DownloadRewards))
	if payload.Checkin.Enabled {
		result = append(result, payload.Checkin.Currency)
	}
	for _, reward := range payload.DownloadRewards {
		result = appendUniqueString(result, reward.Currency)
	}
	return result
}

func validateActiveEconomyCurrencyReferencesTx(ctx context.Context, tx pgx.Tx, payload economyConfigPayload) error {
	for _, code := range referencedEconomyCurrencyCodes(payload) {
		var active bool
		if err := tx.QueryRow(ctx, `select exists(
			select 1 from currencies where code=$1 and status='active'
		)`, code).Scan(&active); err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("%w: %s", errInactiveEconomyCurrency, code)
		}
	}
	return nil
}

func economyCurrencyReferenceBlockersTx(ctx context.Context, tx pgx.Tx, code string) ([]string, error) {
	payload, err := loadEconomyConfig(ctx, tx)
	if err != nil {
		return nil, err
	}
	blockers := make([]string, 0, 3)
	if payload.Checkin.Enabled && payload.Checkin.Currency == code {
		blockers = append(blockers, "enabled check-in")
	}
	for _, reward := range payload.DownloadRewards {
		if reward.Currency == code {
			blockers = append(blockers, "download reward rules")
			break
		}
	}
	var activeShopItems bool
	if err = tx.QueryRow(ctx, `select exists(
		select 1 from shop_items item
		join currencies currency on currency.id=item.price_currency_id
		where currency.code=$1 and item.status='active'
	)`, code).Scan(&activeShopItems); err != nil {
		return nil, err
	}
	if activeShopItems {
		blockers = append(blockers, "active shop items")
	}
	return blockers, nil
}

func loadLocation(name string) *time.Location {
	location, err := time.LoadLocation(strings.TrimSpace(name))
	if err != nil {
		return time.UTC
	}
	return location
}

func (s *Server) nextCheckinState(ctx context.Context, userID int64, timezone string, minimumHours int) (*time.Time, bool, error) {
	var claimedAt time.Time
	var localDate string
	err := s.db.QueryRow(ctx, `select claimed_at,local_date::text from user_checkins
		where user_id=$1 order by claimed_at desc limit 1`, userID).Scan(&claimedAt, &localDate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC()
	checkedInToday := localDate == now.In(loadLocation(timezone)).Format("2006-01-02")
	eligible := claimedAt.Add(time.Duration(minimumHours) * time.Hour)
	return &eligible, checkedInToday, nil
}

var (
	errInsufficientBalance = errors.New("insufficient balance")
	errCurrencyOutOfRange  = errors.New("currency amount is outside the supported range")
)

func calculateTransferAmounts(amount int64, taxBPS int) (int64, int64, error) {
	if amount <= 0 || amount > maxCurrencyAmount || taxBPS < 0 || taxBPS > 10_000 {
		return 0, 0, errCurrencyOutOfRange
	}
	basisPoints := int64(taxBPS)
	tax := (amount / 10_000) * basisPoints
	remainderProduct := (amount % 10_000) * basisPoints
	if remainderProduct > 0 {
		tax += (remainderProduct + 9_999) / 10_000
	}
	received := amount - tax
	if tax < 0 || received <= 0 || received+tax != amount {
		return 0, 0, errCurrencyOutOfRange
	}
	return tax, received, nil
}

func calculateShopTotal(unitPrice int64, quantity int) (int64, error) {
	if unitPrice < 0 || unitPrice > maxCurrencyAmount || quantity <= 0 || quantity > maxShopPurchaseQuantity {
		return 0, errCurrencyOutOfRange
	}
	return checkedNonNegativeCurrencyProduct(unitPrice, int64(quantity))
}

func checkedNonNegativeCurrencyProduct(left, right int64) (int64, error) {
	if left < 0 || right < 0 || (right > 0 && left > maxCurrencyBalance/right) {
		return 0, errCurrencyOutOfRange
	}
	product := left * right
	if product > maxCurrencyBalance {
		return 0, errCurrencyOutOfRange
	}
	return product, nil
}

func checkedCurrencyBalance(current, delta int64) (int64, error) {
	if current < 0 || current > maxCurrencyBalance {
		return current, errCurrencyOutOfRange
	}
	if delta < -current {
		return current, errInsufficientBalance
	}
	if delta > maxCurrencyBalance-current {
		return current, errCurrencyOutOfRange
	}
	return current + delta, nil
}

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

func (s *Server) recordOwnedContentDownload(
	ctx context.Context,
	objectType, objectPublicID string,
	ownerID, downloaderID int64,
) error {
	if ownerID <= 0 || ownerID == downloaderID {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockEconomyConfigurationTx(ctx, tx); err != nil {
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
	return tx.Commit(ctx)
}
