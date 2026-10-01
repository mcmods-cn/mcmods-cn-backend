package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type minecraftServerProofFile struct {
	ID           string `json:"id"`
	OriginalName string `json:"originalName"`
	SizeBytes    int64  `json:"sizeBytes"`
}

type minecraftServerReviewItem struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Address           string                     `json:"address"`
	ShortDescription  string                     `json:"shortDescription"`
	BodyMarkdown      string                     `json:"bodyMarkdown"`
	MinecraftVersions []string                   `json:"minecraftVersions"`
	DedicatedClient   bool                       `json:"dedicatedClient"`
	Languages         []string                   `json:"languages"`
	PrimaryTag        string                     `json:"primaryTag"`
	HasWhitelist      bool                       `json:"hasWhitelist"`
	OnlineMode        bool                       `json:"onlineMode"`
	Modded            bool                       `json:"modded"`
	Loader            string                     `json:"loader"`
	ProofText         string                     `json:"proofText"`
	ReviewStatus      string                     `json:"reviewStatus"`
	ReviewNote        string                     `json:"reviewNote"`
	SubmitterID       string                     `json:"submitterId"`
	SubmitterUsername string                     `json:"submitterUsername"`
	ProofFiles        []minecraftServerProofFile `json:"proofFiles"`
	Links             []minecraftServerLink      `json:"links"`
	Mods              []minecraftServerMod       `json:"mods"`
	CreatedAt         time.Time                  `json:"createdAt"`
	ReviewedAt        *time.Time                 `json:"reviewedAt,omitempty"`
}

const minecraftServerReviewListQuery = `select server.public_id,server.name,server.address,
	server.short_description,server.body_markdown,server.minecraft_versions,
	server.dedicated_client,server.languages,server.primary_tag,server.has_whitelist,
	server.online_mode,server.modded,server.loader,server.proof_text,server.review_status,
	server.review_note,submitter.public_id,submitter.username,server.created_at,
	server.reviewed_at
	from minecraft_servers server join users submitter on submitter.id=server.submitted_by
	where server.review_status=$1 order by server.created_at,server.id limit 200`

func (s *Server) adminMinecraftServerReviews(w http.ResponseWriter, r *http.Request) {
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" {
		writeError(w, http.StatusBadRequest, "审核状态不正确")
		return
	}
	rows, err := s.db.Query(r.Context(), minecraftServerReviewListQuery, status)
	if err != nil {
		log.Printf("load minecraft server review list status=%s: %v", status, err)
		writeError(w, http.StatusInternalServerError, "读取服务器审核列表失败")
		return
	}
	defer rows.Close()
	items := make([]minecraftServerReviewItem, 0)
	for rows.Next() {
		var item minecraftServerReviewItem
		if err = rows.Scan(&item.ID, &item.Name, &item.Address, &item.ShortDescription,
			&item.BodyMarkdown, &item.MinecraftVersions, &item.DedicatedClient,
			&item.Languages, &item.PrimaryTag, &item.HasWhitelist, &item.OnlineMode,
			&item.Modded, &item.Loader, &item.ProofText, &item.ReviewStatus,
			&item.ReviewNote, &item.SubmitterID, &item.SubmitterUsername,
			&item.CreatedAt, &item.ReviewedAt); err != nil {
			log.Printf("scan minecraft server review list status=%s: %v", status, err)
			writeError(w, http.StatusInternalServerError, "解析服务器审核列表失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		log.Printf("iterate minecraft server review list status=%s: %v", status, err)
		writeError(w, http.StatusInternalServerError, "读取服务器审核列表失败")
		return
	}
	// All association queries share the same pool. Release the list connection
	// before reading them, including when the pool has just one connection.
	rows.Close()
	for index := range items {
		item := &items[index]
		item.ProofFiles, err = s.minecraftServerProofFiles(r.Context(), item.ID)
		if err == nil {
			item.Links, err = s.minecraftServerLinks(r.Context(), item.ID)
		}
		if err == nil {
			item.Mods, err = s.minecraftServerMods(r.Context(), item.ID)
		}
		if err != nil {
			log.Printf("load minecraft server review details server=%s: %v", item.ID, err)
			writeError(w, http.StatusInternalServerError, "读取服务器审核资料失败")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) minecraftServerProofFiles(ctx context.Context, publicID string) ([]minecraftServerProofFile, error) {
	rows, err := s.db.Query(ctx, `select file.public_id,file.original_name,
		greatest(file.size_bytes,file.source_size_bytes)
		from minecraft_server_proof_files proof
		join minecraft_servers server on server.id=proof.server_id
		join oss_files file on file.id=proof.oss_file_id
		where server.public_id=$1 and `+safeReviewAttachmentPredicate+`
		order by proof.display_order,file.id`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]minecraftServerProofFile, 0)
	for rows.Next() {
		var item minecraftServerProofFile
		if err = rows.Scan(&item.ID, &item.OriginalName, &item.SizeBytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) reviewMinecraftServer(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("serverId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "服务器编号不正确")
		return
	}
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "approved" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "审核状态不正确")
		return
	}
	if request.Status == "rejected" && request.Note == "" {
		writeError(w, http.StatusBadRequest, "拒绝服务器时必须填写原因")
		return
	}
	if len(request.Note) > 4000 {
		writeError(w, http.StatusBadRequest, "审核说明过长")
		return
	}
	var submitterID int64
	var name string
	err := s.db.QueryRow(r.Context(), `update minecraft_servers set
		review_status=$2,review_note=$3,reviewed_by=$4,reviewed_at=now(),
		published_at=case when $2='approved' then coalesce(published_at,now()) else published_at end,
		next_probe_at=case when $2='approved' then now() else next_probe_at end,
		updated_at=now()
		where public_id=$1 and review_status='pending'
		returning submitted_by,name`, publicID, request.Status, request.Note,
		currentClaims(r).Subject).Scan(&submitterID, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "服务器不存在或已经审核")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存服务器审核结果失败")
		return
	}
	templateCode := "review_approved"
	if request.Status == "rejected" {
		templateCode = "review_rejected"
	}
	s.sendTemplatedNotification(r.Context(), submitterID, templateCode, map[string]string{
		"name": name, "reason": request.Note,
	}, map[string]any{
		"serverId": publicID, "reviewStatus": request.Status,
		"targetLabel": name, "url": "/servers/" + publicID,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": request.Status})
}

func (s *Server) presignMinecraftServerProof(w http.ResponseWriter, r *http.Request) {
	serverID := strings.ToLower(strings.TrimSpace(r.PathValue("serverId")))
	fileID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	if !validCatalogPublicID(serverID) || !validCatalogPublicID(fileID) {
		writeError(w, http.StatusBadRequest, "服务器或附件编号不正确")
		return
	}
	file, err := lookupReviewAttachment(r.Context(), s.db, reviewAttachmentLookup{
		Kind: reviewAttachmentForMinecraftServer, SubjectPublicID: serverID,
	}, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "证明附件不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取证明附件失败")
		return
	}
	s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: file.ObjectKey})
}
