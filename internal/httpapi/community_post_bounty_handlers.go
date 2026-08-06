package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

func holdCommunityPostBountyTx(ctx context.Context, tx pgx.Tx, postID int64, publicID string, authorID int64, snapshot communityPostSnapshot) error {
	if snapshot.Kind != "discussion" || snapshot.BountyAmount == 0 {
		return nil
	}
	var currencyID int64
	if err := tx.QueryRow(ctx, `select id from currencies where code=$1 and status='active'`, snapshot.BountyCurrency).Scan(&currencyID); err != nil {
		return errors.New("question bounty currency is unavailable")
	}
	if _, err := changeCurrencyBalanceByIDTx(ctx, tx, authorID, currencyID, -snapshot.BountyAmount,
		"question_bounty_hold", nil, "community_post_bounty", publicID, map[string]any{"amount": snapshot.BountyAmount}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `insert into community_post_bounties(post_id,currency_id,amount) values($1,$2,$3)`, postID, currencyID, snapshot.BountyAmount)
	return err
}

func validateCommunityPostBountyEditTx(ctx context.Context, tx pgx.Tx, postID int64, publicID string, authorID int64, snapshot communityPostSnapshot) error {
	var currencyID, amount int64
	var currency, status, resolution string
	err := tx.QueryRow(ctx, `select bounty.currency_id,currency.code,bounty.amount,bounty.status,post.resolution_status
		from community_post_bounties bounty join currencies currency on currency.id=bounty.currency_id
		join community_posts post on post.id=bounty.post_id
		where bounty.post_id=$1 for update of bounty`, postID).Scan(&currencyID, &currency, &amount, &status, &resolution)
	if errors.Is(err, pgx.ErrNoRows) {
		if snapshot.BountyCurrency != "" || snapshot.BountyAmount != 0 {
			return errors.New("a question bounty can only be selected when the question is created")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if snapshot.Kind != "discussion" || snapshot.BountyCurrency != currency || snapshot.BountyAmount != amount {
		return errors.New("an existing question bounty cannot be changed")
	}
	if status == "refunded" && resolution == "open" {
		if _, err = changeCurrencyBalanceByIDTx(ctx, tx, authorID, currencyID, -amount,
			"question_bounty_hold", nil, "community_post_bounty", publicID, map[string]any{"amount": amount, "resubmitted": true}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `update community_post_bounties set status='held',recipient_id=null,tax_amount=0,
			net_amount=0,settled_at=null where post_id=$1`, postID)
	}
	return err
}

func refundCommunityPostBountyTx(ctx context.Context, tx pgx.Tx, postID, authorID int64, publicID, reason string) error {
	var currencyID, amount int64
	err := tx.QueryRow(ctx, `select currency_id,amount from community_post_bounties where post_id=$1 and status='held' for update`, postID).
		Scan(&currencyID, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = changeCurrencyBalanceByIDTx(ctx, tx, authorID, currencyID, amount,
		"question_bounty_refund", nil, "community_post_bounty", publicID, map[string]any{"amount": amount, "reason": reason}); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `update community_post_bounties set status='refunded',settled_at=now() where post_id=$1`, postID)
	return err
}

func (s *Server) loadCommunityPostBounty(ctx context.Context, postID int64) (*communityPostBounty, error) {
	values, err := s.loadCommunityPostBounties(ctx, []int64{postID})
	return values[postID], err
}

func (s *Server) loadCommunityPostBounties(ctx context.Context, postIDs []int64) (map[int64]*communityPostBounty, error) {
	result := make(map[int64]*communityPostBounty, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}
	rows, err := s.db.Query(ctx, `select bounty.post_id,currency.code,currency.name,currency.icon,currency.translations,
		bounty.amount,bounty.status,bounty.tax_amount,bounty.net_amount
		from community_post_bounties bounty join currencies currency on currency.id=bounty.currency_id
		where bounty.post_id=any($1::bigint[])`, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var postID int64
		var bounty communityPostBounty
		var translations []byte
		if err = rows.Scan(&postID, &bounty.Currency, &bounty.CurrencyName, &bounty.CurrencyIcon, &translations,
			&bounty.Amount, &bounty.Status, &bounty.TaxAmount, &bounty.NetAmount); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(translations, &bounty.Translations)
		if bounty.Translations == nil {
			bounty.Translations = map[string]any{}
		}
		result[postID] = &bounty
	}
	return result, rows.Err()
}

func (s *Server) acceptCommunityPostAnswer(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	commentPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("commentId")))
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start answer settlement")
		return
	}
	defer tx.Rollback(r.Context())
	var postID, authorID int64
	var resolution, title string
	err = tx.QueryRow(r.Context(), `select id,author_id,resolution_status,title from community_posts
		where public_id=$1 and kind='discussion' and status='active' and review_status='approved' for update`, publicID).
		Scan(&postID, &authorID, &resolution, &title)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "question was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load question")
		return
	}
	if claims.Subject != authorID {
		writeError(w, http.StatusForbidden, "only the question author can accept an answer")
		return
	}
	if resolution != "open" {
		writeError(w, http.StatusConflict, "question has already been resolved")
		return
	}
	var commentID, recipientID int64
	err = tx.QueryRow(r.Context(), `select id,author_id from comments where public_id=$1 and target_type='community_post'
		and target_id=$2 and target_version_id is null and status='published' and deleted_at is null for update`, commentPublicID, postID).
		Scan(&commentID, &recipientID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "answer was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load answer")
		return
	}
	if recipientID == authorID {
		writeError(w, http.StatusBadRequest, "use self-solved for your own solution")
		return
	}
	var currencyID, amount int64
	var bountyStatus string
	var taxBPS int
	err = tx.QueryRow(r.Context(), `select bounty.currency_id,bounty.amount,bounty.status,currency.transfer_tax_bps
		from community_post_bounties bounty join currencies currency on currency.id=bounty.currency_id
		where bounty.post_id=$1 for update of bounty`, postID).Scan(&currencyID, &amount, &bountyStatus, &taxBPS)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load question bounty")
		return
	}
	tax, received := int64(0), int64(0)
	if err == nil {
		if bountyStatus != "held" {
			writeError(w, http.StatusConflict, "question bounty is no longer available")
			return
		}
		tax = (amount*int64(taxBPS) + 9999) / 10000
		received = amount - tax
		if received <= 0 {
			writeError(w, http.StatusConflict, "question bounty is too small after tax")
			return
		}
		if _, err = changeCurrencyBalanceByIDTx(r.Context(), tx, recipientID, currencyID, received,
			"question_bounty_award", &authorID, "community_post_bounty", publicID,
			map[string]any{"gross": amount, "tax": tax, "commentId": commentPublicID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to award question bounty")
			return
		}
		if _, err = tx.Exec(r.Context(), `update community_post_bounties set status='awarded',recipient_id=$2,
			tax_amount=$3,net_amount=$4,settled_at=now() where post_id=$1`, postID, recipientID, tax, received); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to settle question bounty")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `update community_posts set resolution_status='answered',accepted_comment_id=$2,
		resolved_at=now(),updated_at=now() where id=$1`, postID, commentID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve question")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit answer settlement")
		return
	}
	s.sendTemplatedNotification(r.Context(), recipientID, "question_answer_accepted", map[string]string{"name": title}, map[string]any{
		"communityPostId": publicID, "commentId": commentPublicID, "targetLabel": title,
		"url": communityPostPath("discussion", publicID) + "#comment-" + commentPublicID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"resolutionStatus": "answered", "acceptedCommentId": commentPublicID, "taxAmount": tax, "netAmount": received})
}

func (s *Server) selfSolveCommunityPost(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start question resolution")
		return
	}
	defer tx.Rollback(r.Context())
	var postID, authorID int64
	var resolution string
	err = tx.QueryRow(r.Context(), `select id,author_id,resolution_status from community_posts
		where public_id=$1 and kind='discussion' and status='active' and review_status='approved' for update`, publicID).
		Scan(&postID, &authorID, &resolution)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "question was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load question")
		return
	}
	if claims.Subject != authorID {
		writeError(w, http.StatusForbidden, "only the question author can resolve it")
		return
	}
	if resolution != "open" {
		writeError(w, http.StatusConflict, "question has already been resolved")
		return
	}
	if err = refundCommunityPostBountyTx(r.Context(), tx, postID, authorID, publicID, "self_solved"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to refund question bounty")
		return
	}
	if _, err = tx.Exec(r.Context(), `update community_posts set resolution_status='self_solved',accepted_comment_id=null,
		resolved_at=now(),updated_at=now() where id=$1`, postID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve question")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit question resolution")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resolutionStatus": "self_solved"})
}
