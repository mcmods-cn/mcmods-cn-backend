package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestServerReviewSummaryUsesOneQueryAndLoadsLargeDetailOnDemandIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate the bounded server review projection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table users (id bigint primary key,public_id text not null,username text not null);
		create temporary table minecraft_servers (
			id bigint primary key,public_id text not null,name text not null,address text not null,
			short_description text not null,body_markdown text not null,minecraft_versions text[] not null,
			dedicated_client boolean not null,languages text[] not null,primary_tag text not null,
			has_whitelist boolean not null,online_mode boolean not null,modded boolean not null,
			loader text not null,proof_text text not null,review_status text not null,review_note text not null,
			submitted_by bigint not null,created_at timestamptz not null,reviewed_at timestamptz
		);
		create index idx_minecraft_servers_review_page on minecraft_servers(review_status,created_at,id);
		create temporary table oss_files (
			id bigint primary key,public_id text not null,original_name text not null,
			size_bytes bigint not null,source_size_bytes bigint not null,status text not null,scan_status text not null
		);
		create temporary table minecraft_server_proof_files (
			server_id bigint not null,oss_file_id bigint not null,display_order integer not null,
			primary key(server_id,oss_file_id)
		);
		create temporary table minecraft_server_links (
			id bigint primary key,server_id bigint not null,kind text not null,label text not null,
			url text not null,display_order integer not null
		);
		create index idx_minecraft_server_links_order on minecraft_server_links(server_id,display_order,id);
		create temporary table minecraft_server_mods (
			id bigint primary key,server_id bigint not null,mod_id bigint,raw_mod_id text not null,
			unique(server_id,raw_mod_id)
		);
		create temporary table minecraft_server_mod_evidence (
			server_mod_id bigint not null,source text not null,version text not null,confidence text not null,
			primary key(server_mod_id,source)
		);
		create temporary table mods (
			id bigint primary key,project_code text not null,primary_name text not null,slug text not null,icon_url text not null
		);
		create temporary table mod_identifiers (
			id bigint primary key,mod_id bigint not null,identifier text not null,is_primary boolean not null,display_order integer not null
		);
		create temporary table system_settings (key text primary key,value jsonb not null)
	`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		insert into users values(1,'u00000001','submitter');
		insert into minecraft_servers(
			id,public_id,name,address,short_description,body_markdown,minecraft_versions,
			dedicated_client,languages,primary_tag,has_whitelist,online_mode,modded,loader,
			proof_text,review_status,review_note,submitted_by,created_at
		)
		select value,'s'||lpad(value::text,8,'0'),'server-'||value,'review.example:'||(25000+value),
			'summary-'||value,'BODY_SECRET_'||value||repeat('x',1048576),array['1.21.1'],false,
			array['zh-CN'],'survival',false,true,true,'fabric',
			'PROOF_SECRET_'||value||repeat('p',65536),'pending','',1,
			timestamptz '2026-08-22 00:00:00+00'+value*interval '1 millisecond'
		from generate_series(1,100) value;
		insert into oss_files
		select server_id*10+ordinal,'f'||lpad((server_id*10+ordinal)::text,8,'0'),'proof.txt',100,100,'active','clean'
		from generate_series(1,100) server_id cross join generate_series(1,2) ordinal;
		insert into minecraft_server_proof_files
		select server_id,server_id*10+ordinal,ordinal
		from generate_series(1,100) server_id cross join generate_series(1,2) ordinal;
		insert into minecraft_server_links
		select server_id*10+ordinal,server_id,'website','link','https://example.test/'||server_id||'/'||ordinal,ordinal
		from generate_series(1,100) server_id cross join generate_series(1,2) ordinal;
		insert into minecraft_server_mods
		select server_id*10+ordinal,server_id,null,'mod-'||ordinal
		from generate_series(1,100) server_id cross join generate_series(1,2) ordinal;
		insert into minecraft_server_mod_evidence
		select server_id*10+ordinal,'manual','','declared'
		from generate_series(1,100) server_id cross join generate_series(1,2) ordinal
	`); err != nil {
		t.Fatal(err)
	}
	var serverCount int
	if err = pool.QueryRow(ctx, `select count(*) from minecraft_servers where review_status='pending'`).Scan(&serverCount); err != nil {
		t.Fatal(err)
	}
	if serverCount != 100 {
		t.Fatalf("fixture inserted %d pending servers, want 100", serverCount)
	}

	server := &Server{db: pool}
	counter.queries.Store(0)
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-reviews?status=pending&limit=100", nil)
	listResponse := httptest.NewRecorder()
	server.adminMinecraftServerReviews(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	if queries := counter.queries.Load(); queries != 1 {
		t.Fatalf("100-item server review summary executed %d SQL statements, want 1", queries)
	}
	if listResponse.Body.Len() >= 256*1024 {
		t.Fatalf("summary response=%d bytes, want below 256 KiB", listResponse.Body.Len())
	}
	if strings.Contains(listResponse.Body.String(), "BODY_SECRET_") || strings.Contains(listResponse.Body.String(), "PROOF_SECRET_") {
		t.Fatal("server review summary leaked an on-demand body or proof text")
	}
	var listEnvelope struct {
		Data struct {
			Items      []minecraftServerReviewSummary `json:"items"`
			HasMore    bool                           `json:"hasMore"`
			NextCursor string                         `json:"nextCursor"`
		} `json:"data"`
	}
	if err = json.Unmarshal(listResponse.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	page := listEnvelope.Data
	if len(page.Items) != 100 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("summary page items=%d hasMore=%t cursor=%q body=%s", len(page.Items), page.HasMore, page.NextCursor, listResponse.Body.String())
	}
	for _, item := range page.Items {
		if item.ProofFileCount != 2 || item.LinkCount != 2 || item.ModCount != 2 {
			t.Fatalf("summary %s counts=%d/%d/%d", item.ID, item.ProofFileCount, item.LinkCount, item.ModCount)
		}
	}

	counter.queries.Store(0)
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-reviews/s00000001", nil)
	detailRequest.SetPathValue("serverId", "s00000001")
	detailResponse := httptest.NewRecorder()
	server.adminMinecraftServerReviewDetail(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	if queries := counter.queries.Load(); queries != 5 {
		t.Fatalf("one server review detail executed %d SQL statements, want constant 5", queries)
	}
	var detailEnvelope struct {
		Data minecraftServerReviewItem `json:"data"`
	}
	if err = json.Unmarshal(detailResponse.Body.Bytes(), &detailEnvelope); err != nil {
		t.Fatal(err)
	}
	detail := detailEnvelope.Data
	if !strings.HasPrefix(detail.BodyMarkdown, "BODY_SECRET_1") ||
		!strings.HasPrefix(detail.ProofText, "PROOF_SECRET_1") ||
		len(detail.ProofFiles) != 2 || len(detail.Links) != 2 || len(detail.Mods) != 2 ||
		detail.ProofFileCount != 2 || detail.LinkCount != 2 || detail.ModCount != 2 {
		t.Fatalf("on-demand detail body=%d proof=%d files=%d links=%d mods=%d",
			len(detail.BodyMarkdown), len(detail.ProofText), len(detail.ProofFiles), len(detail.Links), len(detail.Mods))
	}
}
