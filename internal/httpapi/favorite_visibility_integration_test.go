package httpapi

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func TestFavoriteTargetsRevalidateVisibilityForEveryViewer(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`create temp table public_routes(
			id bigint primary key,internal_id bigint not null,public_id text not null,entity_type text not null,canonical_path text not null
		) on commit drop`,
		`create temp table mods(
			id bigint primary key,primary_name text not null,secondary_name text not null default '',icon_url text not null default '',
			slug text not null,review_status text not null,submitted_by bigint not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table modpacks(
			id bigint primary key,primary_name text not null,secondary_name text not null default '',icon_url text not null default '',
			slug text not null,review_status text not null,submitted_by bigint not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table blueprints(
			id bigint primary key,public_id text not null,title text not null,cover_object_key text not null default '',status text not null,
			review_status text not null,owner_id bigint not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table favorite_collections(
			id bigint primary key,public_id text not null,user_id bigint not null,name text not null,is_default boolean not null,
			is_public boolean not null,created_at timestamptz not null default now()
		) on commit drop`,
		`create temp table favorite_collection_items(
			id bigint primary key,collection_id bigint not null,entity_type text not null,entity_id bigint not null,
			created_at timestamptz not null default now()
		) on commit drop`,
		`insert into public_routes values
			(100,200,'hidmod001','mod','/mods/hidden'),
			(101,201,'pubmod001','mod','/mods/public'),
			(102,202,'ownpack01','modpack','/modpacks/own'),
			(103,203,'draftbp01','blueprint','/blueprints/draftbp01'),
			(104,204,'publicbp1','blueprint','/blueprints/publicbp1'),
			(105,205,'ownblue01','blueprint','/blueprints/ownblue01'),
			(106,206,'plugin001','plugin','/plugins/plugin001')`,
		`insert into mods(id,primary_name,slug,review_status,submitted_by) values
			(200,'Hidden mod','hidden','pending',11),(201,'Public mod','public','approved',11)`,
		`insert into modpacks(id,primary_name,slug,review_status,submitted_by) values
			(202,'Own pending pack','own-pack','pending',10)`,
		`insert into blueprints(id,public_id,title,status,review_status,owner_id) values
			(203,'draftbp01','Other unfinished blueprint','processing','not_required',11),
			(204,'publicbp1','Public blueprint','ready','not_required',11),
			(205,'ownblue01','Own pending blueprint','ready','pending',10)`,
		`insert into favorite_collections values(300,'col000001',10,'Collection',false,true,now())`,
		`insert into favorite_collection_items(id,collection_id,entity_type,entity_id) values
			(400,300,'mod',200),(401,300,'mod',201),(402,300,'modpack',202),(403,300,'blueprint',203),
			(404,300,'blueprint',204),(405,300,'blueprint',205),(406,300,'plugin',206)`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	collectionOwner := security.Claims{Subject: 10}
	ordinaryViewer := security.Claims{Subject: 12}
	targetOwner := security.Claims{Subject: 11}
	if _, err = resolveFavoriteTargetWithQueryer(ctx, tx, "mod", "hidmod001", collectionOwner); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("collection owner resolved somebody else's pending Mod: %v", err)
	}
	if _, err = resolveFavoriteTargetWithQueryer(ctx, tx, "mod", "hidmod001", targetOwner); err != nil {
		t.Fatalf("target owner could not resolve their pending Mod: %v", err)
	}
	if _, err = resolveFavoriteTargetWithQueryer(ctx, tx, "plugin", "plugin001", targetOwner); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unsupported favorite target type was accepted: %v", err)
	}
	if _, err = resolveFavoriteTargetWithQueryer(ctx, tx, "modpack", "hidmod001", targetOwner); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("request type did not have to match the route type: %v", err)
	}

	ownerCollections, err := loadFavoriteCollectionPageForVisibilityTest(ctx, tx, 10, true, collectionOwner)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerCollections) != 1 {
		t.Fatalf("owner collection page is incomplete: %+v", ownerCollections)
	}
	publicCollections, err := loadFavoriteCollectionPageForVisibilityTest(ctx, tx, 10, false, ordinaryViewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(publicCollections) != 1 {
		t.Fatalf("public collection page is incomplete: %+v", publicCollections)
	}
	ownerItems, err := loadFavoriteItemPageForVisibilityTest(ctx, tx, 10, true, "col000001", collectionOwner)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerItems) != 4 {
		t.Fatalf("owner list included inaccessible or unsupported targets: %+v", ownerItems)
	}
	publicItems, err := loadFavoriteItemPageForVisibilityTest(ctx, tx, 10, false, "col000001", ordinaryViewer)
	if err != nil {
		t.Fatal(err)
	}
	if len(publicItems) != 2 {
		t.Fatalf("public list leaked hidden targets: %+v", publicItems)
	}
}

func loadFavoriteCollectionPageForVisibilityTest(
	ctx context.Context,
	queryer favoriteQueryer,
	ownerID int64,
	includePrivate bool,
	claims security.Claims,
) ([]favoriteCollectionSummary, error) {
	page, err := parseFavoriteCollectionPageRequest(url.Values{"limit": {"100"}}, ownerID, includePrivate, claims)
	if err != nil {
		return nil, err
	}
	query, arguments := favoriteCollectionPageSQL(page)
	rows, err := queryer.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]favoriteCollectionSummary, 0)
	for rows.Next() {
		row, scanErr := scanFavoriteCollectionPageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, row.Summary)
	}
	return items, rows.Err()
}

func loadFavoriteItemPageForVisibilityTest(
	ctx context.Context,
	queryer favoriteQueryer,
	ownerID int64,
	includePrivate bool,
	collectionPublicID string,
	claims security.Claims,
) ([]favoriteCollectionItemRow, error) {
	page, err := parseFavoriteItemPageRequest(url.Values{"limit": {"100"}}, ownerID, includePrivate, collectionPublicID, claims)
	if err != nil {
		return nil, err
	}
	query, arguments := favoriteItemPageSQL(page)
	rows, err := queryer.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]favoriteCollectionItemRow, 0)
	for rows.Next() {
		row, scanErr := scanFavoriteItemPageRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, row.Item)
	}
	return items, rows.Err()
}
