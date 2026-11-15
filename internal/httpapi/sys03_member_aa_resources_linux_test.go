package httpapi

// Private selectors and raw integration output are local-only. This fixture
// creates one owned child database and one fresh nonce Redis namespace. It
// never migrates or cleans the parent database or an existing namespace.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sys/unix"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type sys03MemberAAPrivateInput struct {
	Nonce                   string    `json:"nonce"`
	ParentDSNFile           string    `json:"parentDSNFile"`
	ParentDSNSHA256         string    `json:"parentDSNSHA256"`
	PostgresAddress         string    `json:"postgresAddress"`
	PostgresUser            string    `json:"postgresUser"`
	PostgresDatabase        string    `json:"postgresDatabase"`
	PostgresSSLMode         string    `json:"postgresSSLMode"`
	PostgresDatabaseOID     uint32    `json:"postgresDatabaseOID"`
	PostgresDatabaseOwner   string    `json:"postgresDatabaseOwner"`
	PostgresPostmasterStart time.Time `json:"postgresPostmasterStart"`
	RedisAddress            string    `json:"redisAddress"`
	RedisDB                 int       `json:"redisDB"`
	RedisPrefix             string    `json:"redisPrefix"`
	RedisRunID              string    `json:"redisRunId"`
}

func sys03MemberAAFail(t *testing.T, stage string, err error) {
	t.Helper()
	state, errorType, messageHash := "UNKNOWN", "nil", ""
	if err != nil {
		errorType = fmt.Sprintf("%T", err)
		digest := sha256.Sum256([]byte(err.Error()))
		messageHash = hex.EncodeToString(digest[:])
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && regexp.MustCompile(`^[A-Z0-9]{5}$`).MatchString(postgresError.Code) {
			state = postgresError.Code
		}
	}
	t.Fatalf("owned regression failed stage=%s errorType=%s sqlState=%s messageSHA256=%s (raw error/values withheld)", stage, errorType, state, messageHash)
}

func sys03MemberAAStatEqual(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Uid == b.Uid && a.Gid == b.Gid &&
		a.Mode == b.Mode && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func sys03MemberAAPrivateReceipt(t *testing.T, input sys03MemberAAPrivateInput, stage string, value any) {
	t.Helper()
	// Only a new fixed-name receipt adjacent to this task's sealed authority.
	path := filepath.Join(filepath.Dir(os.Getenv("MCMODS_SYS03_MEMBER_AA_PRIVATE_INPUT_FILE")), "sys03-member-aa-"+input.Nonce+"-"+stage+".private.json")
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("owned private receipt encoding failed")
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal("owned private receipt exclusive creation failed")
	}
	var created unix.Stat_t
	if unix.Fstat(int(file.Fd()), &created) != nil || created.Mode&unix.S_IFMT != unix.S_IFREG || created.Mode&07777 != 0600 || created.Uid != uint32(os.Getuid()) || created.Nlink != 1 {
		file.Close()
		t.Fatal("owned private receipt file ownership/mode is invalid")
	}
	encoded := append(raw, '\n')
	written, writeErr := file.Write(encoded)
	syncErr, closeErr := file.Sync(), file.Close()
	if writeErr != nil || written != len(encoded) || syncErr != nil || closeErr != nil {
		t.Fatal("owned private receipt persistence failed")
	}
}

func sys03MemberAAOwnedDatabase(t *testing.T, ctx context.Context, cfg config.Config, input sys03MemberAAPrivateInput, writersClosed *bool) *pgxpool.Pool {
	t.Helper()
	base, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		t.Fatal("sealed parent configuration unavailable")
	}
	base.MaxConns, base.MinConns = 1, 0
	admin, err := pgxpool.NewWithConfig(ctx, base)
	if err != nil {
		t.Fatal("owned child database admin connection unavailable")
	}
	original, err := createTEST018OwnedDatabase(ctx, admin)
	if err != nil {
		admin.Close()
		sys03MemberAAPrivateReceipt(t, input, "database-incomplete", map[string]any{"partialOriginal": original, "completeOriginalIdentity": false, "cleanupAuthorized": false})
		t.Fatal("owned child creation lacked complete original identity; retain any partial resource")
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		defer admin.Close()
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if !*writersClosed {
			t.Error("owned server writers did not drain; preserve original child database")
			return
		}
		if err := cleanupTEST018OwnedDatabase(cleanupCtx, admin, original, func(ctx context.Context) error {
			if pool == nil {
				return nil
			}
			return closeTEST018OwnedPool(ctx, pool)
		}); err != nil {
			t.Error("owned child database original-OID/owner/nonce/ordinary-DROP cleanup not confirmed; retain scope")
			return
		}
		t.Log("SYS03_MEMBER_AA_DB_CLOSED original_oid_owner_nonce_matched=true pool_closed=true ordinary_drop=true absence_confirmed=true")
	})
	sys03MemberAAPrivateReceipt(t, input, "database-origin", original)
	scoped := base.Copy()
	scoped.ConnConfig.Database = original.Name
	scoped.MaxConns, scoped.MinConns = 8, 0
	pool, err = pgxpool.NewWithConfig(ctx, scoped)
	if err != nil {
		t.Fatal("owned child database pool unavailable")
	}
	if err := database.Migrate(ctx, pool); err != nil {
		sys03MemberAAFail(t, "owned_child_migration", err)
	}
	return pool
}

func sys03MemberAASealedFile(path, seal string, limit int64) ([]byte, unix.Stat_t, error) {
	var before, opened, after, named unix.Stat_t
	invalid := errors.New("sealed private integration input is invalid; values withheld")
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(seal) || unix.Lstat(path, &before) != nil ||
		before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != 0600 || before.Uid != uint32(os.Getuid()) || before.Nlink != 1 || before.Size < 1 || before.Size > limit {
		return nil, before, invalid
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, before, invalid
	}
	file := os.NewFile(uintptr(fd), "private-integration-input")
	defer file.Close()
	if unix.Fstat(fd, &opened) != nil || !sys03MemberAAStatEqual(before, opened) {
		return nil, before, invalid
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(raw)) != before.Size || unix.Fstat(fd, &after) != nil || unix.Lstat(path, &named) != nil ||
		!sys03MemberAAStatEqual(before, after) || !sys03MemberAAStatEqual(before, named) {
		return nil, before, invalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != seal {
		return nil, before, invalid
	}
	return raw, before, nil
}

func sys03MemberAALoadConfig(t *testing.T, ctx context.Context) (config.Config, sys03MemberAAPrivateInput) {
	t.Helper()
	path, seal := os.Getenv("MCMODS_SYS03_MEMBER_AA_PRIVATE_INPUT_FILE"), os.Getenv("MCMODS_SYS03_MEMBER_AA_PRIVATE_INPUT_SHA256")
	raw, inputIdentity, err := sys03MemberAASealedFile(path, seal, 16384)
	if err != nil {
		t.Fatal("private regression authority file failed its seal/ownership check")
	}
	var input sys03MemberAAPrivateInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(input.Nonce) ||
		input.PostgresDatabaseOID == 0 || input.PostgresDatabaseOwner == "" || input.PostgresPostmasterStart.IsZero() ||
		input.PostgresUser == "" || input.PostgresDatabase == "" || input.PostgresSSLMode == "" ||
		!regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(input.RedisRunID) || input.RedisDB != 0 ||
		!regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(input.RedisPrefix) {
		t.Fatal("private regression authority shape is invalid; values withheld")
	}
	dsnRaw, dsnIdentity, err := sys03MemberAASealedFile(input.ParentDSNFile, input.ParentDSNSHA256, 4096)
	if err != nil {
		t.Fatal("private parent connection file failed its seal/ownership check")
	}
	dsn := strings.TrimSuffix(string(dsnRaw), "\n")
	parsed, parseErr := url.Parse(dsn)
	pgHost, pgPort, addressErr := net.SplitHostPort(input.PostgresAddress)
	redisHost, redisPort, redisAddressErr := net.SplitHostPort(input.RedisAddress)
	validPort := func(port string) bool {
		value, err := strconv.ParseUint(port, 10, 16)
		return err == nil && value > 0 && strconv.FormatUint(value, 10) == port
	}
	if parseErr != nil || addressErr != nil || redisAddressErr != nil || parsed.User == nil ||
		(parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host != input.PostgresAddress || parsed.Fragment != "" ||
		parsed.User.Username() != input.PostgresUser || strings.TrimPrefix(parsed.Path, "/") != input.PostgresDatabase ||
		strings.Contains(input.PostgresDatabase, "/") || strings.TrimSpace(dsn) != dsn || strings.ContainsAny(dsn, "\r\n\x00\t") ||
		net.ParseIP(pgHost) == nil || !net.ParseIP(pgHost).IsLoopback() || !validPort(pgPort) ||
		net.ParseIP(redisHost) == nil || !net.ParseIP(redisHost).IsLoopback() || !validPort(redisPort) {
		t.Fatal("private parent connection identity is invalid; values withheld")
	}
	query, queryErr := url.ParseQuery(parsed.RawQuery)
	if queryErr != nil || len(query) != 1 || len(query["sslmode"]) != 1 || query.Get("sslmode") != input.PostgresSSLMode {
		t.Fatal("private parent connection options do not match explicit authority")
	}
	pgConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil || pgConfig.ConnConfig.Host != pgHost || strconv.Itoa(int(pgConfig.ConnConfig.Port)) != pgPort ||
		pgConfig.ConnConfig.User != input.PostgresUser || pgConfig.ConnConfig.Database != input.PostgresDatabase {
		t.Fatal("effective PostgreSQL configuration does not match private authority")
	}
	pgConfig.MaxConns, pgConfig.MinConns = 1, 0
	probe, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		t.Fatal("private PostgreSQL identity probe could not open")
	}
	var oid uint32
	var owner, user, database string
	var started time.Time
	var others int
	readOnly, err := probe.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		probe.Close()
		t.Fatal("fresh read-only PostgreSQL identity transaction unavailable")
	}
	err = readOnly.QueryRow(ctx, `select d.oid,pg_get_userbyid(d.datdba),current_user,current_database(),pg_postmaster_start_time(),
		(select count(*) from pg_stat_activity where datid=d.oid and pid<>pg_backend_pid()) from pg_database d where d.datname=current_database()`).Scan(&oid, &owner, &user, &database, &started, &others)
	rollbackErr := readOnly.Rollback(ctx)
	probe.Close()
	if err != nil || rollbackErr != nil || oid != input.PostgresDatabaseOID || owner != input.PostgresDatabaseOwner || user != input.PostgresUser ||
		database != input.PostgresDatabase || !started.Equal(input.PostgresPostmasterStart) || others != 0 {
		t.Fatal("fresh PostgreSQL parent identity/zero-other-client check failed")
	}
	// The sealed file is the only PostgreSQL selector, never an inherited or
	// compiled connection default. Keep its value in private process memory.
	cfg := config.Load()
	cfg.DB.URL = dsn
	if cfg.DB.ConnString() != dsn || !cfg.Redis.Enabled || cfg.Redis.Addr != input.RedisAddress || cfg.Redis.DB != input.RedisDB ||
		cfg.Redis.Prefix != input.RedisPrefix || cfg.Redis.Namespace != "sys03_member_aa_"+input.Nonce ||
		!cfg.AntiAbuse.Enabled || cfg.AntiAbuse.ChallengeProvider != "proof" || cfg.AntiAbuse.FormMinimumAge != 800*time.Millisecond {
		t.Fatal("normal enabled Redis/anti-abuse configuration and fresh private scope are required")
	}
	t.Cleanup(func() {
		_, currentInput, inputErr := sys03MemberAASealedFile(path, seal, 16384)
		_, currentDSN, dsnErr := sys03MemberAASealedFile(input.ParentDSNFile, input.ParentDSNSHA256, 4096)
		if inputErr != nil || dsnErr != nil || !sys03MemberAAStatEqual(currentInput, inputIdentity) || !sys03MemberAAStatEqual(currentDSN, dsnIdentity) {
			t.Error("original private authority/connection file integrity changed")
		}
	})
	return cfg, input
}

func sys03MemberAARedisScope(t *testing.T, ctx context.Context, cfg config.RedisConfig, input sys03MemberAAPrivateInput, writersClosed *bool) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: cfg.Addr, Username: cfg.Username, Password: cfg.Password, DB: cfg.DB, PoolSize: 1, MinIdleConns: 0})
	clientClosed := false
	t.Cleanup(func() {
		if !clientClosed && client.Close() != nil {
			t.Error("owned Redis guard client close failed")
		}
		clientClosed = true
	})
	// No protocol errors are formatted: Redis can include private addresses.
	checkRun := func(ctx context.Context) bool {
		info, err := client.Info(ctx, "server").Result()
		if err != nil {
			return false
		}
		for _, line := range strings.Split(info, "\n") {
			if strings.TrimSpace(line) == "run_id:"+input.RedisRunID {
				return true
			}
		}
		return false
	}
	keyPrefix := cfg.Prefix + ":" + cfg.Namespace + ":"
	keys := func(ctx context.Context) ([]string, error) {
		var result []string
		var cursor uint64
		for {
			part, next, err := client.Scan(ctx, cursor, keyPrefix+"*", 128).Result()
			if err != nil {
				return nil, errors.New("owned Redis scan unavailable")
			}
			result = append(result, part...)
			if len(result) > 4096 {
				return nil, errors.New("owned Redis scan exceeded bound")
			}
			cursor = next
			if cursor == 0 {
				return result, nil
			}
		}
	}
	before, err := keys(ctx)
	if !checkRun(ctx) || err != nil || len(before) != 0 {
		t.Fatal("fresh Redis identity/empty namespace check failed")
	}
	clientID, err := client.ClientID(ctx).Result()
	listing, listErr := client.ClientList(ctx).Result()
	if err != nil || listErr != nil {
		t.Fatal("fresh Redis client census unavailable")
	}
	clientLines := strings.Split(strings.TrimSpace(listing), "\n")
	if len(clientLines) != 1 {
		t.Fatal("fresh Redis parent has an unowned client")
	}
	for _, line := range clientLines {
		ownID := false
		for _, field := range strings.Fields(line) {
			if field == "id="+strconv.FormatInt(clientID, 10) {
				ownID = true
			}
		}
		if !ownID {
			t.Fatal("fresh Redis parent client identity does not match")
		}
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		t.Fatal("private Redis ownership entropy unavailable")
	}
	marker := hex.EncodeToString(random)
	owner := cfg.Prefix + ":sys03-member-aa-owner:" + input.Nonce
	sentinel := cfg.Prefix + ":sys03-member-aa-sentinel:" + input.Nonce
	sys03MemberAAPrivateReceipt(t, input, "redis-attempt", map[string]any{
		"nonce": input.Nonce, "runId": input.RedisRunID, "db": cfg.DB, "keyPrefix": keyPrefix,
		"ownerKey": owner, "sentinelKey": sentinel, "privateOwnerMarker": marker, "positiveOwnership": false,
	})
	claim := redis.NewScript(`if redis.call('EXISTS',KEYS[1])~=0 or redis.call('EXISTS',KEYS[2])~=0 then return 0 end redis.call('SET',KEYS[1],ARGV[1]) redis.call('SET',KEYS[2],ARGV[1]) return 1`)
	claimed, err := claim.Run(ctx, client, []string{owner, sentinel}, marker).Int()
	if err != nil || claimed != 1 {
		t.Fatal("fresh Redis ownership was not positively established; no cleanup/adoption")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		retain := func() {
			t.Error("owned Redis cleanup not proven; preserve original namespace and private ownership evidence")
		}
		if !*writersClosed || !checkRun(cleanupCtx) {
			retain()
			return
		}
		own, ownErr := client.Get(cleanupCtx, owner).Result()
		foreignSentinel, sentinelErr := client.Get(cleanupCtx, sentinel).Result()
		if ownErr != nil || sentinelErr != nil || own != marker || foreignSentinel != marker {
			retain()
			return
		}
		snapshot, scanErr := keys(cleanupCtx)
		if scanErr != nil {
			retain()
			return
		}
		for _, key := range snapshot {
			kind, typeErr := client.Type(cleanupCtx, key).Result()
			if !strings.HasPrefix(key, keyPrefix) || typeErr != nil || (kind != "string" && kind != "hash" && kind != "list" && kind != "set" && kind != "zset" && kind != "none") {
				retain()
				return
			}
		}
		// Delete only this observed fresh namespace while both original private
		// ownership witnesses remain exact. No FLUSH, name adoption or force.
		remove := redis.NewScript(`if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end for i=3,#KEYS do redis.call('DEL',KEYS[i]) end return 1`)
		removed, removeErr := remove.Run(cleanupCtx, client, append([]string{owner, sentinel}, snapshot...), marker).Int()
		after, afterErr := keys(cleanupCtx)
		if removeErr != nil || removed != 1 || afterErr != nil || len(after) != 0 || !checkRun(cleanupCtx) {
			retain()
			return
		}
		release := redis.NewScript(`if redis.call('GET',KEYS[1])~=ARGV[1] or redis.call('GET',KEYS[2])~=ARGV[1] then return 0 end redis.call('DEL',KEYS[1],KEYS[2]) return 1`)
		released, releaseErr := release.Run(cleanupCtx, client, []string{owner, sentinel}, marker).Int()
		if releaseErr != nil || released != 1 {
			retain()
			return
		}
		closeErr := client.Close()
		clientClosed = true
		if closeErr != nil {
			t.Error("owned Redis guard client close failed")
			return
		}
		t.Logf("SYS03_MEMBER_AA_REDIS_CLOSED owned_keys=%d namespace_absent=true original_run_id_unchanged=true witnesses_preserved_until_release=true", len(snapshot))
	})
	sys03MemberAAPrivateReceipt(t, input, "redis-origin", map[string]any{
		"nonce": input.Nonce, "runId": input.RedisRunID, "db": cfg.DB, "keyPrefix": keyPrefix,
		"ownerKey": owner, "sentinelKey": sentinel, "privateOwnerMarker": marker,
		"namespaceEmptyBeforeClaim": true, "otherClientsBeforeClaim": 0,
	})
}
