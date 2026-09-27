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

func TestUnresolvedReferenceTypeFilterAcceptsCatalogKindBoundary(t *testing.T) {
	maximum := strings.Repeat("a", unresolvedReferenceMaximumTypeSize)
	request, err := parseUnresolvedReferencePageRequest(map[string][]string{"type": {maximum}})
	if err != nil || request.ReferenceType != maximum {
		t.Fatalf("maximum catalog kind was rejected: request=%#v err=%v", request, err)
	}
	if _, err = parseUnresolvedReferencePageRequest(map[string][]string{"type": {maximum + "a"}}); err == nil {
		t.Fatal("oversized catalog kind was accepted")
	}
}

func TestUnresolvedReferencePagePublishesEveryAuthoritativeReferenceTypeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify authoritative unresolved-reference types")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	if _, err = pool.Exec(ctx, `
		create temporary table resource_kinds (
			code text primary key,
			family text not null,
			display_order integer not null default 0,
			user_visible boolean not null default true
		);
		insert into resource_kinds(code,family,display_order,user_visible) values
			('minecraft.item','item',10,true),
			('custom.dynamic_resource','document',20,true),
			('internal.hidden_resource','document',30,false);
	`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/unresolved-reference-types", nil)
	response := httptest.NewRecorder()
	(&Server{db: pool}).adminUnresolvedReferenceTypes(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Items []string `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon",
		"tag", "enchantment", "server", "minecraft.item", "custom.dynamic_resource", "internal.hidden_resource",
	}
	available := make(map[string]bool, len(envelope.Data.Items))
	for _, referenceType := range envelope.Data.Items {
		available[referenceType] = true
	}
	for _, referenceType := range want {
		if !available[referenceType] {
			t.Errorf("authoritative reference type %q is absent from %#v", referenceType, envelope.Data.Items)
		}
	}
}
