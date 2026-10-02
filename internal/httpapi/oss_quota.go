package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const maxOSSUserQuotaReservations = 64

type ossUserQuotaLimits struct {
	single int64
	daily  int64
	total  int64
}

type ossUserQuotaSnapshot struct {
	activeSource   int64
	activeStored   int64
	reservedSource int64
	reservedStored int64
}

type ossUserQuotaReservation struct {
	usageDate   time.Time
	sourceBytes int64
	storedBytes int64
}

type ossUserQuotaError struct {
	message string
}

func (err *ossUserQuotaError) Error() string { return err.message }

func newOSSUserQuotaError(message string) error {
	return &ossUserQuotaError{message: message}
}

func writeOSSUserQuotaError(w http.ResponseWriter, err error) {
	var quotaErr *ossUserQuotaError
	if errors.As(err, &quotaErr) {
		writeError(w, http.StatusForbidden, quotaErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "检查用户文件额度失败")
}

func ossUserQuotaLimitsForRequest(r *http.Request) (ossUserQuotaLimits, error) {
	claims := currentClaims(r)
	limits := ossUserQuotaLimits{
		single: permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.single_limit")),
		daily:  permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.daily_limit")),
		total:  permissionMiBToBytes(claimsNumericPermissionValue(claims, "user.file.total_limit")),
	}
	if limits.single <= 0 {
		return ossUserQuotaLimits{}, newOSSUserQuotaError("没有单文件上传权限")
	}
	if limits.daily <= 0 {
		return ossUserQuotaLimits{}, newOSSUserQuotaError("没有每日上传额度")
	}
	if limits.total <= 0 {
		return ossUserQuotaLimits{}, newOSSUserQuotaError("没有用户文件总容量额度")
	}
	return limits, nil
}

func (limits ossUserQuotaLimits) validateSingle(sizeBytes int64) error {
	if sizeBytes <= 0 {
		return newOSSUserQuotaError("上传文件大小不正确")
	}
	if limits.single != maxPermissionBytes && sizeBytes > limits.single {
		return newOSSUserQuotaError(fmt.Sprintf("文件超过单文件大小限制：%s", formatLimitBytes(limits.single)))
	}
	return nil
}

func quotaAllows(active, reserved, requested, limit int64) bool {
	if active < 0 || reserved < 0 || requested < 0 {
		return false
	}
	if limit == maxPermissionBytes {
		return true
	}
	return requested <= limit && active <= limit-requested && reserved <= limit-requested-active
}

func (s *Server) loadOSSUserQuotaAdmissionSnapshots(ctx context.Context, userID int64) (ossUserQuotaSnapshot, ossUserQuotaSnapshot, error) {
	var total, daily ossUserQuotaSnapshot
	if err := s.db.QueryRow(ctx, `select active_source_bytes,active_stored_bytes,reserved_source_bytes,reserved_stored_bytes
		from oss_user_quota_usage where user_id=$1`, userID).Scan(
		&total.activeSource, &total.activeStored, &total.reservedSource, &total.reservedStored,
	); errors.Is(err, pgx.ErrNoRows) {
		total = ossUserQuotaSnapshot{}
	} else if err != nil {
		return ossUserQuotaSnapshot{}, ossUserQuotaSnapshot{}, err
	}
	if err := s.db.QueryRow(ctx, `select active_source_bytes,active_stored_bytes,reserved_source_bytes,reserved_stored_bytes
		from oss_user_daily_quota_usage where user_id=$1 and usage_date=current_date`, userID).Scan(
		&daily.activeSource, &daily.activeStored, &daily.reservedSource, &daily.reservedStored,
	); errors.Is(err, pgx.ErrNoRows) {
		daily = ossUserQuotaSnapshot{}
	} else if err != nil {
		return ossUserQuotaSnapshot{}, ossUserQuotaSnapshot{}, err
	}
	return total, daily, nil
}

func (s *Server) reserveUserOSSUploadQuota(
	ctx context.Context,
	userID int64,
	objectKey string,
	sourceBytes, storedBytes int64,
	expiresAt time.Time,
	limits ossUserQuotaLimits,
) error {
	if userID <= 0 || objectKey == "" || sourceBytes <= 0 || storedBytes < 0 || !expiresAt.After(time.Now()) {
		return errors.New("上传额度预留参数不正确")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	usageDate, err := lockOSSUserQuotaTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if err = cleanupExpiredOSSUserQuotaReservationsTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err = releaseOSSUserQuotaReservationTx(ctx, tx, userID, objectKey); err != nil {
		return err
	}
	var reservationCount int
	if err = tx.QueryRow(ctx, `select count(*) from (
		select 1 from oss_user_upload_quota_reservations where user_id=$1 order by expires_at,object_key limit $2
	) active`, userID, maxOSSUserQuotaReservations).Scan(&reservationCount); err != nil {
		return err
	}
	if reservationCount >= maxOSSUserQuotaReservations {
		return newOSSUserQuotaError("并发上传预留数量已达上限")
	}
	total, daily, err := loadLockedOSSUserQuotaSnapshotsTx(ctx, tx, userID, usageDate)
	if err != nil {
		return err
	}
	if !quotaAllows(daily.activeSource, daily.reservedSource, sourceBytes, limits.daily) {
		return newOSSUserQuotaError(fmt.Sprintf("超过每日上传额度：%s", formatLimitBytes(limits.daily)))
	}
	if !quotaAllows(total.activeStored, total.reservedStored, storedBytes, limits.total) {
		return newOSSUserQuotaError(fmt.Sprintf("超过用户文件总容量：%s", formatLimitBytes(limits.total)))
	}
	if _, err = tx.Exec(ctx, `insert into oss_user_upload_quota_reservations
		(user_id,object_key,usage_date,source_bytes,stored_bytes,expires_at)
		values($1,$2,$3,$4,$5,$6)`, userID, objectKey, usageDate, sourceBytes, storedBytes, expiresAt); err != nil {
		return err
	}
	if err = adjustOSSUserReservedQuotaTx(ctx, tx, userID,
		ossUserQuotaReservation{usageDate: usageDate, sourceBytes: sourceBytes, storedBytes: storedBytes}, 1); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) releaseUserOSSUploadQuotaReservation(ctx context.Context, userID int64, objectKey string) error {
	if userID <= 0 || objectKey == "" {
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = lockOSSUserQuotaTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err = releaseOSSUserQuotaReservationTx(ctx, tx, userID, objectKey); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func settleUserOSSUploadQuotaTx(
	ctx context.Context,
	tx pgx.Tx,
	userID int64,
	reservationObjectKey string,
	sourceBytes, storedBytes int64,
	limits ossUserQuotaLimits,
) error {
	if userID <= 0 || sourceBytes <= 0 || storedBytes < 0 {
		return errors.New("上传额度结算参数不正确")
	}
	usageDate, err := lockOSSUserQuotaTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if err = cleanupExpiredOSSUserQuotaReservationsTx(ctx, tx, userID); err != nil {
		return err
	}
	if _, err = releaseOSSUserQuotaReservationTx(ctx, tx, userID, reservationObjectKey); err != nil {
		return err
	}
	total, daily, err := loadLockedOSSUserQuotaSnapshotsTx(ctx, tx, userID, usageDate)
	if err != nil {
		return err
	}
	if !quotaAllows(daily.activeSource, daily.reservedSource, sourceBytes, limits.daily) {
		return newOSSUserQuotaError(fmt.Sprintf("超过每日上传额度：%s", formatLimitBytes(limits.daily)))
	}
	if !quotaAllows(total.activeStored, total.reservedStored, storedBytes, limits.total) {
		return newOSSUserQuotaError(fmt.Sprintf("超过用户文件总容量：%s", formatLimitBytes(limits.total)))
	}
	return nil
}

func lockOSSUserQuotaTx(ctx context.Context, tx pgx.Tx, userID int64) (time.Time, error) {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('oss-user-quota:'||$1::bigint::text,0))`, userID); err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `insert into oss_user_quota_usage(user_id) values($1) on conflict do nothing`, userID); err != nil {
		return time.Time{}, err
	}
	var usageDate time.Time
	if err := tx.QueryRow(ctx, `select current_date`).Scan(&usageDate); err != nil {
		return time.Time{}, err
	}
	if _, err := tx.Exec(ctx, `insert into oss_user_daily_quota_usage(user_id,usage_date) values($1,$2) on conflict do nothing`, userID, usageDate); err != nil {
		return time.Time{}, err
	}
	return usageDate, nil
}

func cleanupExpiredOSSUserQuotaReservationsTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `delete from oss_user_upload_quota_reservations
		where user_id=$1 and expires_at<=now()
		returning usage_date,source_bytes,stored_bytes`, userID)
	if err != nil {
		return err
	}
	defer rows.Close()
	reservations := make([]ossUserQuotaReservation, 0)
	for rows.Next() {
		var reservation ossUserQuotaReservation
		if err = rows.Scan(&reservation.usageDate, &reservation.sourceBytes, &reservation.storedBytes); err != nil {
			return err
		}
		reservations = append(reservations, reservation)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, reservation := range reservations {
		if err = adjustOSSUserReservedQuotaTx(ctx, tx, userID, reservation, -1); err != nil {
			return err
		}
	}
	return nil
}

func releaseOSSUserQuotaReservationTx(ctx context.Context, tx pgx.Tx, userID int64, objectKey string) (bool, error) {
	if objectKey == "" {
		return false, nil
	}
	var reservation ossUserQuotaReservation
	err := tx.QueryRow(ctx, `delete from oss_user_upload_quota_reservations
		where user_id=$1 and object_key=$2
		returning usage_date,source_bytes,stored_bytes`, userID, objectKey).
		Scan(&reservation.usageDate, &reservation.sourceBytes, &reservation.storedBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = adjustOSSUserReservedQuotaTx(ctx, tx, userID, reservation, -1); err != nil {
		return false, err
	}
	return true, nil
}

func adjustOSSUserReservedQuotaTx(ctx context.Context, tx pgx.Tx, userID int64, reservation ossUserQuotaReservation, direction int64) error {
	sourceDelta := reservation.sourceBytes * direction
	storedDelta := reservation.storedBytes * direction
	tag, err := tx.Exec(ctx, `update oss_user_quota_usage set
		reserved_source_bytes=reserved_source_bytes+$2,
		reserved_stored_bytes=reserved_stored_bytes+$3,updated_at=now()
		where user_id=$1 and reserved_source_bytes+$2>=0 and reserved_stored_bytes+$3>=0`,
		userID, sourceDelta, storedDelta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("OSS user reserved quota counter is inconsistent")
	}
	if _, err = tx.Exec(ctx, `insert into oss_user_daily_quota_usage(user_id,usage_date) values($1,$2) on conflict do nothing`, userID, reservation.usageDate); err != nil {
		return err
	}
	tag, err = tx.Exec(ctx, `update oss_user_daily_quota_usage set
		reserved_source_bytes=reserved_source_bytes+$3,
		reserved_stored_bytes=reserved_stored_bytes+$4,updated_at=now()
		where user_id=$1 and usage_date=$2
		  and reserved_source_bytes+$3>=0 and reserved_stored_bytes+$4>=0`,
		userID, reservation.usageDate, sourceDelta, storedDelta)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("OSS user daily reserved quota counter is inconsistent")
	}
	return nil
}

func loadLockedOSSUserQuotaSnapshotsTx(ctx context.Context, tx pgx.Tx, userID int64, usageDate time.Time) (ossUserQuotaSnapshot, ossUserQuotaSnapshot, error) {
	var total, daily ossUserQuotaSnapshot
	err := tx.QueryRow(ctx, `select
		total.active_source_bytes,total.active_stored_bytes,total.reserved_source_bytes,total.reserved_stored_bytes,
		daily.active_source_bytes,daily.active_stored_bytes,daily.reserved_source_bytes,daily.reserved_stored_bytes
		from oss_user_quota_usage total
		join oss_user_daily_quota_usage daily on daily.user_id=total.user_id and daily.usage_date=$2
		where total.user_id=$1 for update of total,daily`, userID, usageDate).Scan(
		&total.activeSource, &total.activeStored, &total.reservedSource, &total.reservedStored,
		&daily.activeSource, &daily.activeStored, &daily.reservedSource, &daily.reservedStored,
	)
	return total, daily, err
}
