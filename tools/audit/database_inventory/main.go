package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type column struct {
	table, name, dataType, nullable, defaultValue string
	position                                      int
}

type namedDefinition struct {
	table, name, definition string
}

func main() {
	output := flag.String("output", "docs/audit/full-project-audit/database/SCHEMA_CATALOG.md", "output path")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	must(err)
	defer pool.Close()
	must(pool.Ping(ctx))

	var version string
	must(pool.QueryRow(ctx, `select current_setting('server_version')`).Scan(&version))

	columns := queryColumns(ctx, pool)
	constraints := queryDefinitions(ctx, pool, `select relation.relname,constraint_name.conname,pg_get_constraintdef(constraint_name.oid,true)
		from pg_constraint constraint_name join pg_class relation on relation.oid=constraint_name.conrelid
		join pg_namespace namespace on namespace.oid=relation.relnamespace where namespace.nspname='public'
		order by relation.relname,constraint_name.conname`)
	indexes := queryDefinitions(ctx, pool, `select tablename,indexname,indexdef from pg_indexes
		where schemaname='public' order by tablename,indexname`)
	triggers := queryDefinitions(ctx, pool, `select relation.relname,trigger.tgname,pg_get_triggerdef(trigger.oid,true)
		from pg_trigger trigger join pg_class relation on relation.oid=trigger.tgrelid
		join pg_namespace namespace on namespace.oid=relation.relnamespace
		where namespace.nspname='public' and not trigger.tgisinternal order by relation.relname,trigger.tgname`)
	views := queryPairs(ctx, pool, `select viewname,definition from pg_views where schemaname='public' order by viewname`)
	sequences := queryNames(ctx, pool, `select sequence_name from information_schema.sequences where sequence_schema='public' order by sequence_name`)
	functions := queryNames(ctx, pool, `select distinct routine_name from information_schema.routines where specific_schema='public' order by routine_name`)
	tableStats := queryStats(ctx, pool)

	var out strings.Builder
	out.WriteString("# PostgreSQL Schema 目录\n\n")
	fmt.Fprintf(&out, "生成时间：%s  \nPostgreSQL：`%s`\n\n", time.Now().UTC().Format(time.RFC3339), escape(version))
	out.WriteString("本目录通过只读系统目录查询生成，不包含连接地址、凭据或业务数据。`估算行数` 来自 PostgreSQL 统计信息，不是精确 COUNT。\n\n")

	tables := make([]string, 0)
	seen := map[string]bool{}
	for _, item := range columns {
		if !seen[item.table] {
			seen[item.table] = true
			tables = append(tables, item.table)
		}
	}
	sort.Strings(tables)
	fmt.Fprintf(&out, "## 汇总\n\n- 表：%d\n- 列：%d\n- 约束：%d\n- 索引：%d\n- 触发器：%d\n- 视图：%d\n- 序列：%d\n- 函数：%d\n\n", len(tables), len(columns), len(constraints), len(indexes), len(triggers), len(views), len(sequences), len(functions))

	out.WriteString("## 表与列\n\n| 表 | 估算行数 | 序号 | 列 | 类型 | NULL | 默认值 |\n| --- | ---: | ---: | --- | --- | --- | --- |\n")
	for _, item := range columns {
		fmt.Fprintf(&out, "| `%s` | %d | %d | `%s` | `%s` | %s | `%s` |\n", escape(item.table), tableStats[item.table], item.position, escape(item.name), escape(item.dataType), item.nullable, escape(short(item.defaultValue, 120)))
	}
	writeDefinitions(&out, "约束", constraints)
	writeDefinitions(&out, "索引", indexes)
	writeDefinitions(&out, "非内部触发器", triggers)

	out.WriteString("## 视图\n\n| 名称 | 定义 |\n| --- | --- |\n")
	for _, item := range views {
		fmt.Fprintf(&out, "| `%s` | `%s` |\n", escape(item[0]), escape(short(compact(item[1]), 500)))
	}
	out.WriteString("\n## 序列\n\n")
	writeNameList(&out, sequences)
	out.WriteString("\n## 函数\n\n")
	writeNameList(&out, functions)

	must(os.MkdirAll(filepath.Dir(*output), 0o755))
	must(os.WriteFile(*output, []byte(out.String()), 0o644))
}

func queryColumns(ctx context.Context, pool *pgxpool.Pool) []column {
	rows, err := pool.Query(ctx, `select table_name,ordinal_position,column_name,
		case when data_type='USER-DEFINED' then udt_name else data_type end,is_nullable,coalesce(column_default,'')
		from information_schema.columns where table_schema='public' order by table_name,ordinal_position`)
	must(err)
	defer rows.Close()
	result := make([]column, 0, 2048)
	for rows.Next() {
		var item column
		must(rows.Scan(&item.table, &item.position, &item.name, &item.dataType, &item.nullable, &item.defaultValue))
		result = append(result, item)
	}
	must(rows.Err())
	return result
}

func queryDefinitions(ctx context.Context, pool *pgxpool.Pool, query string) []namedDefinition {
	rows, err := pool.Query(ctx, query)
	must(err)
	defer rows.Close()
	result := make([]namedDefinition, 0, 512)
	for rows.Next() {
		var item namedDefinition
		must(rows.Scan(&item.table, &item.name, &item.definition))
		result = append(result, item)
	}
	must(rows.Err())
	return result
}

func queryPairs(ctx context.Context, pool *pgxpool.Pool, query string) [][2]string {
	rows, err := pool.Query(ctx, query)
	must(err)
	defer rows.Close()
	result := make([][2]string, 0)
	for rows.Next() {
		var item [2]string
		must(rows.Scan(&item[0], &item[1]))
		result = append(result, item)
	}
	must(rows.Err())
	return result
}

func queryNames(ctx context.Context, pool *pgxpool.Pool, query string) []string {
	rows, err := pool.Query(ctx, query)
	must(err)
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var item string
		must(rows.Scan(&item))
		result = append(result, item)
	}
	must(rows.Err())
	return result
}

func queryStats(ctx context.Context, pool *pgxpool.Pool) map[string]int64 {
	rows, err := pool.Query(ctx, `select relname,greatest(n_live_tup,0)::bigint from pg_stat_user_tables`)
	must(err)
	defer rows.Close()
	result := map[string]int64{}
	for rows.Next() {
		var name string
		var count int64
		must(rows.Scan(&name, &count))
		result[name] = count
	}
	must(rows.Err())
	return result
}

func writeDefinitions(out *strings.Builder, title string, items []namedDefinition) {
	fmt.Fprintf(out, "\n## %s\n\n| 表 | 名称 | 定义 |\n| --- | --- | --- |\n", title)
	for _, item := range items {
		fmt.Fprintf(out, "| `%s` | `%s` | `%s` |\n", escape(item.table), escape(item.name), escape(short(compact(item.definition), 500)))
	}
}

func writeNameList(out *strings.Builder, items []string) {
	for _, item := range items {
		fmt.Fprintf(out, "- `%s`\n", escape(item))
	}
}

func compact(value string) string { return strings.Join(strings.Fields(value), " ") }

func short(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

func escape(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "`", "'")
	return strings.ReplaceAll(value, "\n", " ")
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
