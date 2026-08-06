package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/serverprobe"
)

const serverSettingsKey = "server_catalog"

type serverCatalogSettings struct {
	MaxProofFiles      int   `json:"maxProofFiles"`
	MaxProofTotalBytes int64 `json:"maxProofTotalBytes"`
	NameMaxLength      int   `json:"nameMaxLength"`
	SummaryMaxLength   int   `json:"summaryMaxLength"`
	HistoryDays        int   `json:"historyDays"`
}

type serverSubmissionSettings struct {
	serverCatalogSettings
	ReviewRequired bool `json:"reviewRequired"`
}

type createServerLinkRequest struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type createServerModRequest struct {
	ID         string `json:"id"`
	Version    string `json:"version"`
	Source     string `json:"source"`
	Confidence string `json:"confidence"`
}

type createMinecraftServerRequest struct {
	Address           string                    `json:"address"`
	Name              string                    `json:"name"`
	ShortDescription  string                    `json:"shortDescription"`
	BodyMarkdown      string                    `json:"bodyMarkdown"`
	MinecraftVersions []string                  `json:"minecraftVersions"`
	DedicatedClient   bool                      `json:"dedicatedClient"`
	Languages         []string                  `json:"languages"`
	PrimaryTag        string                    `json:"primaryTag"`
	HasWhitelist      bool                      `json:"hasWhitelist"`
	OnlineMode        bool                      `json:"onlineMode"`
	Links             []createServerLinkRequest `json:"links"`
	Mods              []createServerModRequest  `json:"mods"`
	ProofText         string                    `json:"proofText"`
	ProofFileIDs      []string                  `json:"proofFileIds"`
}

type updateMinecraftServerRequest struct {
	Name              string                    `json:"name"`
	ShortDescription  string                    `json:"shortDescription"`
	BodyMarkdown      string                    `json:"bodyMarkdown"`
	MinecraftVersions []string                  `json:"minecraftVersions"`
	DedicatedClient   bool                      `json:"dedicatedClient"`
	Languages         []string                  `json:"languages"`
	PrimaryTag        string                    `json:"primaryTag"`
	HasWhitelist      bool                      `json:"hasWhitelist"`
	OnlineMode        bool                      `json:"onlineMode"`
	Links             []createServerLinkRequest `json:"links"`
	Mods              []createServerModRequest  `json:"mods"`
}

type minecraftServerListItem struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ShortDescription  string     `json:"shortDescription"`
	IconDataURI       string     `json:"iconDataUri,omitempty"`
	Modded            bool       `json:"modded"`
	Loader            string     `json:"loader,omitempty"`
	Languages         []string   `json:"languages"`
	PrimaryTag        string     `json:"primaryTag"`
	MinecraftVersions []string   `json:"minecraftVersions"`
	Online            bool       `json:"online"`
	LatencyMS         *int       `json:"latencyMs,omitempty"`
	PlayersOnline     int        `json:"playersOnline"`
	PlayersMax        int        `json:"playersMax"`
	LastCheckedAt     *time.Time `json:"lastCheckedAt,omitempty"`
}

type minecraftServerLink struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type minecraftServerMod struct {
	ID          string `json:"id"`
	Version     string `json:"version,omitempty"`
	Source      string `json:"source"`
	Confidence  string `json:"confidence"`
	Resolved    bool   `json:"resolved"`
	ModPublicID string `json:"modPublicId,omitempty"`
	ModID       string `json:"modId,omitempty"`
	ModName     string `json:"modName,omitempty"`
	ModSlug     string `json:"modSlug,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
}

type minecraftServerDetail struct {
	minecraftServerListItem
	Address          string                `json:"address"`
	BodyMarkdown     string                `json:"bodyMarkdown"`
	DedicatedClient  bool                  `json:"dedicatedClient"`
	HasWhitelist     bool                  `json:"hasWhitelist"`
	OnlineMode       bool                  `json:"onlineMode"`
	MOTD             string                `json:"motd"`
	MinecraftVersion string                `json:"minecraftVersion"`
	Protocol         *int                  `json:"protocol,omitempty"`
	ModListComplete  bool                  `json:"modListComplete"`
	Links            []minecraftServerLink `json:"links"`
	Mods             []minecraftServerMod  `json:"mods"`
	ReviewStatus     string                `json:"reviewStatus"`
	CreatedAt        time.Time             `json:"createdAt"`
	UpdatedAt        time.Time             `json:"updatedAt"`
	CanEdit          bool                  `json:"canEdit"`
}

func defaultServerCatalogSettings() serverCatalogSettings {
	return serverCatalogSettings{
		MaxProofFiles:      5,
		MaxProofTotalBytes: 10 << 20,
		NameMaxLength:      80,
		SummaryMaxLength:   240,
		HistoryDays:        90,
	}
}

func loadServerCatalogSettings(ctx context.Context, query revisionQuery) serverCatalogSettings {
	settings := defaultServerCatalogSettings()
	var raw []byte
	if query.QueryRow(ctx, `select value from system_settings where key=$1`, serverSettingsKey).Scan(&raw) == nil {
		_ = json.Unmarshal(raw, &settings)
	}
	settings.MaxProofFiles = min(max(settings.MaxProofFiles, 1), 20)
	settings.MaxProofTotalBytes = min(max(settings.MaxProofTotalBytes, 1<<20), int64(100<<20))
	settings.NameMaxLength = min(max(settings.NameMaxLength, 20), 160)
	settings.SummaryMaxLength = min(max(settings.SummaryMaxLength, 80), 1000)
	settings.HistoryDays = min(max(settings.HistoryDays, 7), 90)
	return settings
}

func (s *Server) publicServerSettings(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	reviewRequired := loadReviewConfig(r.Context(), s.db).ServerCreate &&
		!claimsAllow(claims, "server.create.no-review") &&
		!claimsAllow(claims, "admin.*")
	writeJSON(w, http.StatusOK, serverSubmissionSettings{
		serverCatalogSettings: loadServerCatalogSettings(r.Context(), s.db),
		ReviewRequired:        reviewRequired,
	})
}

func (s *Server) adminServerSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, loadServerCatalogSettings(r.Context(), s.db))
		return
	}
	var request serverCatalogSettings
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if request.MaxProofFiles < 1 || request.MaxProofFiles > 20 ||
		request.MaxProofTotalBytes < 1<<20 || request.MaxProofTotalBytes > 100<<20 ||
		request.NameMaxLength < 20 || request.NameMaxLength > 160 ||
		request.SummaryMaxLength < 80 || request.SummaryMaxLength > 1000 ||
		request.HistoryDays < 7 || request.HistoryDays > 90 {
		writeError(w, http.StatusBadRequest, "服务器设置超出允许范围")
		return
	}
	raw, _ := json.Marshal(request)
	if _, err := s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now()) on conflict(key) do update
		set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		serverSettingsKey, string(raw), currentClaims(r).Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "保存服务器设置失败")
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func (s *Server) probeMinecraftServer(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Address string `json:"address"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请填写服务器地址")
		return
	}
	result, err := serverprobe.ProbeWithOptions(r.Context(), request.Address, true)
	if err != nil {
		writeError(w, http.StatusBadRequest, "无法连接服务器："+err.Error())
		return
	}
	var existingID string
	err = s.db.QueryRow(r.Context(), `select public_id from minecraft_servers
		where normalized_address=$1 and review_status in ('pending','approved') limit 1`,
		result.NormalizedAddress).Scan(&existingID)
	if err == nil {
		writeError(w, http.StatusConflict, "该服务器地址已经收录或正在审核（编号："+existingID+"）")
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "检查重复服务器失败")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) createMinecraftServer(w http.ResponseWriter, r *http.Request) {
	var request createMinecraftServerRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	settings := loadServerCatalogSettings(r.Context(), s.db)
	claims := currentClaims(r)
	reviewRequired := loadReviewConfig(r.Context(), s.db).ServerCreate &&
		!claimsAllow(claims, "server.create.no-review") &&
		!claimsAllow(claims, "admin.*")
	if err := normalizeAndValidateServerRequest(&request, settings, reviewRequired); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Re-probe on submission. This avoids accepting a stale or forged result
	// from step one and guarantees that only reachable servers enter review.
	probe, err := serverprobe.Probe(r.Context(), request.Address)
	if err != nil {
		writeError(w, http.StatusBadRequest, "提交前无法再次连接服务器："+err.Error())
		return
	}
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建服务器提交失败")
		return
	}
	defer tx.Rollback(r.Context())
	proofFiles := make([]reviewAttachmentFile, 0)
	if reviewRequired {
		proofFiles, err = resolveServerProofFiles(r.Context(), tx, claims.Subject, request.ProofFileIDs, settings)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	hash := sha256.Sum256([]byte(probe.NormalizedAddress))
	slug := "server-" + hex.EncodeToString(hash[:6]) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var serverID int64
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into minecraft_servers(
		slug,address,normalized_address,handshake_host,connect_host,connect_port,
		name,short_description,body_markdown,minecraft_versions,dedicated_client,languages,
		primary_tag,has_whitelist,online_mode,icon_data_uri,modded,loader,mod_list_complete,
		review_status,proof_text,created_by,last_online,last_latency_ms,last_players_online,
		last_players_max,last_motd,last_minecraft_version,last_protocol,last_checked_at,
		last_error,next_probe_at,published_at
	) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
		$20,$21,$22,true,$23,$24,$25,$26,$27,$28,now(),'',now()+interval '5 minutes',
		case when $20='approved' then now() else null end)
		returning id,public_id`,
		slug, strings.TrimSpace(request.Address), probe.NormalizedAddress, probe.HandshakeHost,
		probe.ConnectHost, probe.ConnectPort, request.Name, request.ShortDescription,
		request.BodyMarkdown, request.MinecraftVersions, request.DedicatedClient,
		request.Languages, request.PrimaryTag, request.HasWhitelist, request.OnlineMode,
		probe.IconDataURI, probe.Modded || len(request.Mods) > 0 || len(probe.Mods) > 0,
		probe.Loader, probe.ModListComplete, reviewStatus, request.ProofText, claims.Subject,
		probe.LatencyMS, probe.PlayersOnline, probe.PlayersMax, probe.MOTD,
		probe.MinecraftVersion, probe.Protocol,
	).Scan(&serverID, &publicID)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "normalized_address") ||
			strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "该服务器地址已经收录或正在审核")
		} else {
			writeError(w, http.StatusInternalServerError, "保存服务器失败")
		}
		return
	}
	for index, link := range request.Links {
		if _, err = tx.Exec(r.Context(), `insert into minecraft_server_links(server_id,kind,label,url,display_order)
			values($1,$2,$3,$4,$5)`, serverID, link.Kind, link.Label, link.URL, index); err != nil {
			writeError(w, http.StatusInternalServerError, "保存服务器链接失败")
			return
		}
	}
	mods := mergeServerModRequests(probe.Mods, request.Mods)
	if err = insertMinecraftServerMods(r.Context(), tx, serverID, mods); err != nil {
		log.Printf("save minecraft server mods failed: server_id=%d public_id=%s mod_count=%d: %v",
			serverID, publicID, len(mods), err)
		writeError(w, http.StatusInternalServerError, "保存服务器模组列表失败")
		return
	}
	for index, file := range proofFiles {
		if _, err = tx.Exec(r.Context(), `insert into minecraft_server_proof_files(server_id,oss_file_id,display_order)
			values($1,$2,$3)`, serverID, file.InternalID, index); err != nil {
			writeError(w, http.StatusInternalServerError, "保存证明附件失败")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `insert into minecraft_server_status_samples(
		server_id,online,latency_ms,players_online,players_max,minecraft_version,protocol
	) values($1,true,$2,$3,$4,$5,$6)`, serverID, probe.LatencyMS, probe.PlayersOnline,
		probe.PlayersMax, probe.MinecraftVersion, probe.Protocol); err != nil {
		writeError(w, http.StatusInternalServerError, "保存服务器状态失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交服务器失败")
		return
	}
	annotateActivityID(r, activity.ActionCreate, activity.ObjectServer, "minecraft_server", serverID, len(request.BodyMarkdown))
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": publicID, "reviewStatus": reviewStatus, "published": reviewStatus == "approved",
	})
}

func (s *Server) updateMinecraftServer(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("serverId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "服务器编号不正确")
		return
	}
	var request updateMinecraftServerRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err := normalizeAndValidateServerUpdateRequest(&request, loadServerCatalogSettings(r.Context(), s.db)); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新服务器失败")
		return
	}
	defer tx.Rollback(r.Context())

	var serverID, ownerID int64
	var loader, previousBody string
	err = tx.QueryRow(r.Context(), `select id,created_by,loader,body_markdown from minecraft_servers
		where public_id=$1 for update`, publicID).Scan(&serverID, &ownerID, &loader, &previousBody)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器失败")
		return
	}
	claims := currentClaims(r)
	if claims.Subject != ownerID &&
		!claimsAllow(claims, "server.edit."+publicID) &&
		!claimsAllow(claims, "server.review") &&
		!claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "没有编辑这个服务器的权限")
		return
	}

	if _, err = tx.Exec(r.Context(), `update minecraft_servers set
		name=$2,short_description=$3,body_markdown=$4,minecraft_versions=$5,
		dedicated_client=$6,languages=$7,primary_tag=$8,has_whitelist=$9,
		online_mode=$10,modded=$11,updated_at=now()
		where id=$1`, serverID, request.Name, request.ShortDescription, request.BodyMarkdown,
		request.MinecraftVersions, request.DedicatedClient, request.Languages,
		request.PrimaryTag, request.HasWhitelist, request.OnlineMode,
		len(request.Mods) > 0 || loader != ""); err != nil {
		writeError(w, http.StatusInternalServerError, "保存服务器资料失败")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from minecraft_server_links where server_id=$1`, serverID); err != nil {
		writeError(w, http.StatusInternalServerError, "更新服务器链接失败")
		return
	}
	for index, link := range request.Links {
		if _, err = tx.Exec(r.Context(), `insert into minecraft_server_links(server_id,kind,label,url,display_order)
			values($1,$2,$3,$4,$5)`, serverID, link.Kind, link.Label, link.URL, index); err != nil {
			writeError(w, http.StatusInternalServerError, "保存服务器链接失败")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `delete from minecraft_server_mods where server_id=$1`, serverID); err != nil {
		writeError(w, http.StatusInternalServerError, "更新服务器模组列表失败")
		return
	}
	if err = insertMinecraftServerMods(r.Context(), tx, serverID, request.Mods); err != nil {
		log.Printf("update minecraft server mods failed: server_id=%d public_id=%s mod_count=%d: %v",
			serverID, publicID, len(request.Mods), err)
		writeError(w, http.StatusInternalServerError, "保存服务器模组列表失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "更新服务器失败")
		return
	}
	annotateActivityID(r, activity.ActionEdit, activity.ObjectServer, "minecraft_server", serverID,
		activity.AddedMarkdownBytes(previousBody, request.BodyMarkdown))
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID})
}

func normalizeAndValidateServerUpdateRequest(request *updateMinecraftServerRequest, settings serverCatalogSettings) error {
	candidate := createMinecraftServerRequest{
		Address:           "existing-server",
		Name:              request.Name,
		ShortDescription:  request.ShortDescription,
		BodyMarkdown:      request.BodyMarkdown,
		MinecraftVersions: request.MinecraftVersions,
		DedicatedClient:   request.DedicatedClient,
		Languages:         request.Languages,
		PrimaryTag:        request.PrimaryTag,
		HasWhitelist:      request.HasWhitelist,
		OnlineMode:        request.OnlineMode,
		Links:             request.Links,
		Mods:              request.Mods,
	}
	if err := normalizeAndValidateServerRequest(&candidate, settings, false); err != nil {
		return err
	}
	request.Name = candidate.Name
	request.ShortDescription = candidate.ShortDescription
	request.BodyMarkdown = candidate.BodyMarkdown
	request.MinecraftVersions = candidate.MinecraftVersions
	request.Languages = candidate.Languages
	request.PrimaryTag = candidate.PrimaryTag
	request.Links = candidate.Links
	request.Mods = candidate.Mods
	return nil
}

func normalizeAndValidateServerRequest(request *createMinecraftServerRequest, settings serverCatalogSettings, proofRequired bool) error {
	request.Address = strings.TrimSpace(request.Address)
	request.Name = strings.TrimSpace(request.Name)
	request.ShortDescription = strings.TrimSpace(request.ShortDescription)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	request.PrimaryTag = strings.ToLower(strings.TrimSpace(request.PrimaryTag))
	request.ProofText = strings.TrimSpace(request.ProofText)
	request.MinecraftVersions = uniqueTrimmed(request.MinecraftVersions, 32)
	request.Languages = uniqueTrimmed(request.Languages, 16)
	if request.Address == "" || request.Name == "" || len([]rune(request.Name)) > settings.NameMaxLength {
		return fmt.Errorf("服务器名称不能为空且不能超过 %d 个字符", settings.NameMaxLength)
	}
	if len([]rune(request.ShortDescription)) > settings.SummaryMaxLength {
		return fmt.Errorf("列表简介不能超过 %d 个字符", settings.SummaryMaxLength)
	}
	if len(request.BodyMarkdown) > 100000 {
		return errors.New("正文介绍不能超过 100000 个字符")
	}
	if len(request.MinecraftVersions) == 0 || len(request.Languages) == 0 {
		return errors.New("请选择至少一个 Minecraft 版本和服务器语言")
	}
	switch request.PrimaryTag {
	case "survival", "casual", "adventure", "creative", "war", "rpg", "minigame", "technology":
	default:
		return errors.New("请选择有效的服务器主标签")
	}
	if proofRequired {
		if request.ProofText == "" || len(request.ProofText) > 10000 {
			return errors.New("请填写有效的服主或管理成员证明说明")
		}
		if len(uniquePublicIDs(request.ProofFileIDs)) == 0 {
			return errors.New("请至少上传一份证明附件")
		}
	} else {
		request.ProofText = ""
		request.ProofFileIDs = nil
	}
	if len(request.Links) > 12 || len(request.Mods) > 4096 {
		return errors.New("服务器链接或模组数量过多")
	}
	links := make([]createServerLinkRequest, 0, len(request.Links))
	for _, link := range request.Links {
		link.Kind = strings.ToLower(strings.TrimSpace(link.Kind))
		link.Label = strings.TrimSpace(link.Label)
		link.URL = strings.TrimSpace(link.URL)
		switch link.Kind {
		case "website", "forum", "discord", "qq", "bilibili", "other":
		default:
			return errors.New("服务器链接类型不正确")
		}
		if !validHTTPURL(link.URL) || len(link.Label) > 60 {
			return errors.New("服务器链接格式不正确")
		}
		links = append(links, link)
	}
	request.Links = links
	return nil
}

func resolveServerProofFiles(ctx context.Context, tx pgx.Tx, userID int64, publicIDs []string, settings serverCatalogSettings) ([]reviewAttachmentFile, error) {
	publicIDs = uniquePublicIDs(publicIDs)
	if len(publicIDs) < 1 || len(publicIDs) > settings.MaxProofFiles {
		return nil, fmt.Errorf("证明附件需要 1–%d 个", settings.MaxProofFiles)
	}
	files := make([]reviewAttachmentFile, 0, len(publicIDs))
	var total int64
	for _, publicID := range publicIDs {
		file, err := lookupReviewAttachment(ctx, tx, reviewAttachmentLookup{
			Kind: reviewAttachmentByUploader, UploaderID: userID,
		}, publicID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("证明附件不存在或不属于当前用户")
		}
		if err != nil {
			return nil, errors.New("读取证明附件失败")
		}
		total += file.SizeBytes
		files = append(files, file)
	}
	if total > settings.MaxProofTotalBytes {
		return nil, fmt.Errorf("证明附件总大小不能超过 %.1f MB", float64(settings.MaxProofTotalBytes)/(1<<20))
	}
	return files, nil
}

func mergeServerModRequests(detected []serverprobe.Mod, declared []createServerModRequest) []createServerModRequest {
	result := make([]createServerModRequest, 0, len(detected)+len(declared))
	byID := make(map[string]int, len(detected)+len(declared))
	appendMod := func(mod createServerModRequest) {
		mod.ID = normalizeServerModID(mod.ID)
		if mod.ID == "" {
			return
		}
		mod.Version = strings.TrimSpace(mod.Version)
		if index, exists := byID[mod.ID]; exists {
			if result[index].Version == "" {
				result[index].Version = mod.Version
			}
			if result[index].Source == "manual" && mod.Source != "manual" {
				result[index].Source, result[index].Confidence = mod.Source, mod.Confidence
			}
			return
		}
		byID[mod.ID] = len(result)
		result = append(result, mod)
	}
	for _, mod := range detected {
		appendMod(createServerModRequest{ID: mod.ID, Version: mod.Version, Source: mod.Source, Confidence: mod.Confidence})
	}
	for _, mod := range declared {
		if mod.Source == "" {
			mod.Source = "manual"
		}
		if mod.Confidence == "" {
			mod.Confidence = "declared"
		}
		appendMod(mod)
	}
	return result
}

func normalizeServerModID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') &&
			character != '_' && character != '-' && character != '.' {
			return ""
		}
	}
	return value
}

func insertMinecraftServerMods(ctx context.Context, tx pgx.Tx, serverID int64, mods []createServerModRequest) error {
	mods = mergeServerModRequests(nil, mods)
	if len(mods) == 0 {
		return nil
	}
	modIDs := make([]string, 0, len(mods))
	versions := make([]string, 0, len(mods))
	sources := make([]string, 0, len(mods))
	confidences := make([]string, 0, len(mods))
	for _, mod := range mods {
		switch mod.Source {
		case "forge_status", "configuration", "agent", "manual":
		default:
			mod.Source = "manual"
		}
		switch mod.Confidence {
		case "exact", "high", "inferred", "declared":
		default:
			mod.Confidence = "declared"
		}
		modIDs = append(modIDs, mod.ID)
		versions = append(versions, mod.Version)
		sources = append(sources, mod.Source)
		confidences = append(confidences, mod.Confidence)
	}
	rows, err := tx.Query(ctx, `insert into minecraft_server_mods(
		server_id,mod_id,raw_mod_id,version,source,confidence
	)
	select $1,resolved.mod_id,input.raw_mod_id,input.version,input.source,input.confidence
	from unnest($2::text[],$3::text[],$4::text[],$5::text[])
		as input(raw_mod_id,version,source,confidence)
	left join lateral (
		select mod.id mod_id
		from mod_identifiers identifier join mods mod on mod.id=identifier.mod_id
		where lower(identifier.identifier)=input.raw_mod_id and mod.review_status='approved'
		order by mod.id limit 1
	) resolved on true
	on conflict(server_id,raw_mod_id) do update set
		mod_id=excluded.mod_id,version=excluded.version,source=excluded.source,confidence=excluded.confidence
	returning id,raw_mod_id,mod_id`, serverID, modIDs, versions, sources, confidences)
	if err != nil {
		return err
	}
	rowIDs := make([]int64, 0, len(mods))
	unresolvedRowIDs := make([]int64, 0, len(mods))
	unresolvedModIDs := make([]string, 0, len(mods))
	for rows.Next() {
		var rowID int64
		var rawModID string
		var resolvedModID *int64
		if err = rows.Scan(&rowID, &rawModID, &resolvedModID); err != nil {
			rows.Close()
			return err
		}
		rowIDs = append(rowIDs, rowID)
		if resolvedModID == nil {
			unresolvedRowIDs = append(unresolvedRowIDs, rowID)
			unresolvedModIDs = append(unresolvedModIDs, rawModID)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(rowIDs) != len(mods) {
		return fmt.Errorf("saved %d of %d server mods", len(rowIDs), len(mods))
	}
	if _, err = tx.Exec(ctx, `delete from unresolved_references
		where source_type='minecraft_server_mod' and source_id=any($1::bigint[])`, rowIDs); err != nil {
		return err
	}
	if len(unresolvedRowIDs) == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `insert into unresolved_references(
		source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
	)
	select 'minecraft_server_mod',input.source_id,'mods','mod',input.raw_identifier,
		lower(input.raw_identifier),jsonb_build_object('serverId',$3::bigint)
	from unnest($1::bigint[],$2::text[]) as input(source_id,raw_identifier)
	on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
	set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
		resolved_at=null,metadata=excluded.metadata,updated_at=now()`, unresolvedRowIDs, unresolvedModIDs, serverID)
	return err
}

func (s *Server) publicMinecraftServers(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag")))
	language := strings.TrimSpace(r.URL.Query().Get("language"))
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	modFilters := serverModFilters(r.URL.Query().Get("mods"))
	modded := strings.TrimSpace(r.URL.Query().Get("modded"))
	online := strings.TrimSpace(r.URL.Query().Get("online"))
	whitelist := strings.TrimSpace(r.URL.Query().Get("whitelist"))
	onlineMode := strings.TrimSpace(r.URL.Query().Get("onlineMode"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 20, 60)
	page := 1
	if parsed, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && parsed > 0 {
		page = min(parsed, 10000)
	}
	indexed := s.searchServerPage(r.Context(), query, tag, language, version, modFilters,
		modded, online, whitelist, onlineMode, limit, (page-1)*limit)
	databaseOffset := (page - 1) * limit
	if indexed.Used {
		databaseOffset = 0
	}
	where := []string{"server.review_status='approved'"}
	args := make([]any, 0, 10)
	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}
	indexedPosition := 0
	if indexed.Used {
		args = append(args, indexed.IDs)
		indexedPosition = len(args)
		where = append(where, fmt.Sprintf("server.id=any($%d::bigint[])", indexedPosition))
	} else if query != "" {
		args = append(args, query)
		position := len(args)
		where = append(where, fmt.Sprintf(`(
			to_tsvector('simple',server.name||' '||server.short_description||' '||server.body_markdown)
				@@ plainto_tsquery('simple',$%d)
			or server.name ilike '%%'||$%d||'%%'
			or exists(select 1 from minecraft_server_mods server_mod
				left join mods mod on mod.id=server_mod.mod_id
				where server_mod.server_id=server.id
				  and (server_mod.raw_mod_id ilike '%%'||$%d||'%%'
					or mod.primary_name ilike '%%'||$%d||'%%'
					or mod.secondary_name ilike '%%'||$%d||'%%'))
		)`, position, position, position, position, position))
	}
	if tag != "" {
		add("server.primary_tag=$%d", tag)
	}
	if language != "" {
		add("$%d=any(server.languages)", language)
	}
	if version != "" {
		add("$%d=any(server.minecraft_versions)", version)
	}
	for _, modFilter := range modFilters {
		args = append(args, modFilter)
		position := len(args)
		where = append(where, fmt.Sprintf(`exists(
			select 1 from minecraft_server_mods filter_mod
			left join mods collected_mod on collected_mod.id=filter_mod.mod_id
			where filter_mod.server_id=server.id and (
				lower(filter_mod.raw_mod_id)=lower($%d)
				or lower(coalesce(collected_mod.public_id,''))=lower($%d)
				or lower(coalesce(collected_mod.project_code,''))=lower($%d)
				or exists(
					select 1 from mod_identifiers identifier
					where identifier.mod_id=filter_mod.mod_id
					  and lower(identifier.identifier)=lower($%d)
				)
			)
		)`, position, position, position, position))
	}
	if modded == "true" || modded == "false" {
		add("server.modded=$%d", modded == "true")
	}
	if online == "true" || online == "false" {
		add("server.last_online=$%d", online == "true")
	}
	if whitelist == "true" || whitelist == "false" {
		add("server.has_whitelist=$%d", whitelist == "true")
	}
	if onlineMode == "true" || onlineMode == "false" {
		add("server.online_mode=$%d", onlineMode == "true")
	}
	whereSQL := strings.Join(where, " and ")
	var total int
	if indexed.Used {
		total = indexed.Total
	} else if err := s.db.QueryRow(r.Context(), `select count(*) from minecraft_servers server where `+whereSQL, args...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器数量失败")
		return
	}
	listArgs := append(append([]any{}, args...), limit, databaseOffset)
	orderSQL := "server.last_online desc,server.updated_at desc,server.id desc"
	if indexed.Used {
		orderSQL = fmt.Sprintf("array_position($%d::bigint[],server.id)", indexedPosition)
	}
	rows, err := s.db.Query(r.Context(), `select server.public_id,server.name,server.short_description,
		server.icon_data_uri,server.modded,server.loader,server.languages,server.primary_tag,
		server.minecraft_versions,server.last_online,coalesce(server.last_latency_ms,-1),
		server.last_players_online,server.last_players_max,server.last_checked_at
		from minecraft_servers server where `+whereSQL+`
		order by `+orderSQL+`
		limit $`+strconv.Itoa(len(args)+1)+` offset $`+strconv.Itoa(len(args)+2), listArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器列表失败")
		return
	}
	defer rows.Close()
	items := make([]minecraftServerListItem, 0, limit)
	for rows.Next() {
		var item minecraftServerListItem
		var latency int
		if err = rows.Scan(&item.ID, &item.Name, &item.ShortDescription, &item.IconDataURI,
			&item.Modded, &item.Loader, &item.Languages, &item.PrimaryTag,
			&item.MinecraftVersions, &item.Online, &latency, &item.PlayersOnline,
			&item.PlayersMax, &item.LastCheckedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析服务器列表失败")
			return
		}
		if latency >= 0 {
			item.LatencyMS = &latency
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "limit": limit,
		"pages": max(1, (total+limit-1)/limit),
	})
}

func (s *Server) publicMinecraftServerDetail(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("serverId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "服务器编号不正确")
		return
	}
	claims := currentClaims(r)
	var detail minecraftServerDetail
	var latency, protocol int
	var ownerID int64
	err := s.db.QueryRow(r.Context(), `select server.public_id,server.name,server.short_description,
		server.icon_data_uri,server.modded,server.loader,server.languages,server.primary_tag,
		server.minecraft_versions,server.last_online,coalesce(server.last_latency_ms,-1),
		server.last_players_online,server.last_players_max,server.last_checked_at,server.address,
		server.body_markdown,server.dedicated_client,server.has_whitelist,server.online_mode,
		server.last_motd,server.last_minecraft_version,coalesce(server.last_protocol,-1),
		server.mod_list_complete,server.review_status,server.created_at,server.updated_at,server.created_by
		from minecraft_servers server where server.public_id=$1`, publicID).Scan(
		&detail.ID, &detail.Name, &detail.ShortDescription, &detail.IconDataURI,
		&detail.Modded, &detail.Loader, &detail.Languages, &detail.PrimaryTag,
		&detail.MinecraftVersions, &detail.Online, &latency, &detail.PlayersOnline,
		&detail.PlayersMax, &detail.LastCheckedAt, &detail.Address, &detail.BodyMarkdown,
		&detail.DedicatedClient, &detail.HasWhitelist, &detail.OnlineMode, &detail.MOTD,
		&detail.MinecraftVersion, &protocol, &detail.ModListComplete, &detail.ReviewStatus,
		&detail.CreatedAt, &detail.UpdatedAt, &ownerID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "服务器不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器失败")
		return
	}
	if detail.ReviewStatus != "approved" && claims.Subject != ownerID &&
		!claimsAllow(claims, "server.review") && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusNotFound, "服务器不存在")
		return
	}
	detail.CanEdit = claims.Subject == ownerID ||
		claimsAllow(claims, "server.edit."+publicID) ||
		claimsAllow(claims, "server.review") ||
		claimsAllow(claims, "admin.*")
	if latency >= 0 {
		detail.LatencyMS = &latency
	}
	if protocol >= 0 {
		detail.Protocol = &protocol
	}
	detail.Links, err = s.minecraftServerLinks(r.Context(), publicID)
	if err == nil {
		detail.Mods, err = s.minecraftServerMods(r.Context(), publicID)
	}
	if err != nil {
		log.Printf("read minecraft server associations failed: public_id=%s: %v", publicID, err)
		writeError(w, http.StatusInternalServerError, "读取服务器关联资料失败")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) minecraftServerLinks(ctx context.Context, publicID string) ([]minecraftServerLink, error) {
	rows, err := s.db.Query(ctx, `select link.kind,link.label,link.url
		from minecraft_server_links link join minecraft_servers server on server.id=link.server_id
		where server.public_id=$1 order by link.display_order,link.id`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]minecraftServerLink, 0)
	for rows.Next() {
		var item minecraftServerLink
		if err = rows.Scan(&item.Kind, &item.Label, &item.URL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) minecraftServerMods(ctx context.Context, publicID string) ([]minecraftServerMod, error) {
	items, err := readMinecraftServerMods(ctx, s.db, publicID)
	if err != nil {
		return nil, err
	}
	ossCfg := s.ossConfigFromSettings(ctx)
	for index := range items {
		items[index].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, items[index].IconURL)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

type minecraftServerModQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func readMinecraftServerMods(ctx context.Context, query minecraftServerModQuerier, publicID string) ([]minecraftServerMod, error) {
	rows, err := query.Query(ctx, `select server_mod.raw_mod_id,server_mod.version,server_mod.source,
		server_mod.confidence,mod.id is not null,coalesce(mod.project_code,''),coalesce(primary_identifier.identifier,''),
		coalesce(mod.primary_name,''),coalesce(mod.slug,''),coalesce(mod.icon_url,'')
		from minecraft_server_mods server_mod
		join minecraft_servers server on server.id=server_mod.server_id
		left join mods mod on mod.id=server_mod.mod_id
		left join lateral (select identifier.identifier from mod_identifiers identifier where identifier.mod_id=mod.id
			order by identifier.is_primary desc,identifier.display_order,identifier.id limit 1) primary_identifier on true
		where server.public_id=$1 order by lower(coalesce(mod.primary_name,server_mod.raw_mod_id)),server_mod.id`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]minecraftServerMod, 0)
	for rows.Next() {
		var item minecraftServerMod
		if err = rows.Scan(&item.ID, &item.Version, &item.Source, &item.Confidence,
			&item.Resolved, &item.ModPublicID, &item.ModID, &item.ModName, &item.ModSlug, &item.IconURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func serverModFilters(value string) []string {
	result := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)
	for _, candidate := range strings.Split(value, ",") {
		normalized := normalizeServerModID(candidate)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
		if len(result) == 32 {
			break
		}
	}
	return result
}

func (s *Server) minecraftServerHistory(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("serverId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "服务器编号不正确")
		return
	}
	rangeValue := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("range")))
	if rangeValue == "" {
		rangeValue = "24h"
	}
	duration := 24 * time.Hour
	switch rangeValue {
	case "24h":
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	case "90d":
		duration = 90 * 24 * time.Hour
	default:
		writeError(w, http.StatusBadRequest, "查询时间范围不正确")
		return
	}
	settings := loadServerCatalogSettings(r.Context(), s.db)
	maxDuration := time.Duration(settings.HistoryDays) * 24 * time.Hour
	if duration > maxDuration {
		duration = maxDuration
	}
	rows, err := s.db.Query(r.Context(), `select sample.checked_at,sample.online,
		sample.latency_ms,sample.players_online,sample.players_max
		from minecraft_server_status_samples sample
		join minecraft_servers server on server.id=sample.server_id
		where server.public_id=$1 and server.review_status='approved' and sample.checked_at >= $2
		order by sample.checked_at`, publicID, time.Now().Add(-duration))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器在线历史失败")
		return
	}
	defer rows.Close()
	type historyPoint struct {
		CheckedAt     time.Time `json:"checkedAt"`
		Online        bool      `json:"online"`
		LatencyMS     *int      `json:"latencyMs"`
		PlayersOnline *int      `json:"playersOnline"`
		PlayersMax    *int      `json:"playersMax"`
	}
	points := make([]historyPoint, 0)
	for rows.Next() {
		var point historyPoint
		if err = rows.Scan(&point.CheckedAt, &point.Online, &point.LatencyMS,
			&point.PlayersOnline, &point.PlayersMax); err != nil {
			writeError(w, http.StatusInternalServerError, "解析服务器在线历史失败")
			return
		}
		points = append(points, point)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取服务器在线历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range": rangeValue, "points": points, "generatedAt": time.Now(),
	})
}
