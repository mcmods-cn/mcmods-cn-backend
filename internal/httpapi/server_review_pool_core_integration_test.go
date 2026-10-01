package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestServerReviewAssociationsWithSingleConnectionCoreIntegration(t *testing.T) {
	ctx, originalPool, cfg := isolatedAITestDatabase(t)
	configuration := originalPool.Config().Copy()
	configuration.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var userID int64
	name := "server-review-" + randomHex(12)
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, name, name+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		var serverID, fileID int64
		key := randomHex(12)
		if err = pool.QueryRow(ctx, `insert into minecraft_servers(slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by)
			values($1,$2,$2,$2,$2,25565,'Synthetic review server','survival',$3) returning id`, key, key+".example.test", userID).Scan(&serverID); err != nil {
			t.Fatal(err)
		}
		if err = pool.QueryRow(ctx, `insert into oss_files(object_key,original_name,size_bytes,uploader_id,scan_status)
			values($1,'synthetic-proof.txt',16,$2,'clean') returning id`, "synthetic/server-review/"+key, userID).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into minecraft_server_proof_files(server_id,oss_file_id) values($1,$2)`, serverID, fileID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into minecraft_server_links(server_id,url) values($1,'https://example.test/synthetic')`, serverID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into minecraft_server_mods(server_id,raw_mod_id) values($1,'synthetic_mod')`, serverID); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/server-reviews", nil)
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	response := httptest.NewRecorder()
	(&Server{db: pool, cfg: cfg}).adminMinecraftServerReviews(response, request.WithContext(requestCtx))
	if response.Code != http.StatusOK {
		t.Fatalf("single-connection review failed: status=%d body=%s context=%v", response.Code, response.Body.String(), requestCtx.Err())
	}
	var reply struct {
		Data struct {
			Items []minecraftServerReviewItem `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Data.Items) != 2 {
		t.Fatalf("server review items=%d want2", len(reply.Data.Items))
	}
	for _, item := range reply.Data.Items {
		if len(item.ProofFiles) != 1 || len(item.Links) != 1 || len(item.Mods) != 1 || item.Mods[0].ID != "synthetic_mod" {
			t.Fatalf("review associations missing: %#v", item)
		}
	}
}
