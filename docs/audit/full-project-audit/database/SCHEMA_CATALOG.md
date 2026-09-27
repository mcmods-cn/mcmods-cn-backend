# PostgreSQL Schema 目录

生成时间：2026-08-20T14:55:09Z  
PostgreSQL：`18.0`

本目录通过只读系统目录查询生成，不包含连接地址、凭据或业务数据。`估算行数` 来自 PostgreSQL 统计信息，不是精确 COUNT。

## 汇总

- 表：262
- 列：2450
- 约束：3469
- 索引：978
- 触发器：137
- 视图：3
- 序列：119
- 函数：123

## 表与列

| 表 | 估算行数 | 序号 | 列 | 类型 | NULL | 默认值 |
| --- | ---: | ---: | --- | --- | --- | --- |
| `activity_actions` | 11 | 1 | `id` | `smallint` | NO | `` |
| `activity_actions` | 11 | 2 | `code` | `text` | NO | `` |
| `activity_actions` | 11 | 3 | `name` | `text` | NO | `` |
| `activity_cleanup_runs` | 0 | 1 | `id` | `bigint` | NO | `nextval('activity_cleanup_runs_id_seq'::regclass)` |
| `activity_cleanup_runs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `activity_cleanup_runs` | 0 | 3 | `source` | `text` | NO | `` |
| `activity_cleanup_runs` | 0 | 4 | `initiated_by` | `bigint` | YES | `` |
| `activity_cleanup_runs` | 0 | 5 | `status` | `text` | NO | `` |
| `activity_cleanup_runs` | 0 | 6 | `filters` | `jsonb` | NO | `'{}'::jsonb` |
| `activity_cleanup_runs` | 0 | 7 | `confirmation_hash` | `text` | NO | `''::text` |
| `activity_cleanup_runs` | 0 | 8 | `matched_count` | `bigint` | NO | `0` |
| `activity_cleanup_runs` | 0 | 9 | `deleted_count` | `bigint` | NO | `0` |
| `activity_cleanup_runs` | 0 | 10 | `started_at` | `timestamp with time zone` | NO | `now()` |
| `activity_cleanup_runs` | 0 | 11 | `finished_at` | `timestamp with time zone` | YES | `` |
| `activity_cleanup_runs` | 0 | 12 | `expires_at` | `timestamp with time zone` | YES | `` |
| `activity_cleanup_runs` | 0 | 13 | `error_message` | `text` | NO | `''::text` |
| `activity_event_outbox` | 1 | 1 | `id` | `bigint` | NO | `nextval('activity_event_outbox_id_seq'::regclass)` |
| `activity_event_outbox` | 1 | 2 | `user_id` | `bigint` | NO | `` |
| `activity_event_outbox` | 1 | 3 | `action_id` | `smallint` | NO | `` |
| `activity_event_outbox` | 1 | 4 | `object_type_id` | `smallint` | NO | `` |
| `activity_event_outbox` | 1 | 5 | `object_route_id` | `bigint` | YES | `` |
| `activity_event_outbox` | 1 | 6 | `object_public_id` | `text` | NO | `''::text` |
| `activity_event_outbox` | 1 | 7 | `object_entity_type` | `text` | NO | `''::text` |
| `activity_event_outbox` | 1 | 8 | `object_internal_id` | `bigint` | YES | `` |
| `activity_event_outbox` | 1 | 9 | `markdown_added_bytes` | `integer` | NO | `0` |
| `activity_event_outbox` | 1 | 10 | `markdown_deleted_bytes` | `integer` | NO | `0` |
| `activity_event_outbox` | 1 | 11 | `occurred_at` | `timestamp with time zone` | NO | `` |
| `activity_event_outbox` | 1 | 12 | `available_at` | `timestamp with time zone` | NO | `now()` |
| `activity_event_outbox` | 1 | 13 | `attempts` | `integer` | NO | `0` |
| `activity_event_outbox` | 1 | 14 | `last_error` | `text` | NO | `''::text` |
| `activity_event_outbox` | 1 | 15 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `activity_object_types` | 27 | 1 | `id` | `smallint` | NO | `` |
| `activity_object_types` | 27 | 2 | `code` | `text` | NO | `` |
| `activity_object_types` | 27 | 3 | `name` | `text` | NO | `` |
| `ai_task_logs` | 0 | 1 | `id` | `bigint` | NO | `nextval('ai_task_logs_id_seq'::regclass)` |
| `ai_task_logs` | 0 | 2 | `task_id` | `bigint` | YES | `` |
| `ai_task_logs` | 0 | 3 | `level` | `text` | NO | `'info'::text` |
| `ai_task_logs` | 0 | 4 | `event` | `text` | NO | `` |
| `ai_task_logs` | 0 | 5 | `message` | `text` | NO | `''::text` |
| `ai_task_logs` | 0 | 6 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `ai_task_logs` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `ai_tasks` | 0 | 1 | `id` | `bigint` | NO | `nextval('ai_tasks_id_seq'::regclass)` |
| `ai_tasks` | 0 | 2 | `task_uid` | `text` | NO | `` |
| `ai_tasks` | 0 | 3 | `task_type` | `text` | NO | `` |
| `ai_tasks` | 0 | 4 | `provider` | `text` | NO | `''::text` |
| `ai_tasks` | 0 | 5 | `model` | `text` | NO | `''::text` |
| `ai_tasks` | 0 | 6 | `status` | `text` | NO | `'queued'::text` |
| `ai_tasks` | 0 | 7 | `priority` | `integer` | NO | `0` |
| `ai_tasks` | 0 | 8 | `concurrency_key` | `text` | NO | `''::text` |
| `ai_tasks` | 0 | 9 | `input_tokens` | `bigint` | NO | `0` |
| `ai_tasks` | 0 | 10 | `output_tokens` | `bigint` | NO | `0` |
| `ai_tasks` | 0 | 11 | `cost_micros` | `bigint` | NO | `0` |
| `ai_tasks` | 0 | 12 | `quota_reserved_tokens` | `bigint` | NO | `0` |
| `ai_tasks` | 0 | 13 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `ai_tasks` | 0 | 14 | `result` | `jsonb` | NO | `'{}'::jsonb` |
| `ai_tasks` | 0 | 15 | `error` | `text` | NO | `''::text` |
| `ai_tasks` | 0 | 16 | `created_by` | `bigint` | YES | `` |
| `ai_tasks` | 0 | 17 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `ai_tasks` | 0 | 18 | `queued_at` | `timestamp with time zone` | YES | `` |
| `ai_tasks` | 0 | 19 | `started_at` | `timestamp with time zone` | YES | `` |
| `ai_tasks` | 0 | 20 | `finished_at` | `timestamp with time zone` | YES | `` |
| `ai_tasks` | 0 | 21 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_bot_rules` | 0 | 1 | `id` | `bigint` | NO | `nextval('anti_abuse_bot_rules_id_seq'::regclass)` |
| `anti_abuse_bot_rules` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `anti_abuse_bot_rules` | 0 | 3 | `kind` | `text` | NO | `` |
| `anti_abuse_bot_rules` | 0 | 4 | `label` | `text` | NO | `` |
| `anti_abuse_bot_rules` | 0 | 5 | `matcher` | `text` | NO | `''::text` |
| `anti_abuse_bot_rules` | 0 | 6 | `secret_hash` | `text` | NO | `''::text` |
| `anti_abuse_bot_rules` | 0 | 7 | `read_only` | `boolean` | NO | `true` |
| `anti_abuse_bot_rules` | 0 | 8 | `enabled` | `boolean` | NO | `true` |
| `anti_abuse_bot_rules` | 0 | 9 | `expires_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_bot_rules` | 0 | 10 | `created_by` | `bigint` | YES | `` |
| `anti_abuse_bot_rules` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_bot_rules` | 0 | 12 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_challenges` | 6 | 1 | `id` | `bigint` | NO | `nextval('anti_abuse_challenges_id_seq'::regclass)` |
| `anti_abuse_challenges` | 6 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `anti_abuse_challenges` | 6 | 3 | `user_id` | `bigint` | NO | `` |
| `anti_abuse_challenges` | 6 | 4 | `session_hash` | `text` | NO | `` |
| `anti_abuse_challenges` | 6 | 5 | `action` | `text` | NO | `` |
| `anti_abuse_challenges` | 6 | 6 | `object_key` | `text` | NO | `''::text` |
| `anti_abuse_challenges` | 6 | 7 | `kind` | `text` | NO | `` |
| `anti_abuse_challenges` | 6 | 8 | `provider` | `text` | NO | `` |
| `anti_abuse_challenges` | 6 | 9 | `token_hash` | `text` | NO | `` |
| `anti_abuse_challenges` | 6 | 10 | `answer_hash` | `text` | NO | `''::text` |
| `anti_abuse_challenges` | 6 | 11 | `status` | `text` | NO | `'pending'::text` |
| `anti_abuse_challenges` | 6 | 12 | `failure_count` | `integer` | NO | `0` |
| `anti_abuse_challenges` | 6 | 13 | `ip_hash` | `text` | NO | `''::text` |
| `anti_abuse_challenges` | 6 | 14 | `issued_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_challenges` | 6 | 15 | `expires_at` | `timestamp with time zone` | NO | `` |
| `anti_abuse_challenges` | 6 | 16 | `passed_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_challenges` | 6 | 17 | `consumed_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_challenges` | 6 | 18 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `anti_abuse_content_fingerprints` | 1 | 1 | `id` | `bigint` | NO | `nextval('anti_abuse_content_fingerprints_id_seq'::regclass)` |
| `anti_abuse_content_fingerprints` | 1 | 2 | `user_id` | `bigint` | YES | `` |
| `anti_abuse_content_fingerprints` | 1 | 3 | `action` | `text` | NO | `` |
| `anti_abuse_content_fingerprints` | 1 | 4 | `object_key` | `text` | NO | `''::text` |
| `anti_abuse_content_fingerprints` | 1 | 5 | `exact_hash` | `text` | NO | `` |
| `anti_abuse_content_fingerprints` | 1 | 6 | `simhash` | `bigint` | NO | `` |
| `anti_abuse_content_fingerprints` | 1 | 7 | `ip_hash` | `text` | NO | `''::text` |
| `anti_abuse_content_fingerprints` | 1 | 8 | `event_id` | `bigint` | YES | `` |
| `anti_abuse_content_fingerprints` | 1 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_daily_stats` | 4 | 1 | `stat_date` | `date` | NO | `` |
| `anti_abuse_daily_stats` | 4 | 2 | `action` | `text` | NO | `` |
| `anti_abuse_daily_stats` | 4 | 3 | `outcome` | `text` | NO | `` |
| `anti_abuse_daily_stats` | 4 | 4 | `crawler_class` | `text` | NO | `''::text` |
| `anti_abuse_daily_stats` | 4 | 5 | `event_count` | `bigint` | NO | `0` |
| `anti_abuse_events` | 6 | 1 | `id` | `bigint` | NO | `nextval('anti_abuse_events_id_seq'::regclass)` |
| `anti_abuse_events` | 6 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `anti_abuse_events` | 6 | 3 | `user_id` | `bigint` | YES | `` |
| `anti_abuse_events` | 6 | 4 | `action` | `text` | NO | `` |
| `anti_abuse_events` | 6 | 5 | `object_type` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 6 | `object_key` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 7 | `outcome` | `text` | NO | `` |
| `anti_abuse_events` | 6 | 8 | `risk_score` | `integer` | NO | `0` |
| `anti_abuse_events` | 6 | 9 | `rule_codes` | `ARRAY` | NO | `'{}'::text[]` |
| `anti_abuse_events` | 6 | 10 | `ip_hash` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 11 | `subnet_hash` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 12 | `device_hash` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 13 | `session_hash` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 14 | `crawler_class` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 15 | `content_hash` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 16 | `similar_event_id` | `bigint` | YES | `` |
| `anti_abuse_events` | 6 | 17 | `similarity` | `smallint` | NO | `0` |
| `anti_abuse_events` | 6 | 18 | `request_id` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 19 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `anti_abuse_events` | 6 | 20 | `disposition` | `text` | NO | `'unreviewed'::text` |
| `anti_abuse_events` | 6 | 21 | `review_note` | `text` | NO | `''::text` |
| `anti_abuse_events` | 6 | 22 | `reviewed_by` | `bigint` | YES | `` |
| `anti_abuse_events` | 6 | 23 | `reviewed_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_events` | 6 | 24 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_restrictions` | 0 | 1 | `id` | `bigint` | NO | `nextval('anti_abuse_restrictions_id_seq'::regclass)` |
| `anti_abuse_restrictions` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `anti_abuse_restrictions` | 0 | 3 | `user_id` | `bigint` | YES | `` |
| `anti_abuse_restrictions` | 0 | 4 | `ip_hash` | `text` | NO | `''::text` |
| `anti_abuse_restrictions` | 0 | 5 | `device_hash` | `text` | NO | `''::text` |
| `anti_abuse_restrictions` | 0 | 6 | `actions` | `ARRAY` | NO | `'{}'::text[]` |
| `anti_abuse_restrictions` | 0 | 7 | `mode` | `text` | NO | `` |
| `anti_abuse_restrictions` | 0 | 8 | `source` | `text` | NO | `` |
| `anti_abuse_restrictions` | 0 | 9 | `rule_code` | `text` | NO | `''::text` |
| `anti_abuse_restrictions` | 0 | 10 | `risk_score` | `integer` | NO | `0` |
| `anti_abuse_restrictions` | 0 | 11 | `reason` | `text` | NO | `''::text` |
| `anti_abuse_restrictions` | 0 | 12 | `automatic` | `boolean` | NO | `true` |
| `anti_abuse_restrictions` | 0 | 13 | `appeal_allowed` | `boolean` | NO | `true` |
| `anti_abuse_restrictions` | 0 | 14 | `starts_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_restrictions` | 0 | 15 | `ends_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_restrictions` | 0 | 16 | `lifted_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_restrictions` | 0 | 17 | `lifted_by` | `bigint` | YES | `` |
| `anti_abuse_restrictions` | 0 | 18 | `lift_reason` | `text` | NO | `''::text` |
| `anti_abuse_restrictions` | 0 | 19 | `created_by` | `bigint` | YES | `` |
| `anti_abuse_restrictions` | 0 | 20 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `anti_abuse_user_states` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `anti_abuse_user_states` | 0 | 2 | `trust_level` | `text` | NO | `'normal'::text` |
| `anti_abuse_user_states` | 0 | 3 | `risk_score` | `integer` | NO | `0` |
| `anti_abuse_user_states` | 0 | 4 | `hit_count` | `bigint` | NO | `0` |
| `anti_abuse_user_states` | 0 | 5 | `manually_trusted` | `boolean` | NO | `false` |
| `anti_abuse_user_states` | 0 | 6 | `challenge_required_until` | `timestamp with time zone` | YES | `` |
| `anti_abuse_user_states` | 0 | 7 | `review_required_until` | `timestamp with time zone` | YES | `` |
| `anti_abuse_user_states` | 0 | 8 | `restricted_until` | `timestamp with time zone` | YES | `` |
| `anti_abuse_user_states` | 0 | 9 | `last_event_at` | `timestamp with time zone` | YES | `` |
| `anti_abuse_user_states` | 0 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `app_logs` | 218 | 1 | `id` | `bigint` | NO | `nextval('app_logs_id_seq'::regclass)` |
| `app_logs` | 218 | 2 | `category` | `text` | NO | `` |
| `app_logs` | 218 | 3 | `level` | `text` | NO | `'info'::text` |
| `app_logs` | 218 | 4 | `actor_id` | `bigint` | YES | `` |
| `app_logs` | 218 | 5 | `action` | `text` | NO | `''::text` |
| `app_logs` | 218 | 6 | `target` | `text` | NO | `''::text` |
| `app_logs` | 218 | 7 | `ip` | `text` | NO | `''::text` |
| `app_logs` | 218 | 8 | `user_agent` | `text` | NO | `''::text` |
| `app_logs` | 218 | 9 | `method` | `text` | NO | `''::text` |
| `app_logs` | 218 | 10 | `path` | `text` | NO | `''::text` |
| `app_logs` | 218 | 11 | `status` | `integer` | NO | `0` |
| `app_logs` | 218 | 12 | `latency_ms` | `bigint` | NO | `0` |
| `app_logs` | 218 | 13 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `app_logs` | 218 | 14 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `audit_events` | 8 | 1 | `id` | `bigint` | NO | `nextval('audit_events_id_seq'::regclass)` |
| `audit_events` | 8 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `audit_events` | 8 | 3 | `entity_type` | `text` | YES | `` |
| `audit_events` | 8 | 4 | `entity_id` | `bigint` | YES | `` |
| `audit_events` | 8 | 5 | `aggregate_type` | `text` | NO | `` |
| `audit_events` | 8 | 6 | `aggregate_key` | `text` | NO | `` |
| `audit_events` | 8 | 7 | `actor_id` | `bigint` | YES | `` |
| `audit_events` | 8 | 8 | `actor_snapshot` | `text` | NO | `''::text` |
| `audit_events` | 8 | 9 | `action` | `text` | NO | `` |
| `audit_events` | 8 | 10 | `before_hash` | `text` | NO | `''::text` |
| `audit_events` | 8 | 11 | `after_hash` | `text` | NO | `''::text` |
| `audit_events` | 8 | 12 | `trace_id` | `text` | NO | `''::text` |
| `audit_events` | 8 | 13 | `ip` | `text` | NO | `''::text` |
| `audit_events` | 8 | 14 | `user_agent` | `text` | NO | `''::text` |
| `audit_events` | 8 | 15 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `audit_events` | 8 | 16 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `auth_sessions` | 4 | 1 | `id` | `bigint` | NO | `nextval('auth_sessions_id_seq'::regclass)` |
| `auth_sessions` | 4 | 2 | `session_hash` | `bytea` | NO | `` |
| `auth_sessions` | 4 | 3 | `user_id` | `bigint` | NO | `` |
| `auth_sessions` | 4 | 4 | `auth_version` | `bigint` | NO | `` |
| `auth_sessions` | 4 | 5 | `expires_at` | `timestamp with time zone` | NO | `` |
| `auth_sessions` | 4 | 6 | `revoked_at` | `timestamp with time zone` | YES | `` |
| `auth_sessions` | 4 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `ban_reasons` | 12 | 1 | `code` | `text` | NO | `` |
| `ban_reasons` | 12 | 2 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `ban_reasons` | 12 | 3 | `active` | `boolean` | NO | `true` |
| `ban_reasons` | 12 | 4 | `sort_order` | `integer` | NO | `0` |
| `ban_records` | 0 | 1 | `id` | `bigint` | NO | `nextval('ban_records_id_seq'::regclass)` |
| `ban_records` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `ban_records` | 0 | 3 | `user_id` | `bigint` | NO | `` |
| `ban_records` | 0 | 4 | `moderator_id` | `bigint` | NO | `` |
| `ban_records` | 0 | 5 | `report_id` | `bigint` | YES | `` |
| `ban_records` | 0 | 6 | `reason_code` | `text` | NO | `` |
| `ban_records` | 0 | 7 | `custom_reason` | `text` | NO | `''::text` |
| `ban_records` | 0 | 8 | `public_record_markdown` | `text` | NO | `''::text` |
| `ban_records` | 0 | 9 | `internal_note` | `text` | NO | `''::text` |
| `ban_records` | 0 | 10 | `username_snapshot` | `text` | NO | `` |
| `ban_records` | 0 | 11 | `avatar_snapshot` | `text` | NO | `''::text` |
| `ban_records` | 0 | 12 | `status` | `text` | NO | `'active'::text` |
| `ban_records` | 0 | 13 | `starts_at` | `timestamp with time zone` | NO | `now()` |
| `ban_records` | 0 | 14 | `ends_at` | `timestamp with time zone` | YES | `` |
| `ban_records` | 0 | 15 | `revoked_at` | `timestamp with time zone` | YES | `` |
| `ban_records` | 0 | 16 | `revoked_by` | `bigint` | YES | `` |
| `ban_records` | 0 | 17 | `revoke_reason` | `text` | NO | `''::text` |
| `ban_records` | 0 | 18 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `block_entity_model_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `block_entity_model_snapshots` | 0 | 2 | `resource_snapshot_id` | `text` | NO | `` |
| `block_entity_model_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `block_entity_model_snapshots` | 0 | 4 | `block_resource_id` | `bigint` | NO | `` |
| `block_entity_model_snapshots` | 0 | 5 | `block_id` | `text` | NO | `` |
| `block_entity_model_snapshots` | 0 | 6 | `block_entity_type_id` | `text` | NO | `` |
| `block_entity_model_snapshots` | 0 | 7 | `model_source` | `text` | NO | `''::text` |
| `block_entity_model_snapshots` | 0 | 8 | `model_available` | `boolean` | NO | `false` |
| `block_entity_model_snapshots` | 0 | 9 | `variant_count` | `integer` | NO | `0` |
| `block_entity_model_snapshots` | 0 | 10 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `block_entity_model_variants` | 0 | 1 | `id` | `text` | NO | `` |
| `block_entity_model_variants` | 0 | 2 | `model_snapshot_id` | `text` | NO | `` |
| `block_entity_model_variants` | 0 | 3 | `variant_id` | `text` | NO | `` |
| `block_entity_model_variants` | 0 | 4 | `obj_path` | `text` | NO | `''::text` |
| `block_entity_model_variants` | 0 | 5 | `mesh_path` | `text` | NO | `''::text` |
| `block_entity_model_variants` | 0 | 6 | `vertex_count` | `integer` | NO | `0` |
| `block_entity_model_variants` | 0 | 7 | `quad_count` | `integer` | NO | `0` |
| `block_entity_model_variants` | 0 | 8 | `coordinate_space` | `text` | NO | `'block_units'::text` |
| `block_entity_model_variants` | 0 | 9 | `uv_space` | `text` | NO | `''::text` |
| `block_entity_model_variants` | 0 | 10 | `uv_origin` | `text` | NO | `'bottom_left'::text` |
| `block_entity_model_variants` | 0 | 11 | `uv_complete` | `boolean` | NO | `false` |
| `block_entity_model_variants` | 0 | 12 | `textures` | `jsonb` | NO | `'[]'::jsonb` |
| `block_entity_model_variants` | 0 | 13 | `mesh_data` | `jsonb` | NO | `'{}'::jsonb` |
| `block_entity_model_variants` | 0 | 14 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `blueprint_jobs` | 0 | 1 | `id` | `bigint` | NO | `nextval('blueprint_jobs_id_seq'::regclass)` |
| `blueprint_jobs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `blueprint_jobs` | 0 | 3 | `blueprint_id` | `bigint` | NO | `` |
| `blueprint_jobs` | 0 | 4 | `operation` | `text` | NO | `` |
| `blueprint_jobs` | 0 | 5 | `target_format` | `text` | NO | `''::text` |
| `blueprint_jobs` | 0 | 6 | `status` | `text` | NO | `'queued'::text` |
| `blueprint_jobs` | 0 | 7 | `progress` | `integer` | NO | `0` |
| `blueprint_jobs` | 0 | 8 | `attempts` | `integer` | NO | `0` |
| `blueprint_jobs` | 0 | 9 | `last_error` | `text` | NO | `''::text` |
| `blueprint_jobs` | 0 | 10 | `created_by` | `bigint` | YES | `` |
| `blueprint_jobs` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `blueprint_jobs` | 0 | 12 | `started_at` | `timestamp with time zone` | YES | `` |
| `blueprint_jobs` | 0 | 13 | `finished_at` | `timestamp with time zone` | YES | `` |
| `blueprint_jobs` | 0 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `blueprint_materials` | 0 | 1 | `blueprint_id` | `bigint` | NO | `` |
| `blueprint_materials` | 0 | 2 | `block_state` | `text` | NO | `` |
| `blueprint_materials` | 0 | 3 | `block_id` | `text` | NO | `` |
| `blueprint_materials` | 0 | 4 | `properties` | `jsonb` | NO | `'{}'::jsonb` |
| `blueprint_materials` | 0 | 5 | `block_count` | `bigint` | NO | `` |
| `blueprint_mods` | 0 | 1 | `blueprint_id` | `bigint` | NO | `` |
| `blueprint_mods` | 0 | 2 | `source_namespace` | `text` | NO | `` |
| `blueprint_mods` | 0 | 3 | `mod_id` | `bigint` | NO | `` |
| `blueprint_mods` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `blueprint_variants` | 0 | 1 | `id` | `bigint` | NO | `nextval('blueprint_variants_id_seq'::regclass)` |
| `blueprint_variants` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `blueprint_variants` | 0 | 3 | `blueprint_id` | `bigint` | NO | `` |
| `blueprint_variants` | 0 | 4 | `format` | `text` | NO | `` |
| `blueprint_variants` | 0 | 5 | `file_id` | `bigint` | YES | `` |
| `blueprint_variants` | 0 | 6 | `object_key` | `text` | NO | `` |
| `blueprint_variants` | 0 | 7 | `original` | `boolean` | NO | `false` |
| `blueprint_variants` | 0 | 8 | `recommended` | `boolean` | NO | `false` |
| `blueprint_variants` | 0 | 9 | `status` | `text` | NO | `'ready'::text` |
| `blueprint_variants` | 0 | 10 | `original_name` | `text` | NO | `''::text` |
| `blueprint_variants` | 0 | 11 | `content_type` | `text` | NO | `'application/octet-stream'::text` |
| `blueprint_variants` | 0 | 12 | `size_bytes` | `bigint` | NO | `0` |
| `blueprint_variants` | 0 | 13 | `sha256` | `text` | NO | `''::text` |
| `blueprint_variants` | 0 | 14 | `created_by` | `bigint` | YES | `` |
| `blueprint_variants` | 0 | 15 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `blueprints` | 0 | 1 | `id` | `bigint` | NO | `nextval('blueprints_id_seq'::regclass)` |
| `blueprints` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `blueprints` | 0 | 3 | `owner_id` | `bigint` | NO | `` |
| `blueprints` | 0 | 4 | `title` | `text` | NO | `''::text` |
| `blueprints` | 0 | 5 | `description_markdown` | `text` | NO | `''::text` |
| `blueprints` | 0 | 6 | `source_format` | `text` | NO | `` |
| `blueprints` | 0 | 7 | `status` | `text` | NO | `'uploading'::text` |
| `blueprints` | 0 | 8 | `original_file_id` | `bigint` | YES | `` |
| `blueprints` | 0 | 9 | `cover_file_id` | `bigint` | YES | `` |
| `blueprints` | 0 | 10 | `original_object_key` | `text` | NO | `''::text` |
| `blueprints` | 0 | 11 | `cover_object_key` | `text` | NO | `''::text` |
| `blueprints` | 0 | 12 | `normalized_object_key` | `text` | NO | `''::text` |
| `blueprints` | 0 | 13 | `size_x` | `integer` | NO | `0` |
| `blueprints` | 0 | 14 | `size_y` | `integer` | NO | `0` |
| `blueprints` | 0 | 15 | `size_z` | `integer` | NO | `0` |
| `blueprints` | 0 | 16 | `block_count` | `bigint` | NO | `0` |
| `blueprints` | 0 | 17 | `palette_count` | `integer` | NO | `0` |
| `blueprints` | 0 | 18 | `entity_count` | `integer` | NO | `0` |
| `blueprints` | 0 | 19 | `data_version` | `integer` | NO | `0` |
| `blueprints` | 0 | 20 | `last_error` | `text` | NO | `''::text` |
| `blueprints` | 0 | 21 | `review_status` | `text` | NO | `'not_required'::text` |
| `blueprints` | 0 | 22 | `published_revision_id` | `bigint` | YES | `` |
| `blueprints` | 0 | 23 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `blueprints` | 0 | 24 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_entities` | 1 | 1 | `id` | `bigint` | NO | `nextval('catalog_entities_id_seq'::regclass)` |
| `catalog_entities` | 1 | 2 | `identity_key` | `text` | NO | `` |
| `catalog_entities` | 1 | 3 | `public_id` | `text` | NO | `new_public_id()` |
| `catalog_entities` | 1 | 4 | `entity_type` | `text` | NO | `` |
| `catalog_entities` | 1 | 5 | `status` | `text` | NO | `'active'::text` |
| `catalog_entities` | 1 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_entities` | 1 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_entities` | 1 | 8 | `default_locale` | `text` | NO | `'en-US'::text` |
| `catalog_entities` | 1 | 9 | `published_revision_id` | `bigint` | YES | `` |
| `catalog_entities` | 1 | 10 | `archived_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_binary_assets` | 0 | 1 | `id` | `text` | NO | `` |
| `catalog_import_binary_assets` | 0 | 2 | `revision_id` | `text` | NO | `` |
| `catalog_import_binary_assets` | 0 | 3 | `asset_path` | `text` | NO | `` |
| `catalog_import_binary_assets` | 0 | 4 | `asset_kind` | `text` | NO | `` |
| `catalog_import_binary_assets` | 0 | 5 | `content_type` | `text` | NO | `'application/octet-stream'::text` |
| `catalog_import_binary_assets` | 0 | 6 | `sha256` | `text` | NO | `` |
| `catalog_import_binary_assets` | 0 | 7 | `byte_length` | `bigint` | NO | `` |
| `catalog_import_binary_assets` | 0 | 8 | `data` | `bytea` | NO | `` |
| `catalog_import_capabilities` | 0 | 1 | `revision_id` | `text` | NO | `` |
| `catalog_import_capabilities` | 0 | 2 | `capability_id` | `text` | NO | `` |
| `catalog_import_capabilities` | 0 | 3 | `status` | `text` | NO | `` |
| `catalog_import_capabilities` | 0 | 4 | `source` | `text` | NO | `''::text` |
| `catalog_import_capabilities` | 0 | 5 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_job_logs` | 0 | 1 | `id` | `bigint` | NO | `nextval('catalog_import_job_logs_id_seq'::regclass)` |
| `catalog_import_job_logs` | 0 | 2 | `job_id` | `text` | NO | `` |
| `catalog_import_job_logs` | 0 | 3 | `level` | `text` | NO | `'info'::text` |
| `catalog_import_job_logs` | 0 | 4 | `stage` | `text` | NO | `''::text` |
| `catalog_import_job_logs` | 0 | 5 | `message` | `text` | NO | `''::text` |
| `catalog_import_job_logs` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_jobs` | 0 | 1 | `id` | `text` | NO | `` |
| `catalog_import_jobs` | 0 | 2 | `mod_id` | `bigint` | NO | `` |
| `catalog_import_jobs` | 0 | 3 | `package_id` | `text` | NO | `` |
| `catalog_import_jobs` | 0 | 4 | `target_version_id` | `bigint` | NO | `` |
| `catalog_import_jobs` | 0 | 5 | `overwrite_existing` | `boolean` | NO | `false` |
| `catalog_import_jobs` | 0 | 6 | `importer_version` | `text` | NO | `` |
| `catalog_import_jobs` | 0 | 7 | `status` | `text` | NO | `'queued'::text` |
| `catalog_import_jobs` | 0 | 8 | `progress` | `smallint` | NO | `0` |
| `catalog_import_jobs` | 0 | 9 | `current_stage` | `text` | NO | `''::text` |
| `catalog_import_jobs` | 0 | 10 | `error_code` | `text` | NO | `''::text` |
| `catalog_import_jobs` | 0 | 11 | `error_detail` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_jobs` | 0 | 12 | `created_by` | `bigint` | YES | `` |
| `catalog_import_jobs` | 0 | 13 | `detected_modids` | `jsonb` | NO | `'[]'::jsonb` |
| `catalog_import_jobs` | 0 | 14 | `configured_modids` | `ARRAY` | NO | `'{}'::text[]` |
| `catalog_import_jobs` | 0 | 15 | `primary_detected_modid` | `text` | NO | `''::text` |
| `catalog_import_jobs` | 0 | 16 | `modid_analysis_hash` | `text` | NO | `''::text` |
| `catalog_import_jobs` | 0 | 17 | `modid_confirmation_required` | `boolean` | NO | `false` |
| `catalog_import_jobs` | 0 | 18 | `modid_confirmed_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_jobs` | 0 | 19 | `modid_confirmed_by` | `bigint` | YES | `` |
| `catalog_import_jobs` | 0 | 20 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_jobs` | 0 | 21 | `started_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_jobs` | 0 | 22 | `finished_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_jobs` | 0 | 23 | `heartbeat_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_jobs` | 0 | 24 | `run_token` | `text` | NO | `''::text` |
| `catalog_import_jobs` | 0 | 25 | `attempt_count` | `integer` | NO | `0` |
| `catalog_import_jobs` | 0 | 26 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_locales` | 0 | 1 | `revision_id` | `text` | NO | `` |
| `catalog_import_locales` | 0 | 2 | `locale` | `text` | NO | `` |
| `catalog_import_locales` | 0 | 3 | `translation_count` | `integer` | NO | `0` |
| `catalog_import_media` | 0 | 1 | `revision_id` | `text` | NO | `` |
| `catalog_import_media` | 0 | 2 | `asset_path` | `text` | NO | `` |
| `catalog_import_media` | 0 | 3 | `media_kind` | `text` | NO | `` |
| `catalog_import_media` | 0 | 4 | `oss_file_id` | `bigint` | NO | `` |
| `catalog_import_media` | 0 | 5 | `sha256` | `text` | NO | `` |
| `catalog_import_media` | 0 | 6 | `content_type` | `text` | NO | `` |
| `catalog_import_media` | 0 | 7 | `byte_length` | `bigint` | NO | `` |
| `catalog_import_media` | 0 | 8 | `width` | `integer` | YES | `` |
| `catalog_import_media` | 0 | 9 | `height` | `integer` | YES | `` |
| `catalog_import_media` | 0 | 10 | `has_alpha` | `boolean` | YES | `` |
| `catalog_import_packages` | 0 | 1 | `id` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 2 | `sha256` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 3 | `archive_file_id` | `bigint` | YES | `` |
| `catalog_import_packages` | 0 | 4 | `archive_name` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 5 | `schema_version` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 6 | `exporter_version` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 7 | `minecraft_version` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 8 | `loader` | `text` | NO | `` |
| `catalog_import_packages` | 0 | 9 | `manifest` | `jsonb` | NO | `` |
| `catalog_import_packages` | 0 | 10 | `namespaces` | `ARRAY` | NO | `'{}'::text[]` |
| `catalog_import_packages` | 0 | 11 | `profile` | `text` | NO | `'all'::text` |
| `catalog_import_packages` | 0 | 12 | `uploaded_by` | `bigint` | YES | `` |
| `catalog_import_packages` | 0 | 13 | `uploaded_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_packages` | 0 | 14 | `imported_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_revision_stats` | 0 | 1 | `revision_id` | `text` | NO | `` |
| `catalog_import_revision_stats` | 0 | 2 | `registry_counts` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_revision_stats` | 0 | 3 | `document_counts` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_revision_stats` | 0 | 4 | `capability_statuses` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_revision_stats` | 0 | 5 | `asset_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 6 | `structure_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 7 | `advancement_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 8 | `key_mapping_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 9 | `recipe_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 10 | `tag_count` | `integer` | NO | `0` |
| `catalog_import_revision_stats` | 0 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_revisions` | 0 | 1 | `id` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 2 | `mod_id` | `bigint` | NO | `` |
| `catalog_import_revisions` | 0 | 3 | `package_id` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 4 | `job_id` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 5 | `target_version_id` | `bigint` | NO | `` |
| `catalog_import_revisions` | 0 | 6 | `revision_no` | `bigint` | NO | `` |
| `catalog_import_revisions` | 0 | 7 | `status` | `text` | NO | `'staging'::text` |
| `catalog_import_revisions` | 0 | 8 | `minecraft_version` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 9 | `loader` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 10 | `exporter_version` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 11 | `source_namespace` | `text` | NO | `` |
| `catalog_import_revisions` | 0 | 12 | `source_kind` | `text` | NO | `'mcmods_exporter'::text` |
| `catalog_import_revisions` | 0 | 13 | `source_metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_revisions` | 0 | 14 | `import_run_token` | `text` | NO | `''::text` |
| `catalog_import_revisions` | 0 | 15 | `submitted_by` | `bigint` | YES | `` |
| `catalog_import_revisions` | 0 | 16 | `submitted_by_snapshot` | `text` | NO | `''::text` |
| `catalog_import_revisions` | 0 | 17 | `is_active` | `boolean` | NO | `false` |
| `catalog_import_revisions` | 0 | 18 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_import_revisions` | 0 | 19 | `activated_at` | `timestamp with time zone` | YES | `` |
| `catalog_import_structures` | 0 | 1 | `id` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 2 | `revision_id` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 3 | `structure_id` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 4 | `asset_path` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 5 | `source_format` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 6 | `template_blob_id` | `text` | NO | `` |
| `catalog_import_structures` | 0 | 7 | `summary` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_import_text_assets` | 0 | 1 | `revision_id` | `text` | NO | `` |
| `catalog_import_text_assets` | 0 | 2 | `asset_path` | `text` | NO | `` |
| `catalog_import_text_assets` | 0 | 3 | `asset_kind` | `text` | NO | `` |
| `catalog_import_text_assets` | 0 | 4 | `content_type` | `text` | NO | `` |
| `catalog_import_text_assets` | 0 | 5 | `sha256` | `text` | NO | `` |
| `catalog_import_text_assets` | 0 | 6 | `byte_length` | `bigint` | NO | `` |
| `catalog_import_text_assets` | 0 | 7 | `text_content` | `text` | YES | `` |
| `catalog_import_text_assets` | 0 | 8 | `json_content` | `jsonb` | YES | `` |
| `catalog_resource_definitions` | 0 | 1 | `resource_id` | `bigint` | NO | `` |
| `catalog_resource_definitions` | 0 | 2 | `definition_schema_version` | `smallint` | NO | `1` |
| `catalog_resource_definitions` | 0 | 3 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `catalog_resource_definitions` | 0 | 4 | `icon_file_id` | `bigint` | YES | `` |
| `catalog_resource_definitions` | 0 | 5 | `render_file_id` | `bigint` | YES | `` |
| `catalog_resource_definitions` | 0 | 6 | `published_revision_id` | `bigint` | YES | `` |
| `catalog_resource_definitions` | 0 | 7 | `updated_by` | `bigint` | YES | `` |
| `catalog_resource_definitions` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_resource_definitions` | 0 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `catalog_tag_members` | 0 | 1 | `tag_id` | `bigint` | NO | `` |
| `catalog_tag_members` | 0 | 2 | `resource_id` | `bigint` | NO | `` |
| `catalog_tag_members` | 0 | 3 | `ordinal` | `integer` | NO | `` |
| `catalog_tag_members` | 0 | 4 | `published_revision_id` | `bigint` | YES | `` |
| `catalog_tags` | 0 | 1 | `entity_id` | `bigint` | NO | `` |
| `catalog_tags` | 0 | 2 | `registry` | `text` | NO | `` |
| `catalog_tags` | 0 | 3 | `canonical_id` | `text` | NO | `` |
| `change_requests` | 4 | 1 | `id` | `bigint` | NO | `nextval('change_requests_id_seq'::regclass)` |
| `change_requests` | 4 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `change_requests` | 4 | 3 | `entity_type` | `text` | YES | `` |
| `change_requests` | 4 | 4 | `entity_id` | `bigint` | YES | `` |
| `change_requests` | 4 | 5 | `aggregate_type` | `text` | NO | `` |
| `change_requests` | 4 | 6 | `aggregate_key` | `text` | NO | `` |
| `change_requests` | 4 | 7 | `base_revision_id` | `bigint` | YES | `` |
| `change_requests` | 4 | 8 | `proposed_revision_id` | `bigint` | NO | `` |
| `change_requests` | 4 | 9 | `status` | `text` | NO | `'pending'::text` |
| `change_requests` | 4 | 10 | `reason` | `text` | NO | `''::text` |
| `change_requests` | 4 | 11 | `submitted_by` | `bigint` | YES | `` |
| `change_requests` | 4 | 12 | `submitted_by_snapshot` | `text` | NO | `''::text` |
| `change_requests` | 4 | 13 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `change_requests` | 4 | 14 | `submitted_at` | `timestamp with time zone` | NO | `now()` |
| `change_requests` | 4 | 15 | `resolved_at` | `timestamp with time zone` | YES | `` |
| `comment_attachments` | 0 | 1 | `comment_id` | `bigint` | NO | `` |
| `comment_attachments` | 0 | 2 | `attachment_file_id` | `bigint` | NO | `` |
| `comment_attachments` | 0 | 3 | `kind` | `text` | NO | `'file'::text` |
| `comment_attachments` | 0 | 4 | `processing_status` | `text` | NO | `'processing'::text` |
| `comment_attachments` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comment_closure` | 1 | 1 | `ancestor_id` | `bigint` | NO | `` |
| `comment_closure` | 1 | 2 | `descendant_id` | `bigint` | NO | `` |
| `comment_closure` | 1 | 3 | `depth` | `integer` | NO | `` |
| `comment_floor_counters` | 1 | 1 | `target_type` | `text` | NO | `` |
| `comment_floor_counters` | 1 | 2 | `target_id` | `bigint` | NO | `` |
| `comment_floor_counters` | 1 | 3 | `target_version_key` | `bigint` | NO | `0` |
| `comment_floor_counters` | 1 | 4 | `last_floor` | `bigint` | NO | `0` |
| `comment_floor_counters` | 1 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `comment_heat_refresh_queue` | 1 | 1 | `comment_id` | `bigint` | NO | `` |
| `comment_heat_refresh_queue` | 1 | 2 | `attempts` | `integer` | NO | `0` |
| `comment_heat_refresh_queue` | 1 | 3 | `available_at` | `timestamp with time zone` | NO | `now()` |
| `comment_heat_refresh_queue` | 1 | 4 | `locked_at` | `timestamp with time zone` | YES | `` |
| `comment_heat_refresh_queue` | 1 | 5 | `last_error` | `text` | NO | `''::text` |
| `comment_heat_refresh_queue` | 1 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `comment_log_bindings` | 0 | 1 | `comment_id` | `bigint` | NO | `` |
| `comment_log_bindings` | 0 | 2 | `attachment_file_id` | `bigint` | NO | `` |
| `comment_log_bindings` | 0 | 3 | `log_share_id` | `bigint` | NO | `` |
| `comment_log_bindings` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comment_reactions` | 2 | 1 | `comment_id` | `bigint` | NO | `` |
| `comment_reactions` | 2 | 2 | `user_id` | `bigint` | NO | `` |
| `comment_reactions` | 2 | 3 | `reaction` | `text` | NO | `` |
| `comment_reactions` | 2 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comment_watch_replies` | 0 | 1 | `watch_id` | `bigint` | NO | `` |
| `comment_watch_replies` | 0 | 2 | `comment_id` | `bigint` | NO | `` |
| `comment_watch_replies` | 0 | 3 | `notification_id` | `bigint` | YES | `` |
| `comment_watch_replies` | 0 | 4 | `read_at` | `timestamp with time zone` | YES | `` |
| `comment_watch_replies` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comment_watches` | 0 | 1 | `id` | `bigint` | NO | `nextval('comment_watches_id_seq'::regclass)` |
| `comment_watches` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `comment_watches` | 0 | 3 | `user_id` | `bigint` | NO | `` |
| `comment_watches` | 0 | 4 | `comment_id` | `bigint` | NO | `` |
| `comment_watches` | 0 | 5 | `status` | `text` | NO | `'active'::text` |
| `comment_watches` | 0 | 6 | `muted_until` | `timestamp with time zone` | YES | `` |
| `comment_watches` | 0 | 7 | `muted_forever` | `boolean` | NO | `false` |
| `comment_watches` | 0 | 8 | `unread_count` | `integer` | NO | `0` |
| `comment_watches` | 0 | 9 | `watched_reply_count` | `integer` | NO | `0` |
| `comment_watches` | 0 | 10 | `last_activity_at` | `timestamp with time zone` | NO | `now()` |
| `comment_watches` | 0 | 11 | `last_read_comment_id` | `bigint` | YES | `` |
| `comment_watches` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comment_watches` | 0 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `comment_watches` | 0 | 14 | `cancelled_at` | `timestamp with time zone` | YES | `` |
| `comments` | 1 | 1 | `id` | `bigint` | NO | `nextval('comments_id_seq'::regclass)` |
| `comments` | 1 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `comments` | 1 | 3 | `target_type` | `text` | NO | `` |
| `comments` | 1 | 4 | `target_id` | `bigint` | NO | `` |
| `comments` | 1 | 5 | `target_version_id` | `bigint` | YES | `` |
| `comments` | 1 | 6 | `author_id` | `bigint` | NO | `` |
| `comments` | 1 | 7 | `parent_id` | `bigint` | YES | `` |
| `comments` | 1 | 8 | `root_id` | `bigint` | YES | `` |
| `comments` | 1 | 9 | `depth` | `integer` | NO | `0` |
| `comments` | 1 | 10 | `body` | `text` | NO | `` |
| `comments` | 1 | 11 | `status` | `text` | NO | `'published'::text` |
| `comments` | 1 | 12 | `child_count` | `integer` | NO | `0` |
| `comments` | 1 | 13 | `descendant_count` | `integer` | NO | `0` |
| `comments` | 1 | 14 | `like_count` | `integer` | NO | `0` |
| `comments` | 1 | 15 | `unique_reply_users` | `integer` | NO | `0` |
| `comments` | 1 | 16 | `watch_count` | `integer` | NO | `0` |
| `comments` | 1 | 17 | `quality_score` | `numeric` | NO | `0` |
| `comments` | 1 | 18 | `hot_score` | `numeric` | NO | `0` |
| `comments` | 1 | 19 | `last_reply_at` | `timestamp with time zone` | YES | `` |
| `comments` | 1 | 20 | `idempotency_key` | `text` | NO | `''::text` |
| `comments` | 1 | 21 | `pinned_at` | `timestamp with time zone` | YES | `` |
| `comments` | 1 | 22 | `pinned_by` | `bigint` | YES | `` |
| `comments` | 1 | 23 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `comments` | 1 | 24 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `comments` | 1 | 25 | `deleted_at` | `timestamp with time zone` | YES | `` |
| `comments` | 1 | 26 | `floor_number` | `bigint` | YES | `` |
| `community_post_bounties` | 0 | 1 | `post_id` | `bigint` | NO | `` |
| `community_post_bounties` | 0 | 2 | `currency_id` | `bigint` | NO | `` |
| `community_post_bounties` | 0 | 3 | `amount` | `bigint` | NO | `` |
| `community_post_bounties` | 0 | 4 | `status` | `text` | NO | `'held'::text` |
| `community_post_bounties` | 0 | 5 | `recipient_id` | `bigint` | YES | `` |
| `community_post_bounties` | 0 | 6 | `tax_amount` | `bigint` | NO | `0` |
| `community_post_bounties` | 0 | 7 | `net_amount` | `bigint` | NO | `0` |
| `community_post_bounties` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `community_post_bounties` | 0 | 9 | `settled_at` | `timestamp with time zone` | YES | `` |
| `community_post_project_refs` | 0 | 1 | `id` | `bigint` | NO | `nextval('community_post_project_refs_id_seq'::regclass)` |
| `community_post_project_refs` | 0 | 2 | `post_id` | `bigint` | NO | `` |
| `community_post_project_refs` | 0 | 3 | `target_type` | `text` | NO | `` |
| `community_post_project_refs` | 0 | 4 | `target_id` | `bigint` | YES | `` |
| `community_post_project_refs` | 0 | 5 | `raw_identifier` | `text` | NO | `''::text` |
| `community_post_project_refs` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `community_post_resource_refs` | 0 | 1 | `id` | `bigint` | NO | `nextval('community_post_resource_refs_id_seq'::regclass)` |
| `community_post_resource_refs` | 0 | 2 | `post_id` | `bigint` | NO | `` |
| `community_post_resource_refs` | 0 | 3 | `resource_id` | `bigint` | YES | `` |
| `community_post_resource_refs` | 0 | 4 | `kind_code` | `text` | NO | `` |
| `community_post_resource_refs` | 0 | 5 | `raw_resource_id` | `text` | NO | `''::text` |
| `community_post_resource_refs` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `community_post_translations` | 0 | 1 | `post_id` | `bigint` | NO | `` |
| `community_post_translations` | 0 | 2 | `locale` | `text` | NO | `` |
| `community_post_translations` | 0 | 3 | `title` | `text` | NO | `` |
| `community_post_translations` | 0 | 4 | `body_markdown` | `text` | NO | `''::text` |
| `community_post_translations` | 0 | 5 | `source_revision_id` | `bigint` | NO | `` |
| `community_post_translations` | 0 | 6 | `ai_task_id` | `bigint` | YES | `` |
| `community_post_translations` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `community_post_translations` | 0 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `community_posts` | 0 | 1 | `id` | `bigint` | NO | `nextval('community_posts_id_seq'::regclass)` |
| `community_posts` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `community_posts` | 0 | 3 | `kind` | `text` | NO | `` |
| `community_posts` | 0 | 4 | `category` | `text` | NO | `` |
| `community_posts` | 0 | 5 | `author_id` | `bigint` | NO | `` |
| `community_posts` | 0 | 6 | `title` | `text` | NO | `` |
| `community_posts` | 0 | 7 | `source_locale` | `text` | NO | `` |
| `community_posts` | 0 | 8 | `body_markdown` | `text` | NO | `''::text` |
| `community_posts` | 0 | 9 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `community_posts` | 0 | 10 | `mod_version_min` | `text` | NO | `''::text` |
| `community_posts` | 0 | 11 | `mod_version_max` | `text` | NO | `''::text` |
| `community_posts` | 0 | 12 | `severity` | `text` | NO | `''::text` |
| `community_posts` | 0 | 13 | `has_fix` | `boolean` | NO | `false` |
| `community_posts` | 0 | 14 | `issue_url` | `text` | NO | `''::text` |
| `community_posts` | 0 | 15 | `cover_file_id` | `bigint` | YES | `` |
| `community_posts` | 0 | 16 | `resolution_status` | `text` | NO | `'open'::text` |
| `community_posts` | 0 | 17 | `accepted_comment_id` | `bigint` | YES | `` |
| `community_posts` | 0 | 18 | `resolved_at` | `timestamp with time zone` | YES | `` |
| `community_posts` | 0 | 19 | `status` | `text` | NO | `'active'::text` |
| `community_posts` | 0 | 20 | `review_status` | `text` | NO | `'pending'::text` |
| `community_posts` | 0 | 21 | `published_revision_id` | `bigint` | YES | `` |
| `community_posts` | 0 | 22 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `community_posts` | 0 | 23 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `community_posts` | 0 | 24 | `published_at` | `timestamp with time zone` | YES | `` |
| `content_change_items` | 9 | 1 | `id` | `bigint` | NO | `nextval('content_change_items_id_seq'::regclass)` |
| `content_change_items` | 9 | 2 | `revision_id` | `bigint` | NO | `` |
| `content_change_items` | 9 | 3 | `path` | `text` | NO | `` |
| `content_change_items` | 9 | 4 | `operation` | `text` | NO | `` |
| `content_change_items` | 9 | 5 | `before_value` | `jsonb` | YES | `` |
| `content_change_items` | 9 | 6 | `after_value` | `jsonb` | YES | `` |
| `content_creator_bindings` | 0 | 1 | `id` | `bigint` | NO | `nextval('content_creator_bindings_id_seq'::regclass)` |
| `content_creator_bindings` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `content_creator_bindings` | 0 | 3 | `subject_type` | `text` | NO | `` |
| `content_creator_bindings` | 0 | 4 | `subject_id` | `bigint` | NO | `` |
| `content_creator_bindings` | 0 | 5 | `creator_id` | `bigint` | NO | `` |
| `content_creator_bindings` | 0 | 6 | `role_id` | `bigint` | YES | `` |
| `content_creator_bindings` | 0 | 7 | `name_snapshot` | `text` | NO | `''::text` |
| `content_creator_bindings` | 0 | 8 | `role_snapshot` | `text` | NO | `''::text` |
| `content_creator_bindings` | 0 | 9 | `status` | `text` | NO | `'pending'::text` |
| `content_creator_bindings` | 0 | 10 | `permission_granting` | `boolean` | NO | `false` |
| `content_creator_bindings` | 0 | 11 | `approved_by` | `bigint` | YES | `` |
| `content_creator_bindings` | 0 | 12 | `approved_at` | `timestamp with time zone` | YES | `` |
| `content_creator_bindings` | 0 | 13 | `display_order` | `integer` | NO | `0` |
| `content_creator_bindings` | 0 | 14 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_download_counters` | 0 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_download_counters` | 0 | 2 | `owner_id` | `bigint` | NO | `` |
| `content_download_counters` | 0 | 3 | `downloads` | `bigint` | NO | `0` |
| `content_download_counters` | 0 | 4 | `last_download_at` | `timestamp with time zone` | YES | `` |
| `content_download_reward_counters` | 0 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_download_reward_counters` | 0 | 2 | `owner_id` | `bigint` | NO | `` |
| `content_download_reward_counters` | 0 | 3 | `currency_id` | `bigint` | NO | `` |
| `content_download_reward_counters` | 0 | 4 | `rewarded_steps` | `bigint` | NO | `0` |
| `content_download_reward_counters` | 0 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_heat_promotions` | 0 | 1 | `id` | `bigint` | NO | `nextval('content_heat_promotions_id_seq'::regclass)` |
| `content_heat_promotions` | 0 | 2 | `object_route_id` | `bigint` | NO | `` |
| `content_heat_promotions` | 0 | 3 | `shop_item_id` | `bigint` | NO | `` |
| `content_heat_promotions` | 0 | 4 | `applied_by` | `bigint` | NO | `` |
| `content_heat_promotions` | 0 | 5 | `promotion_kind` | `text` | NO | `` |
| `content_heat_promotions` | 0 | 6 | `base_power` | `numeric` | NO | `` |
| `content_heat_promotions` | 0 | 7 | `effective_power` | `numeric` | NO | `` |
| `content_heat_promotions` | 0 | 8 | `half_life_hours` | `integer` | NO | `` |
| `content_heat_promotions` | 0 | 9 | `sequence_no` | `integer` | NO | `` |
| `content_heat_promotions` | 0 | 10 | `started_at` | `timestamp with time zone` | NO | `now()` |
| `content_heat_promotions` | 0 | 11 | `expires_at` | `timestamp with time zone` | NO | `` |
| `content_localizations` | 1 | 1 | `subject_type` | `text` | NO | `` |
| `content_localizations` | 1 | 2 | `subject_id` | `bigint` | NO | `` |
| `content_localizations` | 1 | 3 | `catalog_entity_id` | `bigint` | YES | `` |
| `content_localizations` | 1 | 4 | `locale` | `text` | NO | `` |
| `content_localizations` | 1 | 5 | `name` | `text` | NO | `''::text` |
| `content_localizations` | 1 | 6 | `summary` | `text` | NO | `''::text` |
| `content_localizations` | 1 | 7 | `content_markdown` | `text` | NO | `''::text` |
| `content_localizations` | 1 | 8 | `provenance` | `text` | NO | `'human'::text` |
| `content_localizations` | 1 | 9 | `source_locale` | `text` | NO | `''::text` |
| `content_localizations` | 1 | 10 | `ai_task_id` | `bigint` | YES | `` |
| `content_localizations` | 1 | 11 | `revision_no` | `bigint` | NO | `1` |
| `content_localizations` | 1 | 12 | `editable` | `boolean` | NO | `true` |
| `content_localizations` | 1 | 13 | `review_status` | `text` | NO | `'approved'::text` |
| `content_localizations` | 1 | 14 | `published_revision_id` | `bigint` | YES | `` |
| `content_localizations` | 1 | 15 | `updated_by` | `bigint` | YES | `` |
| `content_localizations` | 1 | 16 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_localizations` | 1 | 17 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_popularity_daily_snapshots` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_popularity_daily_snapshots` | 1 | 2 | `metric_date` | `date` | NO | `CURRENT_DATE` |
| `content_popularity_daily_snapshots` | 1 | 3 | `heat_score` | `numeric` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 4 | `view_count` | `bigint` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 5 | `download_count` | `bigint` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 6 | `favorite_count` | `bigint` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 7 | `comment_count` | `bigint` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 8 | `rating_count` | `bigint` | NO | `0` |
| `content_popularity_daily_snapshots` | 1 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_popularity_events_daily` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_popularity_events_daily` | 1 | 2 | `event_date` | `date` | NO | `CURRENT_DATE` |
| `content_popularity_events_daily` | 1 | 3 | `favorite_value` | `numeric` | NO | `0` |
| `content_popularity_events_daily` | 1 | 4 | `comment_value` | `numeric` | NO | `0` |
| `content_popularity_events_daily` | 1 | 5 | `rating_value` | `numeric` | NO | `0` |
| `content_popularity_events_daily` | 1 | 6 | `release_value` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_popularity_stats` | 1 | 2 | `view_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 3 | `unique_view_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 4 | `page_count` | `integer` | NO | `1` |
| `content_popularity_stats` | 1 | 5 | `download_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 6 | `favorite_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 7 | `comment_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 8 | `effective_commenter_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 9 | `rating_count` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 10 | `rating_sum` | `bigint` | NO | `0` |
| `content_popularity_stats` | 1 | 11 | `rating_average` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 12 | `bayesian_rating` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 13 | `dimension_averages` | `jsonb` | NO | `'{}'::jsonb` |
| `content_popularity_stats` | 1 | 14 | `long_term_score` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 15 | `trend_score` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 16 | `effective_view_score` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 17 | `promotion_score` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 18 | `quality_modifier` | `numeric` | NO | `1` |
| `content_popularity_stats` | 1 | 19 | `new_project_boost` | `numeric` | NO | `1` |
| `content_popularity_stats` | 1 | 20 | `heat_score` | `numeric` | NO | `0` |
| `content_popularity_stats` | 1 | 21 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_popularity_thresholds` | 9 | 1 | `entity_type` | `text` | NO | `` |
| `content_popularity_thresholds` | 9 | 2 | `favorite_threshold` | `numeric` | NO | `` |
| `content_popularity_thresholds` | 9 | 3 | `commenter_threshold` | `numeric` | NO | `` |
| `content_popularity_thresholds` | 9 | 4 | `download_threshold` | `numeric` | NO | `` |
| `content_popularity_thresholds` | 9 | 5 | `rating_threshold` | `numeric` | NO | `` |
| `content_popularity_thresholds` | 9 | 6 | `view_threshold` | `numeric` | NO | `` |
| `content_popularity_thresholds` | 9 | 7 | `trend_threshold` | `numeric` | NO | `` |
| `content_project_pages` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_project_pages` | 1 | 2 | `page_hash` | `bytea` | NO | `` |
| `content_project_pages` | 1 | 3 | `first_seen_at` | `timestamp with time zone` | NO | `now()` |
| `content_rating_global_stats` | 9 | 1 | `entity_type` | `text` | NO | `` |
| `content_rating_global_stats` | 9 | 2 | `rating_count` | `bigint` | NO | `0` |
| `content_rating_global_stats` | 9 | 3 | `rating_sum` | `bigint` | NO | `0` |
| `content_rating_global_stats` | 9 | 4 | `average_rating` | `numeric` | NO | `3.5` |
| `content_rating_global_stats` | 9 | 5 | `updated_at` | `timestamp with time zone` | NO | `'-infinity'::timestamp with time zone` |
| `content_rating_scores` | 8 | 1 | `rating_id` | `bigint` | NO | `` |
| `content_rating_scores` | 8 | 2 | `dimension_code` | `text` | NO | `` |
| `content_rating_scores` | 8 | 3 | `score` | `smallint` | NO | `` |
| `content_ratings` | 1 | 1 | `id` | `bigint` | NO | `nextval('content_ratings_id_seq'::regclass)` |
| `content_ratings` | 1 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `content_ratings` | 1 | 3 | `object_route_id` | `bigint` | NO | `` |
| `content_ratings` | 1 | 4 | `author_id` | `bigint` | NO | `` |
| `content_ratings` | 1 | 5 | `overall_score` | `smallint` | NO | `` |
| `content_ratings` | 1 | 6 | `message` | `text` | NO | `''::text` |
| `content_ratings` | 1 | 7 | `status` | `text` | NO | `'published'::text` |
| `content_ratings` | 1 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_ratings` | 1 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_revisions` | 4 | 1 | `id` | `bigint` | NO | `nextval('content_revisions_id_seq'::regclass)` |
| `content_revisions` | 4 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `content_revisions` | 4 | 3 | `entity_type` | `text` | YES | `` |
| `content_revisions` | 4 | 4 | `entity_id` | `bigint` | YES | `` |
| `content_revisions` | 4 | 5 | `aggregate_type` | `text` | NO | `` |
| `content_revisions` | 4 | 6 | `aggregate_key` | `text` | NO | `` |
| `content_revisions` | 4 | 7 | `revision_no` | `bigint` | NO | `` |
| `content_revisions` | 4 | 8 | `base_revision_id` | `bigint` | YES | `` |
| `content_revisions` | 4 | 9 | `schema_version` | `integer` | NO | `1` |
| `content_revisions` | 4 | 10 | `snapshot` | `jsonb` | NO | `` |
| `content_revisions` | 4 | 11 | `snapshot_hash` | `text` | NO | `` |
| `content_revisions` | 4 | 12 | `created_by` | `bigint` | YES | `` |
| `content_revisions` | 4 | 13 | `created_by_snapshot` | `text` | NO | `''::text` |
| `content_revisions` | 4 | 14 | `source` | `text` | NO | `'user'::text` |
| `content_revisions` | 4 | 15 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_route_metrics` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_route_metrics` | 1 | 2 | `direct_view_count` | `bigint` | NO | `0` |
| `content_route_metrics` | 1 | 3 | `child_view_count` | `bigint` | NO | `0` |
| `content_route_metrics` | 1 | 4 | `total_view_count` | `bigint` | NO | `0` |
| `content_route_metrics` | 1 | 5 | `edit_count` | `bigint` | NO | `0` |
| `content_route_metrics` | 1 | 6 | `created_at` | `timestamp with time zone` | NO | `` |
| `content_route_metrics` | 1 | 7 | `last_edited_at` | `timestamp with time zone` | YES | `` |
| `content_route_metrics` | 1 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_stats_refresh_queue` | 3 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_stats_refresh_queue` | 3 | 2 | `refresh_metrics` | `boolean` | NO | `true` |
| `content_stats_refresh_queue` | 3 | 3 | `refresh_popularity` | `boolean` | NO | `false` |
| `content_stats_refresh_queue` | 3 | 4 | `attempts` | `integer` | NO | `0` |
| `content_stats_refresh_queue` | 3 | 5 | `available_at` | `timestamp with time zone` | NO | `now()` |
| `content_stats_refresh_queue` | 3 | 6 | `locked_at` | `timestamp with time zone` | YES | `` |
| `content_stats_refresh_queue` | 3 | 7 | `last_error` | `text` | NO | `''::text` |
| `content_stats_refresh_queue` | 3 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_stats_refresh_queue` | 3 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_subjects` | 55 | 1 | `subject_type` | `text` | NO | `` |
| `content_subjects` | 55 | 2 | `subject_id` | `bigint` | NO | `` |
| `content_subjects` | 55 | 3 | `default_locale` | `text` | NO | `'en-US'::text` |
| `content_subjects` | 55 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `content_subjects` | 55 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `content_unique_views` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_unique_views` | 1 | 2 | `viewer_hash` | `bytea` | NO | `` |
| `content_unique_views` | 1 | 3 | `viewer_user_id` | `bigint` | YES | `` |
| `content_unique_views` | 1 | 4 | `first_seen_at` | `timestamp with time zone` | NO | `now()` |
| `content_unique_views` | 1 | 5 | `last_seen_at` | `timestamp with time zone` | NO | `now()` |
| `content_view_daily` | 1 | 1 | `object_route_id` | `bigint` | NO | `` |
| `content_view_daily` | 1 | 2 | `view_date` | `date` | NO | `CURRENT_DATE` |
| `content_view_daily` | 1 | 3 | `counter_shard` | `smallint` | NO | `0` |
| `content_view_daily` | 1 | 4 | `views` | `bigint` | NO | `0` |
| `creator_claim_attachments` | 0 | 1 | `claim_id` | `bigint` | NO | `` |
| `creator_claim_attachments` | 0 | 2 | `oss_file_id` | `bigint` | NO | `` |
| `creator_claim_attachments` | 0 | 3 | `display_order` | `integer` | NO | `0` |
| `creator_claim_attachments` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creator_claims` | 2 | 1 | `id` | `bigint` | NO | `nextval('creator_claims_id_seq'::regclass)` |
| `creator_claims` | 2 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `creator_claims` | 2 | 3 | `creator_id` | `bigint` | NO | `` |
| `creator_claims` | 2 | 4 | `user_id` | `bigint` | NO | `` |
| `creator_claims` | 2 | 5 | `proof_markdown` | `text` | NO | `''::text` |
| `creator_claims` | 2 | 6 | `status` | `text` | NO | `'pending'::text` |
| `creator_claims` | 2 | 7 | `reviewed_by` | `bigint` | YES | `` |
| `creator_claims` | 2 | 8 | `review_note` | `text` | NO | `''::text` |
| `creator_claims` | 2 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creator_claims` | 2 | 10 | `reviewed_at` | `timestamp with time zone` | YES | `` |
| `creator_claims` | 2 | 11 | `revoked_at` | `timestamp with time zone` | YES | `` |
| `creator_links` | 0 | 1 | `id` | `bigint` | NO | `nextval('creator_links_id_seq'::regclass)` |
| `creator_links` | 0 | 2 | `creator_id` | `bigint` | NO | `` |
| `creator_links` | 0 | 3 | `link_type` | `text` | NO | `` |
| `creator_links` | 0 | 4 | `url` | `text` | NO | `` |
| `creator_links` | 0 | 5 | `label` | `text` | NO | `''::text` |
| `creator_links` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `creator_links` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creator_role_definitions` | 9 | 1 | `id` | `bigint` | NO | `nextval('creator_role_definitions_id_seq'::regclass)` |
| `creator_role_definitions` | 9 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `creator_role_definitions` | 9 | 3 | `code` | `text` | NO | `` |
| `creator_role_definitions` | 9 | 4 | `name` | `text` | NO | `` |
| `creator_role_definitions` | 9 | 5 | `description` | `text` | NO | `''::text` |
| `creator_role_definitions` | 9 | 6 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `creator_role_definitions` | 9 | 7 | `is_custom` | `boolean` | NO | `false` |
| `creator_role_definitions` | 9 | 8 | `permission_granting` | `boolean` | NO | `false` |
| `creator_role_definitions` | 9 | 9 | `created_by` | `bigint` | YES | `` |
| `creator_role_definitions` | 9 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creator_role_definitions` | 9 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `creator_team_members` | 0 | 1 | `team_id` | `bigint` | NO | `` |
| `creator_team_members` | 0 | 2 | `member_creator_id` | `bigint` | NO | `` |
| `creator_team_members` | 0 | 3 | `role_id` | `bigint` | NO | `` |
| `creator_team_members` | 0 | 4 | `title` | `text` | NO | `''::text` |
| `creator_team_members` | 0 | 5 | `status` | `text` | NO | `'pending'::text` |
| `creator_team_members` | 0 | 6 | `created_by` | `bigint` | YES | `` |
| `creator_team_members` | 0 | 7 | `approved_by` | `bigint` | YES | `` |
| `creator_team_members` | 0 | 8 | `approved_at` | `timestamp with time zone` | YES | `` |
| `creator_team_members` | 0 | 9 | `display_order` | `integer` | NO | `0` |
| `creator_team_members` | 0 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creator_team_members` | 0 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `creators` | 2 | 1 | `id` | `bigint` | NO | `nextval('creators_id_seq'::regclass)` |
| `creators` | 2 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `creators` | 2 | 3 | `kind` | `text` | NO | `` |
| `creators` | 2 | 4 | `name` | `text` | NO | `` |
| `creators` | 2 | 5 | `normalized_name` | `text` | NO | `` |
| `creators` | 2 | 6 | `description_markdown` | `text` | NO | `''::text` |
| `creators` | 2 | 7 | `avatar_url` | `text` | NO | `''::text` |
| `creators` | 2 | 8 | `avatar_file_id` | `bigint` | YES | `` |
| `creators` | 2 | 9 | `created_by` | `bigint` | YES | `` |
| `creators` | 2 | 10 | `review_status` | `text` | NO | `'pending'::text` |
| `creators` | 2 | 11 | `published_revision_id` | `bigint` | YES | `` |
| `creators` | 2 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `creators` | 2 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `currencies` | 3 | 1 | `id` | `bigint` | NO | `nextval('currencies_id_seq'::regclass)` |
| `currencies` | 3 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `currencies` | 3 | 3 | `code` | `text` | NO | `` |
| `currencies` | 3 | 4 | `name` | `text` | NO | `` |
| `currencies` | 3 | 5 | `description` | `text` | NO | `''::text` |
| `currencies` | 3 | 6 | `icon` | `text` | NO | `''::text` |
| `currencies` | 3 | 7 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `currencies` | 3 | 8 | `transfer_tax_bps` | `integer` | NO | `0` |
| `currencies` | 3 | 9 | `status` | `text` | NO | `'active'::text` |
| `currencies` | 3 | 10 | `display_order` | `integer` | NO | `0` |
| `currencies` | 3 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `currencies` | 3 | 12 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `currency_transactions` | 0 | 1 | `id` | `bigint` | NO | `nextval('currency_transactions_id_seq'::regclass)` |
| `currency_transactions` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `currency_transactions` | 0 | 3 | `currency_id` | `bigint` | NO | `` |
| `currency_transactions` | 0 | 4 | `amount_delta` | `bigint` | NO | `` |
| `currency_transactions` | 0 | 5 | `balance_after` | `bigint` | NO | `` |
| `currency_transactions` | 0 | 6 | `transaction_type` | `text` | NO | `` |
| `currency_transactions` | 0 | 7 | `counterparty_user_id` | `bigint` | YES | `` |
| `currency_transactions` | 0 | 8 | `reference_type` | `text` | NO | `''::text` |
| `currency_transactions` | 0 | 9 | `reference_key` | `text` | NO | `''::text` |
| `currency_transactions` | 0 | 10 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `currency_transactions` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `dead_letter_events` | 0 | 1 | `id` | `bigint` | NO | `nextval('dead_letter_events_id_seq'::regclass)` |
| `dead_letter_events` | 0 | 2 | `event_id` | `text` | NO | `` |
| `dead_letter_events` | 0 | 3 | `event_type` | `text` | NO | `` |
| `dead_letter_events` | 0 | 4 | `subject` | `text` | NO | `` |
| `dead_letter_events` | 0 | 5 | `payload` | `jsonb` | NO | `` |
| `dead_letter_events` | 0 | 6 | `failure_stage` | `text` | NO | `` |
| `dead_letter_events` | 0 | 7 | `attempts` | `integer` | NO | `0` |
| `dead_letter_events` | 0 | 8 | `last_error` | `text` | NO | `''::text` |
| `dead_letter_events` | 0 | 9 | `failed_at` | `timestamp with time zone` | NO | `now()` |
| `dead_letter_events` | 0 | 10 | `replayed_at` | `timestamp with time zone` | YES | `` |
| `direct_conversations` | 0 | 1 | `id` | `bigint` | NO | `nextval('direct_conversations_id_seq'::regclass)` |
| `direct_conversations` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `direct_conversations` | 0 | 3 | `user_low_id` | `bigint` | NO | `` |
| `direct_conversations` | 0 | 4 | `user_high_id` | `bigint` | NO | `` |
| `direct_conversations` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `direct_conversations` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `direct_messages` | 0 | 1 | `id` | `bigint` | NO | `nextval('direct_messages_id_seq'::regclass)` |
| `direct_messages` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `direct_messages` | 0 | 3 | `conversation_id` | `bigint` | NO | `` |
| `direct_messages` | 0 | 4 | `sender_id` | `bigint` | NO | `` |
| `direct_messages` | 0 | 5 | `recipient_id` | `bigint` | NO | `` |
| `direct_messages` | 0 | 6 | `body` | `text` | NO | `` |
| `direct_messages` | 0 | 7 | `read_at` | `timestamp with time zone` | YES | `` |
| `direct_messages` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `effective_project_access` | 0 | 1 | `user_id` | `bigint` | YES | `` |
| `effective_project_access` | 0 | 2 | `project_type` | `text` | YES | `` |
| `effective_project_access` | 0 | 3 | `project_id` | `bigint` | YES | `` |
| `effective_project_access` | 0 | 4 | `project_public_id` | `character varying` | YES | `` |
| `effective_project_access` | 0 | 5 | `access_level` | `text` | YES | `` |
| `effective_project_access` | 0 | 6 | `source_type` | `text` | YES | `` |
| `effective_project_access` | 0 | 7 | `source_id` | `bigint` | YES | `` |
| `effective_project_access` | 0 | 8 | `source_path` | `text` | YES | `` |
| `email_verification_codes` | 0 | 1 | `id` | `bigint` | NO | `nextval('email_verification_codes_id_seq'::regclass)` |
| `email_verification_codes` | 0 | 2 | `email` | `text` | NO | `` |
| `email_verification_codes` | 0 | 3 | `purpose` | `text` | NO | `` |
| `email_verification_codes` | 0 | 4 | `code_hash` | `text` | NO | `` |
| `email_verification_codes` | 0 | 5 | `request_ip` | `text` | NO | `''::text` |
| `email_verification_codes` | 0 | 6 | `expires_at` | `timestamp with time zone` | NO | `` |
| `email_verification_codes` | 0 | 7 | `consumed_at` | `timestamp with time zone` | YES | `` |
| `email_verification_codes` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `experience_transactions` | 0 | 1 | `id` | `bigint` | NO | `nextval('experience_transactions_id_seq'::regclass)` |
| `experience_transactions` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `experience_transactions` | 0 | 3 | `amount_delta` | `bigint` | NO | `` |
| `experience_transactions` | 0 | 4 | `experience_after` | `bigint` | NO | `` |
| `experience_transactions` | 0 | 5 | `reason` | `text` | NO | `` |
| `experience_transactions` | 0 | 6 | `reference_type` | `text` | NO | `''::text` |
| `experience_transactions` | 0 | 7 | `reference_key` | `text` | NO | `''::text` |
| `experience_transactions` | 0 | 8 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `experience_transactions` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `external_release_bindings` | 0 | 1 | `id` | `bigint` | NO | `nextval('external_release_bindings_id_seq'::regclass)` |
| `external_release_bindings` | 0 | 2 | `project_route_id` | `bigint` | NO | `` |
| `external_release_bindings` | 0 | 3 | `source_type` | `text` | NO | `` |
| `external_release_bindings` | 0 | 4 | `external_release_id` | `text` | NO | `` |
| `external_release_bindings` | 0 | 5 | `changelog_public_id` | `text` | YES | `` |
| `external_release_bindings` | 0 | 6 | `source_managed` | `boolean` | NO | `true` |
| `external_release_bindings` | 0 | 7 | `manual_override` | `boolean` | NO | `false` |
| `external_release_bindings` | 0 | 8 | `external_body_hash` | `text` | NO | `''::text` |
| `external_release_bindings` | 0 | 9 | `external_url` | `text` | NO | `''::text` |
| `external_release_bindings` | 0 | 10 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `external_release_bindings` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `external_release_bindings` | 0 | 12 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_collection_items` | 0 | 1 | `id` | `bigint` | NO | `nextval('favorite_collection_items_id_seq'::regclass)` |
| `favorite_collection_items` | 0 | 2 | `collection_id` | `bigint` | NO | `` |
| `favorite_collection_items` | 0 | 3 | `entity_type` | `text` | NO | `` |
| `favorite_collection_items` | 0 | 4 | `entity_id` | `bigint` | NO | `` |
| `favorite_collection_items` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_collections` | 1 | 1 | `id` | `bigint` | NO | `nextval('favorite_collections_id_seq'::regclass)` |
| `favorite_collections` | 1 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `favorite_collections` | 1 | 3 | `user_id` | `bigint` | NO | `` |
| `favorite_collections` | 1 | 4 | `name` | `text` | NO | `` |
| `favorite_collections` | 1 | 5 | `is_default` | `boolean` | NO | `false` |
| `favorite_collections` | 1 | 6 | `is_public` | `boolean` | NO | `false` |
| `favorite_collections` | 1 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_collections` | 1 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_modpack_export_items` | 0 | 1 | `id` | `bigint` | NO | `nextval('favorite_modpack_export_items_id_seq'::regclass)` |
| `favorite_modpack_export_items` | 0 | 2 | `task_id` | `bigint` | NO | `` |
| `favorite_modpack_export_items` | 0 | 3 | `source_collection_item_id` | `bigint` | YES | `` |
| `favorite_modpack_export_items` | 0 | 4 | `source_project_route_id` | `bigint` | YES | `` |
| `favorite_modpack_export_items` | 0 | 5 | `source_project_type` | `text` | NO | `` |
| `favorite_modpack_export_items` | 0 | 6 | `source_project_name_snapshot` | `text` | NO | `` |
| `favorite_modpack_export_items` | 0 | 7 | `result_type` | `text` | NO | `` |
| `favorite_modpack_export_items` | 0 | 8 | `reason_code` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 9 | `reason_detail` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 10 | `modrinth_project_id` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 11 | `modrinth_version_id` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 12 | `selected_version_name` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 13 | `selected_file_name` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 14 | `minecraft_version` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 15 | `loader` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 16 | `release_type` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 17 | `env_client` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 18 | `env_server` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 19 | `file_size` | `bigint` | NO | `0` |
| `favorite_modpack_export_items` | 0 | 20 | `sha1` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 21 | `sha512` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 22 | `download_url` | `text` | NO | `''::text` |
| `favorite_modpack_export_items` | 0 | 23 | `dependency_of` | `jsonb` | NO | `'[]'::jsonb` |
| `favorite_modpack_export_items` | 0 | 24 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_modpack_export_tasks` | 0 | 1 | `id` | `bigint` | NO | `nextval('favorite_modpack_export_tasks_id_seq'::regclass)` |
| `favorite_modpack_export_tasks` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `favorite_modpack_export_tasks` | 0 | 3 | `owner_user_id` | `bigint` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 4 | `collection_id` | `bigint` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 5 | `pack_name` | `text` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 6 | `pack_version_id` | `text` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 7 | `minecraft_version` | `text` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 8 | `loader_type` | `text` | NO | `` |
| `favorite_modpack_export_tasks` | 0 | 9 | `loader_version` | `text` | NO | `''::text` |
| `favorite_modpack_export_tasks` | 0 | 10 | `allow_compatible_only` | `boolean` | NO | `false` |
| `favorite_modpack_export_tasks` | 0 | 11 | `status` | `text` | NO | `'pending'::text` |
| `favorite_modpack_export_tasks` | 0 | 12 | `stage` | `text` | NO | `'queued'::text` |
| `favorite_modpack_export_tasks` | 0 | 13 | `attempt_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 14 | `lease_token` | `text` | NO | `''::text` |
| `favorite_modpack_export_tasks` | 0 | 15 | `lease_expires_at` | `timestamp with time zone` | YES | `` |
| `favorite_modpack_export_tasks` | 0 | 16 | `collection_item_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 17 | `exported_mod_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 18 | `auto_dependency_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 19 | `skipped_item_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 20 | `failed_item_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 21 | `final_file_count` | `integer` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 22 | `result_file_size` | `bigint` | NO | `0` |
| `favorite_modpack_export_tasks` | 0 | 23 | `result_sha256` | `text` | NO | `''::text` |
| `favorite_modpack_export_tasks` | 0 | 24 | `result_file_id` | `bigint` | YES | `` |
| `favorite_modpack_export_tasks` | 0 | 25 | `report_version` | `integer` | NO | `1` |
| `favorite_modpack_export_tasks` | 0 | 26 | `report_snapshot` | `jsonb` | NO | `'{}'::jsonb` |
| `favorite_modpack_export_tasks` | 0 | 27 | `error_code` | `text` | NO | `''::text` |
| `favorite_modpack_export_tasks` | 0 | 28 | `error_detail` | `text` | NO | `''::text` |
| `favorite_modpack_export_tasks` | 0 | 29 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `favorite_modpack_export_tasks` | 0 | 30 | `started_at` | `timestamp with time zone` | YES | `` |
| `favorite_modpack_export_tasks` | 0 | 31 | `finished_at` | `timestamp with time zone` | YES | `` |
| `favorite_modpack_export_tasks` | 0 | 32 | `expires_at` | `timestamp with time zone` | YES | `` |
| `favorite_modpack_export_tasks` | 0 | 33 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `game_resource_aliases` | 0 | 1 | `kind_code` | `text` | NO | `` |
| `game_resource_aliases` | 0 | 2 | `alias_id` | `text` | NO | `` |
| `game_resource_aliases` | 0 | 3 | `resource_id` | `bigint` | NO | `` |
| `game_resource_aliases` | 0 | 4 | `source` | `text` | NO | `'manual'::text` |
| `game_resource_aliases` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `game_resource_aliases` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `game_resource_asset_bindings` | 0 | 1 | `snapshot_id` | `text` | NO | `` |
| `game_resource_asset_bindings` | 0 | 2 | `item_resource_id` | `bigint` | YES | `` |
| `game_resource_asset_bindings` | 0 | 3 | `block_resource_id` | `bigint` | YES | `` |
| `game_resource_asset_bindings` | 0 | 4 | `blockstate_path` | `text` | NO | `''::text` |
| `game_resource_asset_bindings` | 0 | 5 | `item_model_path` | `text` | NO | `''::text` |
| `game_resource_asset_bindings` | 0 | 6 | `model_paths` | `ARRAY` | NO | `'{}'::text[]` |
| `game_resource_asset_bindings` | 0 | 7 | `texture_paths` | `ARRAY` | NO | `'{}'::text[]` |
| `game_resources` | 1 | 1 | `entity_id` | `bigint` | NO | `` |
| `game_resources` | 1 | 2 | `kind_code` | `text` | NO | `` |
| `game_resources` | 1 | 3 | `canonical_id` | `text` | NO | `` |
| `game_resources` | 1 | 4 | `namespace` | `text` | NO | `` |
| `game_resources` | 1 | 5 | `resource_path` | `text` | NO | `` |
| `game_resources` | 1 | 6 | `owner_mod_id` | `bigint` | YES | `` |
| `game_resources` | 1 | 7 | `created_from_revision_id` | `text` | YES | `` |
| `game_resources` | 1 | 8 | `resolved` | `boolean` | NO | `false` |
| `game_resources` | 1 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `game_resources` | 1 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `knowledge_pages` | 0 | 1 | `entity_id` | `bigint` | NO | `` |
| `knowledge_pages` | 0 | 2 | `locale` | `text` | NO | `` |
| `knowledge_pages` | 0 | 3 | `content_markdown` | `text` | NO | `''::text` |
| `knowledge_pages` | 0 | 4 | `updated_by` | `bigint` | YES | `` |
| `knowledge_pages` | 0 | 5 | `published_revision_id` | `bigint` | YES | `` |
| `knowledge_pages` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `knowledge_pages` | 0 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `level_system_config` | 1 | 1 | `singleton` | `boolean` | NO | `true` |
| `level_system_config` | 1 | 2 | `role_track_code` | `text` | YES | `` |
| `level_system_config` | 1 | 3 | `level_thresholds` | `ARRAY` | NO | `'{}'::bigint[]` |
| `level_system_config` | 1 | 4 | `updated_by` | `bigint` | YES | `` |
| `level_system_config` | 1 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `license_policies` | 10 | 1 | `spdx_id` | `text` | NO | `` |
| `license_policies` | 10 | 2 | `redistribution_allowed` | `boolean` | NO | `` |
| `license_policies` | 10 | 3 | `notes` | `text` | NO | `''::text` |
| `license_policies` | 10 | 4 | `updated_by` | `bigint` | YES | `` |
| `license_policies` | 10 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `log_share_entries` | 0 | 1 | `id` | `bigint` | NO | `nextval('log_share_entries_id_seq'::regclass)` |
| `log_share_entries` | 0 | 2 | `log_share_id` | `bigint` | NO | `` |
| `log_share_entries` | 0 | 3 | `entry_index` | `integer` | NO | `` |
| `log_share_entries` | 0 | 4 | `original_name` | `text` | NO | `''::text` |
| `log_share_entries` | 0 | 5 | `safe_display_name` | `text` | NO | `''::text` |
| `log_share_entries` | 0 | 6 | `content_type` | `text` | NO | `'text/plain'::text` |
| `log_share_entries` | 0 | 7 | `sanitized_text` | `text` | YES | `` |
| `log_share_entries` | 0 | 8 | `sanitized_object_key` | `text` | NO | `''::text` |
| `log_share_entries` | 0 | 9 | `byte_size` | `bigint` | NO | `0` |
| `log_share_entries` | 0 | 10 | `line_count` | `bigint` | NO | `0` |
| `log_share_entries` | 0 | 11 | `checksum` | `text` | NO | `''::text` |
| `log_share_entries` | 0 | 12 | `status` | `text` | NO | `'ready'::text` |
| `log_shares` | 0 | 1 | `id` | `bigint` | NO | `nextval('log_shares_id_seq'::regclass)` |
| `log_shares` | 0 | 2 | `public_code` | `text` | NO | `` |
| `log_shares` | 0 | 3 | `owner_user_id` | `bigint` | YES | `` |
| `log_shares` | 0 | 4 | `source_type` | `text` | NO | `` |
| `log_shares` | 0 | 5 | `source_file_id` | `bigint` | YES | `` |
| `log_shares` | 0 | 6 | `title` | `text` | NO | `''::text` |
| `log_shares` | 0 | 7 | `original_name` | `text` | NO | `''::text` |
| `log_shares` | 0 | 8 | `status` | `text` | NO | `'processing'::text` |
| `log_shares` | 0 | 9 | `redaction_version` | `integer` | NO | `1` |
| `log_shares` | 0 | 10 | `redaction_counts` | `jsonb` | NO | `'{}'::jsonb` |
| `log_shares` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `log_shares` | 0 | 12 | `expires_at` | `timestamp with time zone` | NO | `` |
| `log_shares` | 0 | 13 | `deleted_at` | `timestamp with time zone` | YES | `` |
| `markdown_playground_drafts` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `markdown_playground_drafts` | 0 | 2 | `content` | `text` | NO | `''::text` |
| `markdown_playground_drafts` | 0 | 3 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_server_links` | 0 | 1 | `id` | `bigint` | NO | `nextval('minecraft_server_links_id_seq'::regclass)` |
| `minecraft_server_links` | 0 | 2 | `server_id` | `bigint` | NO | `` |
| `minecraft_server_links` | 0 | 3 | `kind` | `text` | NO | `'website'::text` |
| `minecraft_server_links` | 0 | 4 | `label` | `text` | NO | `''::text` |
| `minecraft_server_links` | 0 | 5 | `url` | `text` | NO | `` |
| `minecraft_server_links` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `minecraft_server_mods` | 0 | 1 | `id` | `bigint` | NO | `nextval('minecraft_server_mods_id_seq'::regclass)` |
| `minecraft_server_mods` | 0 | 2 | `server_id` | `bigint` | NO | `` |
| `minecraft_server_mods` | 0 | 3 | `mod_id` | `bigint` | YES | `` |
| `minecraft_server_mods` | 0 | 4 | `raw_mod_id` | `text` | NO | `` |
| `minecraft_server_mods` | 0 | 5 | `version` | `text` | NO | `''::text` |
| `minecraft_server_mods` | 0 | 6 | `source` | `text` | NO | `'manual'::text` |
| `minecraft_server_mods` | 0 | 7 | `confidence` | `text` | NO | `'declared'::text` |
| `minecraft_server_mods` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_server_proof_files` | 0 | 1 | `server_id` | `bigint` | NO | `` |
| `minecraft_server_proof_files` | 0 | 2 | `oss_file_id` | `bigint` | NO | `` |
| `minecraft_server_proof_files` | 0 | 3 | `display_order` | `integer` | NO | `0` |
| `minecraft_server_proof_files` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_server_status_samples` | 0 | 1 | `id` | `bigint` | NO | `nextval('minecraft_server_status_samples_id_seq'::regclass)` |
| `minecraft_server_status_samples` | 0 | 2 | `server_id` | `bigint` | NO | `` |
| `minecraft_server_status_samples` | 0 | 3 | `checked_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_server_status_samples` | 0 | 4 | `online` | `boolean` | NO | `` |
| `minecraft_server_status_samples` | 0 | 5 | `latency_ms` | `integer` | YES | `` |
| `minecraft_server_status_samples` | 0 | 6 | `players_online` | `integer` | YES | `` |
| `minecraft_server_status_samples` | 0 | 7 | `players_max` | `integer` | YES | `` |
| `minecraft_server_status_samples` | 0 | 8 | `minecraft_version` | `text` | NO | `''::text` |
| `minecraft_server_status_samples` | 0 | 9 | `protocol` | `integer` | YES | `` |
| `minecraft_server_status_samples` | 0 | 10 | `error` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 1 | `id` | `bigint` | NO | `nextval('minecraft_servers_id_seq'::regclass)` |
| `minecraft_servers` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `minecraft_servers` | 0 | 3 | `slug` | `text` | NO | `` |
| `minecraft_servers` | 0 | 4 | `address` | `text` | NO | `` |
| `minecraft_servers` | 0 | 5 | `normalized_address` | `text` | NO | `` |
| `minecraft_servers` | 0 | 6 | `handshake_host` | `text` | NO | `` |
| `minecraft_servers` | 0 | 7 | `connect_host` | `text` | NO | `` |
| `minecraft_servers` | 0 | 8 | `connect_port` | `integer` | NO | `` |
| `minecraft_servers` | 0 | 9 | `name` | `text` | NO | `` |
| `minecraft_servers` | 0 | 10 | `short_description` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 11 | `body_markdown` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 12 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `minecraft_servers` | 0 | 13 | `dedicated_client` | `boolean` | NO | `false` |
| `minecraft_servers` | 0 | 14 | `languages` | `ARRAY` | NO | `'{}'::text[]` |
| `minecraft_servers` | 0 | 15 | `primary_tag` | `text` | NO | `` |
| `minecraft_servers` | 0 | 16 | `has_whitelist` | `boolean` | NO | `false` |
| `minecraft_servers` | 0 | 17 | `online_mode` | `boolean` | NO | `true` |
| `minecraft_servers` | 0 | 18 | `icon_data_uri` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 19 | `modded` | `boolean` | NO | `false` |
| `minecraft_servers` | 0 | 20 | `loader` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 21 | `mod_list_complete` | `boolean` | NO | `false` |
| `minecraft_servers` | 0 | 22 | `review_status` | `text` | NO | `'pending'::text` |
| `minecraft_servers` | 0 | 23 | `review_note` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 24 | `proof_text` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 25 | `submitted_by` | `bigint` | NO | `` |
| `minecraft_servers` | 0 | 26 | `reviewed_by` | `bigint` | YES | `` |
| `minecraft_servers` | 0 | 27 | `last_online` | `boolean` | NO | `false` |
| `minecraft_servers` | 0 | 28 | `last_latency_ms` | `integer` | YES | `` |
| `minecraft_servers` | 0 | 29 | `last_players_online` | `integer` | NO | `0` |
| `minecraft_servers` | 0 | 30 | `last_players_max` | `integer` | NO | `0` |
| `minecraft_servers` | 0 | 31 | `last_motd` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 32 | `last_minecraft_version` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 33 | `last_protocol` | `integer` | YES | `` |
| `minecraft_servers` | 0 | 34 | `last_checked_at` | `timestamp with time zone` | YES | `` |
| `minecraft_servers` | 0 | 35 | `last_error` | `text` | NO | `''::text` |
| `minecraft_servers` | 0 | 36 | `next_probe_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_servers` | 0 | 37 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_servers` | 0 | 38 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `minecraft_servers` | 0 | 39 | `reviewed_at` | `timestamp with time zone` | YES | `` |
| `minecraft_servers` | 0 | 40 | `published_at` | `timestamp with time zone` | YES | `` |
| `mirrored_project_files` | 0 | 1 | `id` | `bigint` | NO | `nextval('mirrored_project_files_id_seq'::regclass)` |
| `mirrored_project_files` | 0 | 2 | `project_route_id` | `bigint` | NO | `` |
| `mirrored_project_files` | 0 | 3 | `source_type` | `text` | NO | `` |
| `mirrored_project_files` | 0 | 4 | `external_file_id` | `text` | NO | `` |
| `mirrored_project_files` | 0 | 5 | `file_sha256` | `text` | NO | `` |
| `mirrored_project_files` | 0 | 6 | `byte_size` | `bigint` | NO | `` |
| `mirrored_project_files` | 0 | 7 | `oss_file_id` | `bigint` | YES | `` |
| `mirrored_project_files` | 0 | 8 | `license_spdx_id` | `text` | YES | `` |
| `mirrored_project_files` | 0 | 9 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `mirrored_project_files` | 0 | 10 | `status` | `text` | NO | `'pending'::text` |
| `mirrored_project_files` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_section_localizations` | 1 | 1 | `section_id` | `bigint` | NO | `` |
| `mod_content_section_localizations` | 1 | 2 | `locale` | `text` | NO | `` |
| `mod_content_section_localizations` | 1 | 3 | `name` | `text` | NO | `''::text` |
| `mod_content_section_localizations` | 1 | 4 | `description` | `text` | NO | `''::text` |
| `mod_content_section_resources` | 1 | 1 | `section_id` | `bigint` | NO | `` |
| `mod_content_section_resources` | 1 | 2 | `version_id` | `bigint` | NO | `` |
| `mod_content_section_resources` | 1 | 3 | `resource_id` | `bigint` | NO | `` |
| `mod_content_section_resources` | 1 | 4 | `placement_identity_key` | `text` | NO | `` |
| `mod_content_section_resources` | 1 | 5 | `similar_group_id` | `text` | NO | `''::text` |
| `mod_content_section_resources` | 1 | 6 | `ordinal` | `integer` | NO | `0` |
| `mod_content_section_resources` | 1 | 7 | `placement_source` | `text` | NO | `'manual'::text` |
| `mod_content_section_resources` | 1 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_sections` | 1 | 1 | `id` | `bigint` | NO | `nextval('mod_content_sections_id_seq'::regclass)` |
| `mod_content_sections` | 1 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `mod_content_sections` | 1 | 3 | `mod_id` | `bigint` | NO | `` |
| `mod_content_sections` | 1 | 4 | `version_id` | `bigint` | NO | `` |
| `mod_content_sections` | 1 | 5 | `template_id` | `bigint` | NO | `` |
| `mod_content_sections` | 1 | 6 | `parent_id` | `bigint` | YES | `` |
| `mod_content_sections` | 1 | 7 | `system_key` | `text` | NO | `''::text` |
| `mod_content_sections` | 1 | 8 | `default_locale` | `text` | NO | `'en-US'::text` |
| `mod_content_sections` | 1 | 9 | `display_mode` | `text` | NO | `` |
| `mod_content_sections` | 1 | 10 | `definition_override` | `jsonb` | YES | `` |
| `mod_content_sections` | 1 | 11 | `definition_version` | `integer` | NO | `1` |
| `mod_content_sections` | 1 | 12 | `ordinal` | `integer` | NO | `0` |
| `mod_content_sections` | 1 | 13 | `status` | `text` | NO | `'active'::text` |
| `mod_content_sections` | 1 | 14 | `published_revision_id` | `bigint` | YES | `` |
| `mod_content_sections` | 1 | 15 | `created_by` | `bigint` | YES | `` |
| `mod_content_sections` | 1 | 16 | `updated_by` | `bigint` | YES | `` |
| `mod_content_sections` | 1 | 17 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_sections` | 1 | 18 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_template_localizations` | 0 | 1 | `template_id` | `bigint` | NO | `` |
| `mod_content_template_localizations` | 0 | 2 | `locale` | `text` | NO | `` |
| `mod_content_template_localizations` | 0 | 3 | `name` | `text` | NO | `` |
| `mod_content_template_localizations` | 0 | 4 | `description` | `text` | NO | `''::text` |
| `mod_content_templates` | 18 | 1 | `id` | `bigint` | NO | `nextval('mod_content_templates_id_seq'::regclass)` |
| `mod_content_templates` | 18 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `mod_content_templates` | 18 | 3 | `owner_mod_id` | `bigint` | YES | `` |
| `mod_content_templates` | 18 | 4 | `code` | `text` | NO | `` |
| `mod_content_templates` | 18 | 5 | `builtin` | `boolean` | NO | `false` |
| `mod_content_templates` | 18 | 6 | `i18n_key` | `text` | NO | `''::text` |
| `mod_content_templates` | 18 | 7 | `default_locale` | `text` | NO | `'en-US'::text` |
| `mod_content_templates` | 18 | 8 | `default_display_mode` | `text` | NO | `'compact'::text` |
| `mod_content_templates` | 18 | 9 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `mod_content_templates` | 18 | 10 | `status` | `text` | NO | `'active'::text` |
| `mod_content_templates` | 18 | 11 | `published_revision_id` | `bigint` | YES | `` |
| `mod_content_templates` | 18 | 12 | `created_by` | `bigint` | YES | `` |
| `mod_content_templates` | 18 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_templates` | 18 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_versions` | 1 | 1 | `id` | `bigint` | NO | `nextval('mod_content_versions_id_seq'::regclass)` |
| `mod_content_versions` | 1 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `mod_content_versions` | 1 | 3 | `mod_id` | `bigint` | NO | `` |
| `mod_content_versions` | 1 | 4 | `label` | `text` | NO | `''::text` |
| `mod_content_versions` | 1 | 5 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `mod_content_versions` | 1 | 6 | `loaders` | `ARRAY` | NO | `'{}'::text[]` |
| `mod_content_versions` | 1 | 7 | `mod_version` | `text` | NO | `''::text` |
| `mod_content_versions` | 1 | 8 | `status` | `text` | NO | `'active'::text` |
| `mod_content_versions` | 1 | 9 | `published_revision_id` | `bigint` | YES | `` |
| `mod_content_versions` | 1 | 10 | `created_by` | `bigint` | YES | `` |
| `mod_content_versions` | 1 | 11 | `updated_by` | `bigint` | YES | `` |
| `mod_content_versions` | 1 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_content_versions` | 1 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_gallery_images` | 0 | 1 | `id` | `bigint` | NO | `nextval('mod_gallery_images_id_seq'::regclass)` |
| `mod_gallery_images` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `mod_gallery_images` | 0 | 3 | `mod_id` | `bigint` | NO | `` |
| `mod_gallery_images` | 0 | 4 | `oss_file_id` | `bigint` | NO | `` |
| `mod_gallery_images` | 0 | 5 | `display_order` | `integer` | NO | `0` |
| `mod_gallery_images` | 0 | 6 | `created_by` | `bigint` | YES | `` |
| `mod_gallery_images` | 0 | 7 | `published_revision_id` | `bigint` | YES | `` |
| `mod_gallery_images` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_identifiers` | 1 | 1 | `id` | `bigint` | NO | `nextval('mod_identifiers_id_seq'::regclass)` |
| `mod_identifiers` | 1 | 2 | `mod_id` | `bigint` | NO | `` |
| `mod_identifiers` | 1 | 3 | `identifier` | `text` | NO | `` |
| `mod_identifiers` | 1 | 4 | `is_primary` | `boolean` | NO | `false` |
| `mod_identifiers` | 1 | 5 | `minecraft_version_min` | `text` | NO | `''::text` |
| `mod_identifiers` | 1 | 6 | `minecraft_version_max` | `text` | NO | `''::text` |
| `mod_identifiers` | 1 | 7 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `mod_identifiers` | 1 | 8 | `display_order` | `integer` | NO | `0` |
| `mod_identifiers` | 1 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_identifiers` | 1 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_links` | 0 | 1 | `id` | `bigint` | NO | `nextval('mod_links_id_seq'::regclass)` |
| `mod_links` | 0 | 2 | `mod_id` | `bigint` | NO | `` |
| `mod_links` | 0 | 3 | `link_type` | `text` | NO | `` |
| `mod_links` | 0 | 4 | `url` | `text` | NO | `` |
| `mod_links` | 0 | 5 | `note` | `text` | NO | `''::text` |
| `mod_links` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `mod_links` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_loader_compatibilities` | 0 | 1 | `mod_id` | `bigint` | NO | `` |
| `mod_loader_compatibilities` | 0 | 2 | `loader` | `text` | NO | `` |
| `mod_loader_compatibilities` | 0 | 3 | `minecraft_version` | `text` | NO | `` |
| `mod_loader_compatibilities` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_metadata_import_jobs` | 0 | 1 | `id` | `bigint` | NO | `nextval('mod_metadata_import_jobs_id_seq'::regclass)` |
| `mod_metadata_import_jobs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `mod_metadata_import_jobs` | 0 | 3 | `user_id` | `bigint` | NO | `` |
| `mod_metadata_import_jobs` | 0 | 4 | `provider` | `text` | NO | `` |
| `mod_metadata_import_jobs` | 0 | 5 | `source_url` | `text` | NO | `` |
| `mod_metadata_import_jobs` | 0 | 6 | `status` | `text` | NO | `'queued'::text` |
| `mod_metadata_import_jobs` | 0 | 7 | `progress` | `integer` | NO | `0` |
| `mod_metadata_import_jobs` | 0 | 8 | `result` | `jsonb` | YES | `` |
| `mod_metadata_import_jobs` | 0 | 9 | `error` | `text` | NO | `''::text` |
| `mod_metadata_import_jobs` | 0 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_metadata_import_jobs` | 0 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_metadata_import_jobs` | 0 | 12 | `started_at` | `timestamp with time zone` | YES | `` |
| `mod_metadata_import_jobs` | 0 | 13 | `finished_at` | `timestamp with time zone` | YES | `` |
| `mod_metadata_import_jobs` | 0 | 14 | `project_type` | `text` | NO | `'mod'::text` |
| `mod_relationship_groups` | 0 | 1 | `id` | `bigint` | NO | `nextval('mod_relationship_groups_id_seq'::regclass)` |
| `mod_relationship_groups` | 0 | 2 | `mod_id` | `bigint` | NO | `` |
| `mod_relationship_groups` | 0 | 3 | `label` | `text` | NO | `''::text` |
| `mod_relationship_groups` | 0 | 4 | `loader` | `text` | NO | `''::text` |
| `mod_relationship_groups` | 0 | 5 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `mod_relationship_groups` | 0 | 6 | `mod_version` | `text` | NO | `''::text` |
| `mod_relationship_groups` | 0 | 7 | `display_order` | `integer` | NO | `0` |
| `mod_relationship_groups` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_relationships` | 0 | 1 | `id` | `bigint` | NO | `nextval('mod_relationships_id_seq'::regclass)` |
| `mod_relationships` | 0 | 2 | `mod_id` | `bigint` | NO | `` |
| `mod_relationships` | 0 | 3 | `relation_type` | `text` | NO | `` |
| `mod_relationships` | 0 | 4 | `related_mod_id` | `bigint` | YES | `` |
| `mod_relationships` | 0 | 5 | `group_id` | `bigint` | YES | `` |
| `mod_relationships` | 0 | 6 | `related_mod_name` | `text` | NO | `''::text` |
| `mod_relationships` | 0 | 7 | `related_mod_identifier` | `text` | NO | `''::text` |
| `mod_relationships` | 0 | 8 | `display_order` | `integer` | NO | `0` |
| `mod_relationships` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_resource_bindings` | 1 | 1 | `resource_id` | `bigint` | NO | `` |
| `mod_resource_bindings` | 1 | 2 | `mod_id` | `bigint` | NO | `` |
| `mod_resource_bindings` | 1 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_resource_version_detail_localizations` | 1 | 1 | `resource_id` | `bigint` | NO | `` |
| `mod_resource_version_detail_localizations` | 1 | 2 | `version_id` | `bigint` | NO | `` |
| `mod_resource_version_detail_localizations` | 1 | 3 | `locale` | `text` | NO | `` |
| `mod_resource_version_detail_localizations` | 1 | 4 | `name` | `text` | NO | `''::text` |
| `mod_resource_version_detail_localizations` | 1 | 5 | `summary` | `text` | NO | `''::text` |
| `mod_resource_version_detail_localizations` | 1 | 6 | `content_markdown` | `text` | NO | `''::text` |
| `mod_resource_version_detail_localizations` | 1 | 7 | `provenance` | `text` | NO | `'human'::text` |
| `mod_resource_version_details` | 1 | 1 | `resource_id` | `bigint` | NO | `` |
| `mod_resource_version_details` | 1 | 2 | `version_id` | `bigint` | NO | `` |
| `mod_resource_version_details` | 1 | 3 | `entry_type_code` | `text` | NO | `'default'::text` |
| `mod_resource_version_details` | 1 | 4 | `definition_schema_version` | `smallint` | NO | `1` |
| `mod_resource_version_details` | 1 | 5 | `default_locale` | `text` | NO | `'en-US'::text` |
| `mod_resource_version_details` | 1 | 6 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `mod_resource_version_details` | 1 | 7 | `icon_small_file_id` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 8 | `icon_file_id` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 9 | `render_file_id` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 10 | `status` | `text` | NO | `'active'::text` |
| `mod_resource_version_details` | 1 | 11 | `published_revision_id` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 12 | `created_by` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 13 | `updated_by` | `bigint` | YES | `` |
| `mod_resource_version_details` | 1 | 14 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mod_resource_version_details` | 1 | 15 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mod_tags` | 0 | 1 | `mod_id` | `bigint` | NO | `` |
| `mod_tags` | 0 | 2 | `tag` | `text` | NO | `` |
| `moderation_actions` | 0 | 1 | `id` | `bigint` | NO | `nextval('moderation_actions_id_seq'::regclass)` |
| `moderation_actions` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `moderation_actions` | 0 | 3 | `report_id` | `bigint` | YES | `` |
| `moderation_actions` | 0 | 4 | `action_type` | `text` | NO | `` |
| `moderation_actions` | 0 | 5 | `target_type` | `text` | NO | `` |
| `moderation_actions` | 0 | 6 | `target_public_id` | `text` | NO | `` |
| `moderation_actions` | 0 | 7 | `actor_id` | `bigint` | NO | `` |
| `moderation_actions` | 0 | 8 | `target_user_id` | `bigint` | YES | `` |
| `moderation_actions` | 0 | 9 | `before_summary` | `jsonb` | NO | `'{}'::jsonb` |
| `moderation_actions` | 0 | 10 | `after_summary` | `jsonb` | NO | `'{}'::jsonb` |
| `moderation_actions` | 0 | 11 | `reason` | `text` | NO | `''::text` |
| `moderation_actions` | 0 | 12 | `idempotency_key` | `text` | NO | `` |
| `moderation_actions` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `modpack_gallery_images` | 0 | 1 | `id` | `bigint` | NO | `nextval('modpack_gallery_images_id_seq'::regclass)` |
| `modpack_gallery_images` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `modpack_gallery_images` | 0 | 3 | `modpack_id` | `bigint` | NO | `` |
| `modpack_gallery_images` | 0 | 4 | `oss_file_id` | `bigint` | NO | `` |
| `modpack_gallery_images` | 0 | 5 | `display_order` | `integer` | NO | `0` |
| `modpack_gallery_images` | 0 | 6 | `created_by` | `bigint` | YES | `` |
| `modpack_gallery_images` | 0 | 7 | `published_revision_id` | `bigint` | YES | `` |
| `modpack_gallery_images` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `modpack_links` | 0 | 1 | `id` | `bigint` | NO | `nextval('modpack_links_id_seq'::regclass)` |
| `modpack_links` | 0 | 2 | `modpack_id` | `bigint` | NO | `` |
| `modpack_links` | 0 | 3 | `link_type` | `text` | NO | `` |
| `modpack_links` | 0 | 4 | `url` | `text` | NO | `` |
| `modpack_links` | 0 | 5 | `note` | `text` | NO | `''::text` |
| `modpack_links` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `modpack_loader_compatibilities` | 0 | 1 | `modpack_id` | `bigint` | NO | `` |
| `modpack_loader_compatibilities` | 0 | 2 | `loader` | `text` | NO | `` |
| `modpack_loader_compatibilities` | 0 | 3 | `minecraft_version` | `text` | NO | `` |
| `modpack_mods` | 0 | 1 | `id` | `bigint` | NO | `nextval('modpack_mods_id_seq'::regclass)` |
| `modpack_mods` | 0 | 2 | `modpack_id` | `bigint` | NO | `` |
| `modpack_mods` | 0 | 3 | `mod_id` | `bigint` | YES | `` |
| `modpack_mods` | 0 | 4 | `provider` | `text` | NO | `'manual'::text` |
| `modpack_mods` | 0 | 5 | `provider_project_id` | `text` | NO | `''::text` |
| `modpack_mods` | 0 | 6 | `provider_version_id` | `text` | NO | `''::text` |
| `modpack_mods` | 0 | 7 | `identifier` | `text` | NO | `''::text` |
| `modpack_mods` | 0 | 8 | `mod_name` | `text` | NO | `''::text` |
| `modpack_mods` | 0 | 9 | `file_name` | `text` | NO | `''::text` |
| `modpack_mods` | 0 | 10 | `client_required` | `boolean` | NO | `true` |
| `modpack_mods` | 0 | 11 | `server_required` | `boolean` | NO | `true` |
| `modpack_mods` | 0 | 12 | `display_order` | `integer` | NO | `0` |
| `modpack_mods` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `modpack_tags` | 0 | 1 | `modpack_id` | `bigint` | NO | `` |
| `modpack_tags` | 0 | 2 | `tag` | `text` | NO | `` |
| `modpacks` | 0 | 1 | `id` | `bigint` | NO | `nextval('modpacks_id_seq'::regclass)` |
| `modpacks` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `modpacks` | 0 | 3 | `slug` | `text` | NO | `` |
| `modpacks` | 0 | 4 | `primary_name` | `text` | NO | `` |
| `modpacks` | 0 | 5 | `secondary_name` | `text` | NO | `''::text` |
| `modpacks` | 0 | 6 | `abbreviation` | `text` | NO | `''::text` |
| `modpacks` | 0 | 7 | `summary` | `text` | NO | `''::text` |
| `modpacks` | 0 | 8 | `default_locale` | `text` | NO | `'zh-CN'::text` |
| `modpacks` | 0 | 9 | `environment` | `text` | NO | `'bothRequired'::text` |
| `modpacks` | 0 | 10 | `primary_category` | `text` | NO | `'adventure'::text` |
| `modpacks` | 0 | 11 | `pack_type` | `text` | NO | `'native'::text` |
| `modpacks` | 0 | 12 | `packaging_method` | `text` | NO | `'other'::text` |
| `modpacks` | 0 | 13 | `official_status` | `text` | NO | `'development'::text` |
| `modpacks` | 0 | 14 | `source_status` | `text` | NO | `'unknown'::text` |
| `modpacks` | 0 | 15 | `license` | `text` | NO | `'Custom'::text` |
| `modpacks` | 0 | 16 | `curseforge_project_id` | `text` | NO | `''::text` |
| `modpacks` | 0 | 17 | `modrinth_project_id` | `text` | NO | `''::text` |
| `modpacks` | 0 | 18 | `icon_url` | `text` | NO | `''::text` |
| `modpacks` | 0 | 19 | `body_markdown` | `text` | NO | `''::text` |
| `modpacks` | 0 | 20 | `search_keywords` | `ARRAY` | NO | `'{}'::text[]` |
| `modpacks` | 0 | 21 | `submission_method` | `text` | NO | `'manual'::text` |
| `modpacks` | 0 | 22 | `review_status` | `text` | NO | `'pending'::text` |
| `modpacks` | 0 | 23 | `submitted_by` | `bigint` | YES | `` |
| `modpacks` | 0 | 24 | `published_revision_id` | `bigint` | YES | `` |
| `modpacks` | 0 | 25 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `modpacks` | 0 | 26 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `modpacks` | 0 | 27 | `published_at` | `timestamp with time zone` | YES | `` |
| `mods` | 2 | 1 | `id` | `bigint` | NO | `nextval('mods_id_seq'::regclass)` |
| `mods` | 2 | 2 | `project_code` | `text` | NO | `` |
| `mods` | 2 | 3 | `slug` | `text` | NO | `` |
| `mods` | 2 | 4 | `primary_name` | `text` | NO | `` |
| `mods` | 2 | 5 | `secondary_name` | `text` | NO | `''::text` |
| `mods` | 2 | 6 | `abbreviation` | `text` | NO | `''::text` |
| `mods` | 2 | 7 | `summary` | `text` | NO | `''::text` |
| `mods` | 2 | 8 | `environment` | `text` | NO | `'bothRequired'::text` |
| `mods` | 2 | 9 | `primary_category` | `text` | NO | `'utility'::text` |
| `mods` | 2 | 10 | `official_status` | `text` | NO | `'development'::text` |
| `mods` | 2 | 11 | `source_status` | `text` | NO | `'unknown'::text` |
| `mods` | 2 | 12 | `license` | `text` | NO | `'Custom'::text` |
| `mods` | 2 | 13 | `curseforge_project_id` | `text` | NO | `''::text` |
| `mods` | 2 | 14 | `modrinth_project_id` | `text` | NO | `''::text` |
| `mods` | 2 | 15 | `github_project_path` | `text` | NO | `''::text` |
| `mods` | 2 | 16 | `icon_url` | `text` | NO | `''::text` |
| `mods` | 2 | 17 | `body_markdown` | `text` | NO | `''::text` |
| `mods` | 2 | 18 | `search_keywords` | `ARRAY` | NO | `'{}'::text[]` |
| `mods` | 2 | 19 | `submission_method` | `text` | NO | `'manual'::text` |
| `mods` | 2 | 20 | `review_status` | `text` | NO | `'pending'::text` |
| `mods` | 2 | 21 | `submitted_by` | `bigint` | YES | `` |
| `mods` | 2 | 22 | `published_revision_id` | `bigint` | YES | `` |
| `mods` | 2 | 23 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `mods` | 2 | 24 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `mods` | 2 | 25 | `published_at` | `timestamp with time zone` | YES | `` |
| `nats_outbox` | 1 | 1 | `id` | `bigint` | NO | `nextval('nats_outbox_id_seq'::regclass)` |
| `nats_outbox` | 1 | 2 | `event_id` | `text` | NO | `` |
| `nats_outbox` | 1 | 3 | `subject` | `text` | NO | `` |
| `nats_outbox` | 1 | 4 | `aggregate_type` | `text` | NO | `` |
| `nats_outbox` | 1 | 5 | `aggregate_id` | `text` | NO | `` |
| `nats_outbox` | 1 | 6 | `payload` | `jsonb` | NO | `` |
| `nats_outbox` | 1 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `nats_outbox` | 1 | 8 | `published_at` | `timestamp with time zone` | YES | `` |
| `nats_outbox` | 1 | 9 | `attempts` | `integer` | NO | `0` |
| `nats_outbox` | 1 | 10 | `last_error` | `text` | NO | `''::text` |
| `nats_outbox` | 1 | 11 | `event_type` | `text` | NO | `''::text` |
| `nats_outbox` | 1 | 12 | `schema_version` | `integer` | NO | `1` |
| `nats_outbox` | 1 | 13 | `occurred_at` | `timestamp with time zone` | NO | `now()` |
| `nats_outbox` | 1 | 14 | `trace_id` | `text` | NO | `''::text` |
| `nats_outbox` | 1 | 15 | `status` | `text` | NO | `'pending'::text` |
| `nats_outbox` | 1 | 16 | `available_at` | `timestamp with time zone` | NO | `now()` |
| `nats_outbox` | 1 | 17 | `locked_at` | `timestamp with time zone` | YES | `` |
| `nats_outbox` | 1 | 18 | `locked_by` | `text` | NO | `''::text` |
| `nats_outbox` | 1 | 19 | `max_attempts` | `integer` | NO | `12` |
| `nats_outbox` | 1 | 20 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `notification_actors` | 0 | 1 | `notification_id` | `bigint` | NO | `` |
| `notification_actors` | 0 | 2 | `actor_id` | `bigint` | NO | `` |
| `notification_actors` | 0 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `notification_receipts` | 0 | 1 | `notification_id` | `bigint` | NO | `` |
| `notification_receipts` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `notification_receipts` | 0 | 3 | `read_at` | `timestamp with time zone` | YES | `` |
| `notification_receipts` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `notification_translations` | 0 | 1 | `notification_id` | `bigint` | NO | `` |
| `notification_translations` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `notification_translations` | 0 | 3 | `locale` | `text` | NO | `` |
| `notification_translations` | 0 | 4 | `title` | `text` | NO | `''::text` |
| `notification_translations` | 0 | 5 | `body` | `text` | NO | `''::text` |
| `notification_translations` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `notifications` | 0 | 1 | `id` | `bigint` | NO | `nextval('notifications_id_seq'::regclass)` |
| `notifications` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `notifications` | 0 | 3 | `recipient_id` | `bigint` | YES | `` |
| `notifications` | 0 | 4 | `kind` | `text` | NO | `` |
| `notifications` | 0 | 5 | `title` | `text` | NO | `''::text` |
| `notifications` | 0 | 6 | `body` | `text` | NO | `''::text` |
| `notifications` | 0 | 7 | `source_locale` | `text` | NO | `'zh-CN'::text` |
| `notifications` | 0 | 8 | `template_key` | `text` | YES | `` |
| `notifications` | 0 | 9 | `template_version` | `integer` | NO | `0` |
| `notifications` | 0 | 10 | `template_params` | `jsonb` | NO | `'{}'::jsonb` |
| `notifications` | 0 | 11 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `notifications` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `notifications` | 0 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `notifications` | 0 | 14 | `source_event_id` | `text` | YES | `` |
| `notifications` | 0 | 15 | `project_update_event_id` | `bigint` | YES | `` |
| `oauth_accounts` | 0 | 1 | `id` | `bigint` | NO | `nextval('oauth_accounts_id_seq'::regclass)` |
| `oauth_accounts` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `oauth_accounts` | 0 | 3 | `provider` | `text` | NO | `` |
| `oauth_accounts` | 0 | 4 | `provider_user_id` | `text` | NO | `` |
| `oauth_accounts` | 0 | 5 | `username` | `text` | NO | `''::text` |
| `oauth_accounts` | 0 | 6 | `email` | `text` | NO | `''::text` |
| `oauth_accounts` | 0 | 7 | `avatar_url` | `text` | NO | `''::text` |
| `oauth_accounts` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `oauth_accounts` | 0 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `oss_download_stats` | 0 | 1 | `object_key` | `text` | NO | `` |
| `oss_download_stats` | 0 | 2 | `file_id` | `bigint` | YES | `` |
| `oss_download_stats` | 0 | 3 | `downloads` | `bigint` | NO | `0` |
| `oss_download_stats` | 0 | 4 | `total_bytes` | `bigint` | NO | `0` |
| `oss_download_stats` | 0 | 5 | `last_download_at` | `timestamp with time zone` | YES | `` |
| `oss_files` | 0 | 1 | `id` | `bigint` | NO | `nextval('oss_files_id_seq'::regclass)` |
| `oss_files` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `oss_files` | 0 | 3 | `bucket` | `text` | NO | `''::text` |
| `oss_files` | 0 | 4 | `endpoint` | `text` | NO | `''::text` |
| `oss_files` | 0 | 5 | `region` | `text` | NO | `''::text` |
| `oss_files` | 0 | 6 | `object_key` | `text` | NO | `` |
| `oss_files` | 0 | 7 | `category` | `text` | NO | `''::text` |
| `oss_files` | 0 | 8 | `source` | `text` | NO | `''::text` |
| `oss_files` | 0 | 9 | `original_name` | `text` | NO | `''::text` |
| `oss_files` | 0 | 10 | `source_original_name` | `text` | NO | `''::text` |
| `oss_files` | 0 | 11 | `content_type` | `text` | NO | `''::text` |
| `oss_files` | 0 | 12 | `size_bytes` | `bigint` | NO | `0` |
| `oss_files` | 0 | 13 | `source_size_bytes` | `bigint` | NO | `0` |
| `oss_files` | 0 | 14 | `sha256` | `text` | NO | `''::text` |
| `oss_files` | 0 | 15 | `uploader_id` | `bigint` | YES | `` |
| `oss_files` | 0 | 16 | `status` | `text` | NO | `'active'::text` |
| `oss_files` | 0 | 17 | `scan_status` | `text` | NO | `'pending'::text` |
| `oss_files` | 0 | 18 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `oss_files` | 0 | 19 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `oss_object_deletion_outbox` | 0 | 1 | `id` | `bigint` | NO | `nextval('oss_object_deletion_outbox_id_seq'::regclass)` |
| `oss_object_deletion_outbox` | 0 | 2 | `oss_file_id` | `bigint` | YES | `` |
| `oss_object_deletion_outbox` | 0 | 3 | `bucket` | `text` | NO | `` |
| `oss_object_deletion_outbox` | 0 | 4 | `endpoint` | `text` | NO | `` |
| `oss_object_deletion_outbox` | 0 | 5 | `region` | `text` | NO | `` |
| `oss_object_deletion_outbox` | 0 | 6 | `use_cname` | `boolean` | NO | `false` |
| `oss_object_deletion_outbox` | 0 | 7 | `object_key` | `text` | NO | `` |
| `oss_object_deletion_outbox` | 0 | 8 | `reason` | `text` | NO | `''::text` |
| `oss_object_deletion_outbox` | 0 | 9 | `status` | `text` | NO | `'pending'::text` |
| `oss_object_deletion_outbox` | 0 | 10 | `attempts` | `integer` | NO | `0` |
| `oss_object_deletion_outbox` | 0 | 11 | `next_attempt_at` | `timestamp with time zone` | NO | `now()` |
| `oss_object_deletion_outbox` | 0 | 12 | `locked_at` | `timestamp with time zone` | YES | `` |
| `oss_object_deletion_outbox` | 0 | 13 | `last_error` | `text` | NO | `''::text` |
| `oss_object_deletion_outbox` | 0 | 14 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `oss_object_deletion_outbox` | 0 | 15 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `oss_object_deletion_outbox` | 0 | 16 | `deleted_at` | `timestamp with time zone` | YES | `` |
| `oss_scan_logs` | 0 | 1 | `id` | `bigint` | NO | `nextval('oss_scan_logs_id_seq'::regclass)` |
| `oss_scan_logs` | 0 | 2 | `file_id` | `bigint` | YES | `` |
| `oss_scan_logs` | 0 | 3 | `object_key` | `text` | NO | `''::text` |
| `oss_scan_logs` | 0 | 4 | `engine` | `text` | NO | `''::text` |
| `oss_scan_logs` | 0 | 5 | `result` | `text` | NO | `'pending'::text` |
| `oss_scan_logs` | 0 | 6 | `message` | `text` | NO | `''::text` |
| `oss_scan_logs` | 0 | 7 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `oss_scan_logs` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `oss_upload_logs` | 0 | 1 | `id` | `bigint` | NO | `nextval('oss_upload_logs_id_seq'::regclass)` |
| `oss_upload_logs` | 0 | 2 | `file_id` | `bigint` | YES | `` |
| `oss_upload_logs` | 0 | 3 | `uploader_id` | `bigint` | YES | `` |
| `oss_upload_logs` | 0 | 4 | `object_key` | `text` | NO | `''::text` |
| `oss_upload_logs` | 0 | 5 | `original_name` | `text` | NO | `''::text` |
| `oss_upload_logs` | 0 | 6 | `size_bytes` | `bigint` | NO | `0` |
| `oss_upload_logs` | 0 | 7 | `ip` | `text` | NO | `''::text` |
| `oss_upload_logs` | 0 | 8 | `user_agent` | `text` | NO | `''::text` |
| `oss_upload_logs` | 0 | 9 | `result` | `text` | NO | `'success'::text` |
| `oss_upload_logs` | 0 | 10 | `message` | `text` | NO | `''::text` |
| `oss_upload_logs` | 0 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `permission_audit_logs` | 0 | 1 | `id` | `bigint` | NO | `nextval('permission_audit_logs_id_seq'::regclass)` |
| `permission_audit_logs` | 0 | 2 | `operator_id` | `bigint` | YES | `` |
| `permission_audit_logs` | 0 | 3 | `target_user_id` | `bigint` | YES | `` |
| `permission_audit_logs` | 0 | 4 | `action` | `text` | NO | `` |
| `permission_audit_logs` | 0 | 5 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `permission_audit_logs` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `permission_role_track_roles` | 0 | 1 | `track_code` | `text` | NO | `` |
| `permission_role_track_roles` | 0 | 2 | `role_id` | `bigint` | NO | `` |
| `permission_role_track_roles` | 0 | 3 | `position` | `integer` | NO | `` |
| `permission_role_tracks` | 0 | 1 | `code` | `text` | NO | `` |
| `permission_role_tracks` | 0 | 2 | `name` | `text` | NO | `` |
| `permission_role_tracks` | 0 | 3 | `description` | `text` | NO | `''::text` |
| `permission_role_tracks` | 0 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `permission_role_tracks` | 0 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `permissions` | 164 | 1 | `id` | `bigint` | NO | `nextval('permissions_id_seq'::regclass)` |
| `permissions` | 164 | 2 | `code` | `text` | NO | `` |
| `permissions` | 164 | 3 | `module` | `text` | NO | `` |
| `permissions` | 164 | 4 | `name` | `text` | NO | `` |
| `permissions` | 164 | 5 | `description` | `text` | NO | `''::text` |
| `permissions` | 164 | 6 | `access_type` | `text` | NO | `'read'::text` |
| `permissions` | 164 | 7 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `player_profile_name_history` | 0 | 1 | `id` | `bigint` | NO | `nextval('player_profile_name_history_id_seq'::regclass)` |
| `player_profile_name_history` | 0 | 2 | `profile_id` | `bigint` | NO | `` |
| `player_profile_name_history` | 0 | 3 | `old_name` | `text` | NO | `` |
| `player_profile_name_history` | 0 | 4 | `new_name` | `text` | NO | `` |
| `player_profile_name_history` | 0 | 5 | `changed_at` | `timestamp with time zone` | NO | `now()` |
| `player_profile_textures` | 0 | 1 | `profile_id` | `bigint` | NO | `` |
| `player_profile_textures` | 0 | 2 | `kind` | `text` | NO | `` |
| `player_profile_textures` | 0 | 3 | `asset_id` | `bigint` | NO | `` |
| `player_profile_textures` | 0 | 4 | `model` | `text` | NO | `'default'::text` |
| `player_profile_textures` | 0 | 5 | `equipped_at` | `timestamp with time zone` | NO | `now()` |
| `player_profiles` | 0 | 1 | `id` | `bigint` | NO | `nextval('player_profiles_id_seq'::regclass)` |
| `player_profiles` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `player_profiles` | 0 | 3 | `user_id` | `bigint` | NO | `` |
| `player_profiles` | 0 | 4 | `uuid` | `uuid` | NO | `` |
| `player_profiles` | 0 | 5 | `name` | `text` | NO | `` |
| `player_profiles` | 0 | 6 | `bio` | `text` | NO | `''::text` |
| `player_profiles` | 0 | 7 | `visibility` | `text` | NO | `'public'::text` |
| `player_profiles` | 0 | 8 | `is_default` | `boolean` | NO | `false` |
| `player_profiles` | 0 | 9 | `status` | `text` | NO | `'active'::text` |
| `player_profiles` | 0 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `player_profiles` | 0 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `processed_events` | 0 | 1 | `consumer` | `text` | NO | `` |
| `processed_events` | 0 | 2 | `event_id` | `text` | NO | `` |
| `processed_events` | 0 | 3 | `event_type` | `text` | NO | `` |
| `processed_events` | 0 | 4 | `processed_at` | `timestamp with time zone` | NO | `now()` |
| `project_auto_update_runs` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_auto_update_runs_id_seq'::regclass)` |
| `project_auto_update_runs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_auto_update_runs` | 0 | 3 | `setting_id` | `bigint` | NO | `` |
| `project_auto_update_runs` | 0 | 4 | `status` | `text` | NO | `'pending'::text` |
| `project_auto_update_runs` | 0 | 5 | `actor_id` | `bigint` | YES | `` |
| `project_auto_update_runs` | 0 | 6 | `lease_owner` | `text` | NO | `''::text` |
| `project_auto_update_runs` | 0 | 7 | `lease_expires_at` | `timestamp with time zone` | YES | `` |
| `project_auto_update_runs` | 0 | 8 | `attempts` | `integer` | NO | `0` |
| `project_auto_update_runs` | 0 | 9 | `next_attempt_at` | `timestamp with time zone` | NO | `now()` |
| `project_auto_update_runs` | 0 | 10 | `result` | `jsonb` | NO | `'{}'::jsonb` |
| `project_auto_update_runs` | 0 | 11 | `last_error_code` | `text` | NO | `''::text` |
| `project_auto_update_runs` | 0 | 12 | `last_error` | `text` | NO | `''::text` |
| `project_auto_update_runs` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_auto_update_runs` | 0 | 14 | `started_at` | `timestamp with time zone` | YES | `` |
| `project_auto_update_runs` | 0 | 15 | `finished_at` | `timestamp with time zone` | YES | `` |
| `project_auto_update_settings` | 3 | 1 | `id` | `bigint` | NO | `nextval('project_auto_update_settings_id_seq'::regclass)` |
| `project_auto_update_settings` | 3 | 2 | `project_route_id` | `bigint` | NO | `` |
| `project_auto_update_settings` | 3 | 3 | `update_kind` | `text` | NO | `` |
| `project_auto_update_settings` | 3 | 4 | `source_type` | `text` | YES | `` |
| `project_auto_update_settings` | 3 | 5 | `interval_code` | `text` | NO | `` |
| `project_auto_update_settings` | 3 | 6 | `enabled` | `boolean` | NO | `false` |
| `project_auto_update_settings` | 3 | 7 | `next_run_at` | `timestamp with time zone` | YES | `` |
| `project_auto_update_settings` | 3 | 8 | `last_run_at` | `timestamp with time zone` | YES | `` |
| `project_auto_update_settings` | 3 | 9 | `last_status` | `text` | NO | `'never'::text` |
| `project_auto_update_settings` | 3 | 10 | `last_error_code` | `text` | NO | `''::text` |
| `project_auto_update_settings` | 3 | 11 | `last_error` | `text` | NO | `''::text` |
| `project_auto_update_settings` | 3 | 12 | `configured_by` | `bigint` | YES | `` |
| `project_auto_update_settings` | 3 | 13 | `license_override` | `boolean` | NO | `false` |
| `project_auto_update_settings` | 3 | 14 | `license_override_reason` | `text` | NO | `''::text` |
| `project_auto_update_settings` | 3 | 15 | `license_override_source` | `text` | NO | `''::text` |
| `project_auto_update_settings` | 3 | 16 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_auto_update_settings` | 3 | 17 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_automation_activity` | 0 | 1 | `project_route_id` | `bigint` | NO | `` |
| `project_automation_activity` | 0 | 2 | `last_project_change_at` | `timestamp with time zone` | NO | `` |
| `project_automation_activity` | 0 | 3 | `has_external_activity` | `boolean` | NO | `false` |
| `project_automation_activity` | 0 | 4 | `last_checked_at` | `timestamp with time zone` | NO | `now()` |
| `project_automation_activity` | 0 | 5 | `automated_status` | `text` | YES | `` |
| `project_automation_activity` | 0 | 6 | `status_before_automation` | `text` | NO | `''::text` |
| `project_automation_activity` | 0 | 7 | `manual_status_override` | `boolean` | NO | `false` |
| `project_automation_activity` | 0 | 8 | `last_source_type` | `text` | NO | `''::text` |
| `project_automation_activity` | 0 | 9 | `last_update_kind` | `text` | NO | `''::text` |
| `project_automation_activity` | 0 | 10 | `changed_by` | `bigint` | YES | `` |
| `project_automation_activity` | 0 | 11 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelog_categories` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_changelog_categories_id_seq'::regclass)` |
| `project_changelog_categories` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_changelog_categories` | 0 | 3 | `object_route_id` | `bigint` | NO | `` |
| `project_changelog_categories` | 0 | 4 | `default_locale` | `text` | NO | `` |
| `project_changelog_categories` | 0 | 5 | `created_by` | `bigint` | YES | `` |
| `project_changelog_categories` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelog_categories` | 0 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelog_category_localizations` | 0 | 1 | `category_id` | `bigint` | NO | `` |
| `project_changelog_category_localizations` | 0 | 2 | `locale` | `text` | NO | `` |
| `project_changelog_category_localizations` | 0 | 3 | `name` | `text` | NO | `` |
| `project_changelog_localizations` | 0 | 1 | `changelog_id` | `bigint` | NO | `` |
| `project_changelog_localizations` | 0 | 2 | `locale` | `text` | NO | `` |
| `project_changelog_localizations` | 0 | 3 | `body_markdown` | `text` | NO | `` |
| `project_changelog_localizations` | 0 | 4 | `source_revision_id` | `bigint` | NO | `` |
| `project_changelog_localizations` | 0 | 5 | `updated_by` | `bigint` | YES | `` |
| `project_changelog_localizations` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelogs` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_changelogs_id_seq'::regclass)` |
| `project_changelogs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_changelogs` | 0 | 3 | `object_route_id` | `bigint` | NO | `` |
| `project_changelogs` | 0 | 4 | `category_id` | `bigint` | YES | `` |
| `project_changelogs` | 0 | 5 | `event_at` | `timestamp with time zone` | NO | `` |
| `project_changelogs` | 0 | 6 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `project_changelogs` | 0 | 7 | `project_version` | `text` | NO | `` |
| `project_changelogs` | 0 | 8 | `default_locale` | `text` | NO | `` |
| `project_changelogs` | 0 | 9 | `status` | `text` | NO | `'active'::text` |
| `project_changelogs` | 0 | 10 | `review_status` | `text` | NO | `'pending'::text` |
| `project_changelogs` | 0 | 11 | `published_revision_id` | `bigint` | YES | `` |
| `project_changelogs` | 0 | 12 | `created_by` | `bigint` | NO | `` |
| `project_changelogs` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelogs` | 0 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_changelogs` | 0 | 15 | `published_at` | `timestamp with time zone` | YES | `` |
| `project_editor_application_attachments` | 0 | 1 | `application_id` | `bigint` | NO | `` |
| `project_editor_application_attachments` | 0 | 2 | `oss_file_id` | `bigint` | NO | `` |
| `project_editor_applications` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_editor_applications_id_seq'::regclass)` |
| `project_editor_applications` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_editor_applications` | 0 | 3 | `target_route_id` | `bigint` | NO | `` |
| `project_editor_applications` | 0 | 4 | `user_id` | `bigint` | NO | `` |
| `project_editor_applications` | 0 | 5 | `proof_markdown` | `text` | NO | `` |
| `project_editor_applications` | 0 | 6 | `status` | `text` | NO | `'pending'::text` |
| `project_editor_applications` | 0 | 7 | `reviewed_by` | `bigint` | YES | `` |
| `project_editor_applications` | 0 | 8 | `review_note` | `text` | NO | `''::text` |
| `project_editor_applications` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_editor_applications` | 0 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_editor_applications` | 0 | 11 | `reviewed_at` | `timestamp with time zone` | YES | `` |
| `project_editor_assignments` | 0 | 1 | `target_route_id` | `bigint` | NO | `` |
| `project_editor_assignments` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `project_editor_assignments` | 0 | 3 | `application_id` | `bigint` | YES | `` |
| `project_editor_assignments` | 0 | 4 | `granted_by` | `bigint` | YES | `` |
| `project_editor_assignments` | 0 | 5 | `status` | `text` | NO | `'active'::text` |
| `project_editor_assignments` | 0 | 6 | `revoked_by` | `bigint` | YES | `` |
| `project_editor_assignments` | 0 | 7 | `revoked_at` | `timestamp with time zone` | YES | `` |
| `project_editor_assignments` | 0 | 8 | `revoke_reason` | `text` | NO | `''::text` |
| `project_editor_assignments` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_editor_assignments` | 0 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_external_sources` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_external_sources_id_seq'::regclass)` |
| `project_external_sources` | 0 | 2 | `project_route_id` | `bigint` | NO | `` |
| `project_external_sources` | 0 | 3 | `source_type` | `text` | NO | `` |
| `project_external_sources` | 0 | 4 | `external_project_id` | `text` | NO | `` |
| `project_external_sources` | 0 | 5 | `external_project_url` | `text` | NO | `` |
| `project_external_sources` | 0 | 6 | `verified_at` | `timestamp with time zone` | NO | `` |
| `project_external_sources` | 0 | 7 | `verified_by` | `bigint` | YES | `` |
| `project_external_sources` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_files` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_files_id_seq'::regclass)` |
| `project_files` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_files` | 0 | 3 | `project_type` | `text` | NO | `` |
| `project_files` | 0 | 4 | `project_internal_id` | `bigint` | NO | `` |
| `project_files` | 0 | 5 | `oss_file_id` | `bigint` | NO | `` |
| `project_files` | 0 | 6 | `display_name` | `text` | NO | `''::text` |
| `project_files` | 0 | 7 | `version_name` | `text` | NO | `''::text` |
| `project_files` | 0 | 8 | `release_channel` | `text` | NO | `'release'::text` |
| `project_files` | 0 | 9 | `game_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `project_files` | 0 | 10 | `loaders` | `ARRAY` | NO | `'{}'::text[]` |
| `project_files` | 0 | 11 | `file_name` | `text` | NO | `` |
| `project_files` | 0 | 12 | `content_type` | `text` | NO | `'application/java-archive'::text` |
| `project_files` | 0 | 13 | `size_bytes` | `bigint` | NO | `0` |
| `project_files` | 0 | 14 | `sha256` | `text` | NO | `''::text` |
| `project_files` | 0 | 15 | `download_count` | `bigint` | NO | `0` |
| `project_files` | 0 | 16 | `status` | `text` | NO | `'active'::text` |
| `project_files` | 0 | 17 | `uploaded_by` | `bigint` | YES | `` |
| `project_files` | 0 | 18 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_files` | 0 | 19 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_follows` | 1 | 1 | `user_id` | `bigint` | NO | `` |
| `project_follows` | 1 | 2 | `project_route_id` | `bigint` | NO | `` |
| `project_follows` | 1 | 3 | `notifications_enabled` | `boolean` | NO | `true` |
| `project_follows` | 1 | 4 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_follows` | 1 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `project_update_events` | 0 | 1 | `id` | `bigint` | NO | `nextval('project_update_events_id_seq'::regclass)` |
| `project_update_events` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `project_update_events` | 0 | 3 | `project_route_id` | `bigint` | NO | `` |
| `project_update_events` | 0 | 4 | `revision_id` | `text` | YES | `` |
| `project_update_events` | 0 | 5 | `actor_user_id` | `bigint` | YES | `` |
| `project_update_events` | 0 | 6 | `update_kind` | `text` | NO | `` |
| `project_update_events` | 0 | 7 | `changed_sections` | `ARRAY` | NO | `'{}'::text[]` |
| `project_update_events` | 0 | 8 | `publication_batch_id` | `text` | NO | `` |
| `project_update_events` | 0 | 9 | `published_at` | `timestamp with time zone` | NO | `now()` |
| `project_update_events` | 0 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_update_notification_tasks` | 0 | 1 | `event_id` | `bigint` | NO | `` |
| `project_update_notification_tasks` | 0 | 2 | `status` | `text` | NO | `'pending'::text` |
| `project_update_notification_tasks` | 0 | 3 | `next_user_id` | `bigint` | NO | `0` |
| `project_update_notification_tasks` | 0 | 4 | `notified_count` | `bigint` | NO | `0` |
| `project_update_notification_tasks` | 0 | 5 | `attempt_count` | `integer` | NO | `0` |
| `project_update_notification_tasks` | 0 | 6 | `next_attempt_at` | `timestamp with time zone` | NO | `now()` |
| `project_update_notification_tasks` | 0 | 7 | `last_error` | `text` | NO | `''::text` |
| `project_update_notification_tasks` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `project_update_notification_tasks` | 0 | 9 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `public_id_registry` | 128 | 1 | `public_id` | `character varying` | NO | `` |
| `public_id_registry` | 128 | 2 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `public_routes` | 55 | 1 | `id` | `bigint` | NO | `nextval('public_routes_id_seq'::regclass)` |
| `public_routes` | 55 | 2 | `public_id` | `character varying` | NO | `` |
| `public_routes` | 55 | 3 | `entity_type` | `text` | NO | `` |
| `public_routes` | 55 | 4 | `internal_id` | `bigint` | NO | `` |
| `public_routes` | 55 | 5 | `canonical_path` | `text` | NO | `''::text` |
| `public_routes` | 55 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `public_routes` | 55 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `published_project_routes` | 0 | 1 | `id` | `bigint` | YES | `` |
| `published_project_routes` | 0 | 2 | `public_id` | `character varying` | YES | `` |
| `published_project_routes` | 0 | 3 | `entity_type` | `text` | YES | `` |
| `published_project_routes` | 0 | 4 | `internal_id` | `bigint` | YES | `` |
| `recipe_binding_candidates` | 0 | 1 | `id` | `bigint` | NO | `nextval('recipe_binding_candidates_id_seq'::regclass)` |
| `recipe_binding_candidates` | 0 | 2 | `identity_key` | `text` | NO | `` |
| `recipe_binding_candidates` | 0 | 3 | `binding_id` | `bigint` | NO | `` |
| `recipe_binding_candidates` | 0 | 4 | `candidate_index` | `integer` | NO | `` |
| `recipe_binding_candidates` | 0 | 5 | `resource_id` | `bigint` | NO | `` |
| `recipe_binding_candidates` | 0 | 6 | `amount` | `numeric` | NO | `1` |
| `recipe_binding_candidates` | 0 | 7 | `probability` | `numeric` | YES | `` |
| `recipe_binding_candidates` | 0 | 8 | `byproduct` | `boolean` | NO | `false` |
| `recipe_binding_candidates` | 0 | 9 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_bindings` | 0 | 1 | `id` | `bigint` | NO | `nextval('recipe_bindings_id_seq'::regclass)` |
| `recipe_bindings` | 0 | 2 | `identity_key` | `text` | NO | `` |
| `recipe_bindings` | 0 | 3 | `recipe_id` | `bigint` | NO | `` |
| `recipe_bindings` | 0 | 4 | `template_slot_id` | `bigint` | NO | `` |
| `recipe_bindings` | 0 | 5 | `ordinal` | `integer` | NO | `` |
| `recipe_bindings` | 0 | 6 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_content_overrides` | 0 | 1 | `recipe_id` | `bigint` | NO | `` |
| `recipe_content_overrides` | 0 | 2 | `note` | `text` | NO | `''::text` |
| `recipe_content_overrides` | 0 | 3 | `layout_override` | `jsonb` | YES | `` |
| `recipe_content_overrides` | 0 | 4 | `updated_by` | `bigint` | YES | `` |
| `recipe_content_overrides` | 0 | 5 | `published_revision_id` | `bigint` | YES | `` |
| `recipe_content_overrides` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_definitions` | 0 | 1 | `recipe_id` | `bigint` | NO | `` |
| `recipe_definitions` | 0 | 2 | `template_id` | `bigint` | NO | `` |
| `recipe_definitions` | 0 | 3 | `definition_schema_version` | `smallint` | NO | `1` |
| `recipe_definitions` | 0 | 4 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_definitions` | 0 | 5 | `published_revision_id` | `bigint` | YES | `` |
| `recipe_definitions` | 0 | 6 | `updated_by` | `bigint` | YES | `` |
| `recipe_definitions` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_definitions` | 0 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_definitions` | 0 | 9 | `source_mod_content_version_id` | `bigint` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 1 | `id` | `bigint` | NO | `nextval('recipe_import_binding_candidates_id_seq'::regclass)` |
| `recipe_import_binding_candidates` | 0 | 2 | `binding_id` | `text` | NO | `` |
| `recipe_import_binding_candidates` | 0 | 3 | `alternative_index` | `integer` | NO | `` |
| `recipe_import_binding_candidates` | 0 | 4 | `resource_id` | `bigint` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 5 | `raw_resource_id` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 6 | `amount` | `double precision` | NO | `1` |
| `recipe_import_binding_candidates` | 0 | 7 | `ingredient_kind` | `text` | NO | `` |
| `recipe_import_binding_candidates` | 0 | 8 | `ingredient_type` | `text` | NO | `` |
| `recipe_import_binding_candidates` | 0 | 9 | `unique_id` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 10 | `nbt_snbt` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 11 | `chance_available` | `boolean` | NO | `false` |
| `recipe_import_binding_candidates` | 0 | 12 | `chance` | `double precision` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 13 | `chance_percent` | `double precision` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 14 | `chance_comparator` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 15 | `chance_source` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 16 | `chance_text` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 17 | `chance_texts` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_import_binding_candidates` | 0 | 18 | `chance_translation_key` | `text` | NO | `''::text` |
| `recipe_import_binding_candidates` | 0 | 19 | `chance_render_x` | `double precision` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 20 | `chance_render_y` | `double precision` | YES | `` |
| `recipe_import_binding_candidates` | 0 | 21 | `byproduct` | `boolean` | NO | `false` |
| `recipe_import_bindings` | 0 | 1 | `id` | `text` | NO | `` |
| `recipe_import_bindings` | 0 | 2 | `recipe_snapshot_id` | `text` | NO | `` |
| `recipe_import_bindings` | 0 | 3 | `template_slot_id` | `text` | NO | `` |
| `recipe_import_bindings` | 0 | 4 | `source_slot_id` | `text` | NO | `` |
| `recipe_import_bindings` | 0 | 5 | `ordinal` | `integer` | NO | `` |
| `recipe_import_bindings` | 0 | 6 | `ingredient_present` | `boolean` | NO | `false` |
| `recipe_import_bindings` | 0 | 7 | `clickable` | `boolean` | NO | `false` |
| `recipe_import_bindings` | 0 | 8 | `placeholder_item` | `text` | NO | `''::text` |
| `recipe_import_bindings` | 0 | 9 | `item_tag_equivalent` | `text` | NO | `''::text` |
| `recipe_import_bindings` | 0 | 10 | `semantic_role` | `text` | NO | `''::text` |
| `recipe_import_bindings` | 0 | 11 | `role_source` | `text` | NO | `''::text` |
| `recipe_import_bindings` | 0 | 12 | `tag_id` | `bigint` | YES | `` |
| `recipe_import_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 2 | `recipe_id` | `bigint` | NO | `` |
| `recipe_import_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 4 | `source_recipe_id` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 5 | `source_id_kind` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 6 | `source_recipe_key` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 7 | `recipe_collection_path` | `text` | NO | `` |
| `recipe_import_snapshots` | 0 | 8 | `origin_kind` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 9 | `underlying_recipe_type_id` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 10 | `source_mod_id` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 11 | `source_mod_version` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 12 | `source_mod_id_source` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 13 | `render_locale` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 14 | `definition_schema_version` | `smallint` | NO | `1` |
| `recipe_import_snapshots` | 0 | 15 | `template_id` | `text` | YES | `` |
| `recipe_import_snapshots` | 0 | 16 | `layout_available` | `boolean` | NO | `true` |
| `recipe_import_snapshots` | 0 | 17 | `layout_kind` | `text` | NO | `'unknown'::text` |
| `recipe_import_snapshots` | 0 | 18 | `ordered` | `boolean` | YES | `` |
| `recipe_import_snapshots` | 0 | 19 | `layout_classification_source` | `text` | NO | `''::text` |
| `recipe_import_snapshots` | 0 | 20 | `width` | `integer` | YES | `` |
| `recipe_import_snapshots` | 0 | 21 | `height` | `integer` | YES | `` |
| `recipe_import_snapshots` | 0 | 22 | `parameters` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_import_snapshots` | 0 | 23 | `binding_count` | `integer` | NO | `0` |
| `recipe_layout_templates` | 0 | 1 | `entity_id` | `bigint` | NO | `` |
| `recipe_layout_templates` | 0 | 2 | `recipe_type_id` | `bigint` | NO | `` |
| `recipe_layout_templates` | 0 | 3 | `template_key` | `text` | NO | `` |
| `recipe_layout_templates` | 0 | 4 | `import_snapshot_id` | `text` | YES | `` |
| `recipe_layout_templates` | 0 | 5 | `background_file_id` | `bigint` | YES | `` |
| `recipe_layout_templates` | 0 | 6 | `canvas_width` | `integer` | NO | `` |
| `recipe_layout_templates` | 0 | 7 | `canvas_height` | `integer` | NO | `` |
| `recipe_layout_templates` | 0 | 8 | `image_scale` | `integer` | NO | `1` |
| `recipe_layout_templates` | 0 | 9 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_layout_templates` | 0 | 10 | `published_revision_id` | `bigint` | YES | `` |
| `recipe_layout_templates` | 0 | 11 | `updated_by` | `bigint` | YES | `` |
| `recipe_layout_templates` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_layout_templates` | 0 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_template_import_slots` | 0 | 1 | `id` | `text` | NO | `` |
| `recipe_template_import_slots` | 0 | 2 | `template_id` | `text` | NO | `` |
| `recipe_template_import_slots` | 0 | 3 | `source_slot_id` | `text` | NO | `` |
| `recipe_template_import_slots` | 0 | 4 | `role` | `text` | NO | `` |
| `recipe_template_import_slots` | 0 | 5 | `jei_role` | `text` | NO | `''::text` |
| `recipe_template_import_slots` | 0 | 6 | `output_index` | `integer` | YES | `` |
| `recipe_template_import_slots` | 0 | 7 | `ordinal` | `integer` | NO | `` |
| `recipe_template_import_slots` | 0 | 8 | `coordinates_available` | `boolean` | NO | `false` |
| `recipe_template_import_slots` | 0 | 9 | `rect` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_slots` | 0 | 10 | `visual_rect` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_slots` | 0 | 11 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 2 | `recipe_type_snapshot_id` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 4 | `recipe_type_id` | `bigint` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 5 | `canonical_template_id` | `bigint` | YES | `` |
| `recipe_template_import_snapshots` | 0 | 6 | `source_template_id` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 7 | `schema_version` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 8 | `template_collection_path` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 9 | `background_path` | `text` | NO | `` |
| `recipe_template_import_snapshots` | 0 | 10 | `background_contains_ingredients` | `boolean` | NO | `false` |
| `recipe_template_import_snapshots` | 0 | 11 | `coordinate_space` | `text` | NO | `'logical_pixels'::text` |
| `recipe_template_import_snapshots` | 0 | 12 | `image_scale` | `integer` | NO | `1` |
| `recipe_template_import_snapshots` | 0 | 13 | `canvas` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_snapshots` | 0 | 14 | `image_pixels` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_snapshots` | 0 | 15 | `content_rect` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_template_import_snapshots` | 0 | 16 | `slot_count` | `integer` | NO | `0` |
| `recipe_template_slots` | 0 | 1 | `id` | `bigint` | NO | `nextval('recipe_template_slots_id_seq'::regclass)` |
| `recipe_template_slots` | 0 | 2 | `identity_key` | `text` | NO | `` |
| `recipe_template_slots` | 0 | 3 | `template_id` | `bigint` | NO | `` |
| `recipe_template_slots` | 0 | 4 | `slot_key` | `text` | NO | `` |
| `recipe_template_slots` | 0 | 5 | `role` | `text` | NO | `` |
| `recipe_template_slots` | 0 | 6 | `output_index` | `integer` | YES | `` |
| `recipe_template_slots` | 0 | 7 | `ordinal` | `integer` | NO | `` |
| `recipe_template_slots` | 0 | 8 | `x` | `numeric` | NO | `` |
| `recipe_template_slots` | 0 | 9 | `y` | `numeric` | NO | `` |
| `recipe_template_slots` | 0 | 10 | `width` | `numeric` | NO | `` |
| `recipe_template_slots` | 0 | 11 | `height` | `numeric` | NO | `` |
| `recipe_template_slots` | 0 | 12 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_type_catalysts` | 0 | 1 | `recipe_type_id` | `bigint` | NO | `` |
| `recipe_type_catalysts` | 0 | 2 | `resource_id` | `bigint` | NO | `` |
| `recipe_type_catalysts` | 0 | 3 | `ordinal` | `integer` | NO | `` |
| `recipe_type_catalysts` | 0 | 4 | `published_revision_id` | `bigint` | YES | `` |
| `recipe_type_definitions` | 0 | 1 | `recipe_type_id` | `bigint` | NO | `` |
| `recipe_type_definitions` | 0 | 2 | `definition` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_type_definitions` | 0 | 3 | `published_revision_id` | `bigint` | YES | `` |
| `recipe_type_definitions` | 0 | 4 | `updated_by` | `bigint` | YES | `` |
| `recipe_type_definitions` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_type_definitions` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `recipe_type_import_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `recipe_type_import_snapshots` | 0 | 2 | `recipe_type_id` | `bigint` | NO | `` |
| `recipe_type_import_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `recipe_type_import_snapshots` | 0 | 4 | `title_translation_key` | `text` | NO | `''::text` |
| `recipe_type_import_snapshots` | 0 | 5 | `title_names` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_type_import_snapshots` | 0 | 6 | `width` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 7 | `height` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 8 | `image_scale` | `integer` | NO | `1` |
| `recipe_type_import_snapshots` | 0 | 9 | `canvas` | `jsonb` | NO | `'{}'::jsonb` |
| `recipe_type_import_snapshots` | 0 | 10 | `catalysts` | `jsonb` | NO | `'[]'::jsonb` |
| `recipe_type_import_snapshots` | 0 | 11 | `recipe_count` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 12 | `exported_recipe_count` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 13 | `template_count` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 14 | `background_count` | `integer` | NO | `0` |
| `recipe_type_import_snapshots` | 0 | 15 | `template_collection_path` | `text` | NO | `''::text` |
| `recipe_type_import_snapshots` | 0 | 16 | `recipe_collection_path` | `text` | NO | `''::text` |
| `recipe_types` | 0 | 1 | `entity_id` | `bigint` | NO | `` |
| `recipe_types` | 0 | 2 | `canonical_id` | `text` | NO | `` |
| `recipe_version_bindings` | 0 | 1 | `recipe_id` | `bigint` | NO | `` |
| `recipe_version_bindings` | 0 | 2 | `version_code` | `text` | NO | `` |
| `recipe_version_bindings` | 0 | 3 | `created_by` | `bigint` | YES | `` |
| `recipe_version_bindings` | 0 | 4 | `source` | `text` | NO | `'import'::text` |
| `recipe_version_bindings` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `recipes` | 0 | 1 | `entity_id` | `bigint` | NO | `` |
| `recipes` | 0 | 2 | `recipe_type_id` | `bigint` | NO | `` |
| `recipes` | 0 | 3 | `canonical_source_id` | `text` | YES | `` |
| `recipes` | 0 | 4 | `semantic_fingerprint` | `text` | NO | `` |
| `recipes` | 0 | 5 | `owner_mod_id` | `bigint` | YES | `` |
| `recipes` | 0 | 6 | `identity_source` | `text` | NO | `` |
| `recipes` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `report_evidence` | 0 | 1 | `id` | `bigint` | NO | `nextval('report_evidence_id_seq'::regclass)` |
| `report_evidence` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `report_evidence` | 0 | 3 | `uploader_id` | `bigint` | NO | `` |
| `report_evidence` | 0 | 4 | `report_id` | `bigint` | YES | `` |
| `report_evidence` | 0 | 5 | `object_key` | `text` | NO | `` |
| `report_evidence` | 0 | 6 | `original_name` | `text` | NO | `` |
| `report_evidence` | 0 | 7 | `content_type` | `text` | NO | `` |
| `report_evidence` | 0 | 8 | `byte_size` | `bigint` | NO | `` |
| `report_evidence` | 0 | 9 | `sha256` | `text` | NO | `` |
| `report_evidence` | 0 | 10 | `scan_status` | `text` | NO | `'pending'::text` |
| `report_evidence` | 0 | 11 | `status` | `text` | NO | `'temporary'::text` |
| `report_evidence` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `report_evidence` | 0 | 13 | `cleanup_after` | `timestamp with time zone` | NO | `(now() + '24:00:00'::interval)` |
| `report_evidence` | 0 | 14 | `deleted_at` | `timestamp with time zone` | YES | `` |
| `report_evidence` | 0 | 15 | `delete_attempts` | `integer` | NO | `0` |
| `report_evidence` | 0 | 16 | `last_error` | `text` | NO | `''::text` |
| `report_reviews` | 0 | 1 | `id` | `bigint` | NO | `nextval('report_reviews_id_seq'::regclass)` |
| `report_reviews` | 0 | 2 | `report_id` | `bigint` | NO | `` |
| `report_reviews` | 0 | 3 | `reviewer_id` | `bigint` | NO | `` |
| `report_reviews` | 0 | 4 | `conclusion` | `text` | NO | `` |
| `report_reviews` | 0 | 5 | `note` | `text` | NO | `''::text` |
| `report_reviews` | 0 | 6 | `idempotency_key` | `text` | NO | `` |
| `report_reviews` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `report_snapshots` | 0 | 1 | `report_id` | `bigint` | NO | `` |
| `report_snapshots` | 0 | 2 | `schema_version` | `integer` | NO | `1` |
| `report_snapshots` | 0 | 3 | `payload` | `jsonb` | NO | `` |
| `report_snapshots` | 0 | 4 | `content_sha256` | `text` | NO | `` |
| `report_snapshots` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `reports` | 0 | 1 | `id` | `bigint` | NO | `nextval('reports_id_seq'::regclass)` |
| `reports` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `reports` | 0 | 3 | `target_type` | `text` | NO | `` |
| `reports` | 0 | 4 | `target_public_id` | `text` | NO | `` |
| `reports` | 0 | 5 | `target_author_id` | `bigint` | YES | `` |
| `reports` | 0 | 6 | `reporter_id` | `bigint` | NO | `` |
| `reports` | 0 | 7 | `reason_code` | `text` | NO | `` |
| `reports` | 0 | 8 | `reason_version` | `integer` | NO | `1` |
| `reports` | 0 | 9 | `reason_text_snapshot` | `text` | NO | `` |
| `reports` | 0 | 10 | `custom_reason` | `text` | NO | `''::text` |
| `reports` | 0 | 11 | `detail` | `text` | NO | `''::text` |
| `reports` | 0 | 12 | `status` | `text` | NO | `'pending'::text` |
| `reports` | 0 | 13 | `claimed_by` | `bigint` | YES | `` |
| `reports` | 0 | 14 | `claimed_at` | `timestamp with time zone` | YES | `` |
| `reports` | 0 | 15 | `lock_version` | `bigint` | NO | `1` |
| `reports` | 0 | 16 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `reports` | 0 | 17 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `reports` | 0 | 18 | `resolved_at` | `timestamp with time zone` | YES | `` |
| `resource_import_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `resource_import_snapshots` | 0 | 2 | `resource_id` | `bigint` | NO | `` |
| `resource_import_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `resource_import_snapshots` | 0 | 4 | `registry` | `text` | NO | `` |
| `resource_import_snapshots` | 0 | 5 | `entry_type_code` | `text` | NO | `'default'::text` |
| `resource_import_snapshots` | 0 | 6 | `definition_schema_version` | `smallint` | NO | `1` |
| `resource_import_snapshots` | 0 | 7 | `translation_key` | `text` | NO | `''::text` |
| `resource_import_snapshots` | 0 | 8 | `names` | `jsonb` | NO | `'{}'::jsonb` |
| `resource_import_snapshots` | 0 | 9 | `data` | `jsonb` | NO | `'{}'::jsonb` |
| `resource_import_snapshots` | 0 | 10 | `icon_path` | `text` | NO | `''::text` |
| `resource_import_snapshots` | 0 | 11 | `preview_path` | `text` | NO | `''::text` |
| `resource_import_snapshots` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `resource_kinds` | 18 | 1 | `code` | `text` | NO | `` |
| `resource_kinds` | 18 | 2 | `family` | `text` | NO | `` |
| `resource_kinds` | 18 | 3 | `display_order` | `integer` | NO | `0` |
| `resource_kinds` | 18 | 4 | `user_visible` | `boolean` | NO | `true` |
| `resource_kinds` | 18 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `review_completion_subscriptions` | 0 | 1 | `change_request_id` | `bigint` | NO | `` |
| `review_completion_subscriptions` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `review_completion_subscriptions` | 0 | 3 | `target_label` | `text` | NO | `''::text` |
| `review_completion_subscriptions` | 0 | 4 | `target_url` | `text` | NO | `''::text` |
| `review_completion_subscriptions` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `review_completion_subscriptions` | 0 | 6 | `notified_at` | `timestamp with time zone` | YES | `` |
| `review_events` | 11 | 1 | `id` | `bigint` | NO | `nextval('review_events_id_seq'::regclass)` |
| `review_events` | 11 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `review_events` | 11 | 3 | `change_request_id` | `bigint` | NO | `` |
| `review_events` | 11 | 4 | `event_type` | `text` | NO | `` |
| `review_events` | 11 | 5 | `actor_id` | `bigint` | YES | `` |
| `review_events` | 11 | 6 | `actor_snapshot` | `text` | NO | `''::text` |
| `review_events` | 11 | 7 | `note` | `text` | NO | `''::text` |
| `review_events` | 11 | 8 | `ip` | `text` | NO | `''::text` |
| `review_events` | 11 | 9 | `user_agent` | `text` | NO | `''::text` |
| `review_events` | 11 | 10 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `review_events` | 11 | 11 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `role_permissions` | 42 | 1 | `role_id` | `bigint` | NO | `` |
| `role_permissions` | 42 | 2 | `permission_id` | `bigint` | NO | `` |
| `role_permissions` | 42 | 3 | `allow` | `boolean` | NO | `true` |
| `role_permissions` | 42 | 4 | `expires_at` | `timestamp with time zone` | YES | `` |
| `role_permissions` | 42 | 5 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `roles` | 5 | 1 | `id` | `bigint` | NO | `nextval('roles_id_seq'::regclass)` |
| `roles` | 5 | 2 | `code` | `text` | NO | `` |
| `roles` | 5 | 3 | `name` | `text` | NO | `` |
| `roles` | 5 | 4 | `description` | `text` | NO | `''::text` |
| `roles` | 5 | 5 | `weight` | `integer` | NO | `0` |
| `roles` | 5 | 6 | `parents` | `ARRAY` | NO | `'{}'::text[]` |
| `roles` | 5 | 7 | `status` | `text` | NO | `'active'::text` |
| `roles` | 5 | 8 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `roles` | 5 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `roles` | 5 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `runtime_versions` | 3 | 1 | `name` | `text` | NO | `` |
| `runtime_versions` | 3 | 2 | `version` | `bigint` | NO | `1` |
| `runtime_versions` | 3 | 3 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `schema_metadata` | 1 | 1 | `singleton` | `boolean` | NO | `true` |
| `schema_metadata` | 1 | 2 | `generation` | `integer` | NO | `` |
| `schema_metadata` | 1 | 3 | `installed_at` | `timestamp with time zone` | NO | `now()` |
| `search_index_queue` | 17 | 1 | `document_type` | `text` | NO | `` |
| `search_index_queue` | 17 | 2 | `document_id` | `bigint` | NO | `` |
| `search_index_queue` | 17 | 3 | `operation` | `text` | NO | `'upsert'::text` |
| `search_index_queue` | 17 | 4 | `attempts` | `integer` | NO | `0` |
| `search_index_queue` | 17 | 5 | `available_at` | `timestamp with time zone` | NO | `now()` |
| `search_index_queue` | 17 | 6 | `last_error` | `text` | NO | `''::text` |
| `search_index_queue` | 17 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `search_index_queue` | 17 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `search_index_state` | 0 | 1 | `collection_kind` | `text` | NO | `` |
| `search_index_state` | 0 | 2 | `schema_version` | `integer` | NO | `` |
| `search_index_state` | 0 | 3 | `collection_name` | `text` | NO | `` |
| `search_index_state` | 0 | 4 | `rebuilt_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_candidates` | 0 | 1 | `id` | `bigint` | NO | `nextval('seed_crawler_candidates_id_seq'::regclass)` |
| `seed_crawler_candidates` | 0 | 2 | `run_id` | `bigint` | YES | `` |
| `seed_crawler_candidates` | 0 | 3 | `external_project_id` | `text` | NO | `` |
| `seed_crawler_candidates` | 0 | 4 | `project_type` | `text` | NO | `` |
| `seed_crawler_candidates` | 0 | 5 | `downloads` | `bigint` | NO | `0` |
| `seed_crawler_candidates` | 0 | 6 | `status` | `text` | NO | `'candidate'::text` |
| `seed_crawler_candidates` | 0 | 7 | `payload` | `jsonb` | NO | `'{}'::jsonb` |
| `seed_crawler_candidates` | 0 | 8 | `last_error` | `text` | NO | `''::text` |
| `seed_crawler_candidates` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_candidates` | 0 | 10 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_configs` | 1 | 1 | `id` | `boolean` | NO | `true` |
| `seed_crawler_configs` | 1 | 2 | `enabled` | `boolean` | NO | `false` |
| `seed_crawler_configs` | 1 | 3 | `project_types` | `ARRAY` | NO | `ARRAY['mod'::text, 'plugin'::text, 'shader_pack'::text, 'resource_pack'::text]` |
| `seed_crawler_configs` | 1 | 4 | `batch_size` | `integer` | NO | `1` |
| `seed_crawler_configs` | 1 | 5 | `daily_limit` | `integer` | NO | `20` |
| `seed_crawler_configs` | 1 | 6 | `minimum_downloads` | `bigint` | NO | `100` |
| `seed_crawler_configs` | 1 | 7 | `interval_seconds` | `integer` | NO | `21600` |
| `seed_crawler_configs` | 1 | 8 | `max_concurrency` | `integer` | NO | `1` |
| `seed_crawler_configs` | 1 | 9 | `ai_daily_token_budget` | `bigint` | NO | `200000` |
| `seed_crawler_configs` | 1 | 10 | `auto_submit_review` | `boolean` | NO | `false` |
| `seed_crawler_configs` | 1 | 11 | `last_run_at` | `timestamp with time zone` | YES | `` |
| `seed_crawler_configs` | 1 | 12 | `next_run_at` | `timestamp with time zone` | YES | `` |
| `seed_crawler_configs` | 1 | 13 | `updated_by` | `bigint` | YES | `` |
| `seed_crawler_configs` | 1 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_runs` | 0 | 1 | `id` | `bigint` | NO | `nextval('seed_crawler_runs_id_seq'::regclass)` |
| `seed_crawler_runs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `seed_crawler_runs` | 0 | 3 | `status` | `text` | NO | `'pending'::text` |
| `seed_crawler_runs` | 0 | 4 | `dry_run` | `boolean` | NO | `false` |
| `seed_crawler_runs` | 0 | 5 | `requested_by` | `bigint` | YES | `` |
| `seed_crawler_runs` | 0 | 6 | `actor_id` | `bigint` | YES | `` |
| `seed_crawler_runs` | 0 | 7 | `lease_owner` | `text` | NO | `''::text` |
| `seed_crawler_runs` | 0 | 8 | `lease_expires_at` | `timestamp with time zone` | YES | `` |
| `seed_crawler_runs` | 0 | 9 | `attempts` | `integer` | NO | `0` |
| `seed_crawler_runs` | 0 | 10 | `next_attempt_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_runs` | 0 | 11 | `stats` | `jsonb` | NO | `'{}'::jsonb` |
| `seed_crawler_runs` | 0 | 12 | `last_error` | `text` | NO | `''::text` |
| `seed_crawler_runs` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `seed_crawler_runs` | 0 | 14 | `started_at` | `timestamp with time zone` | YES | `` |
| `seed_crawler_runs` | 0 | 15 | `finished_at` | `timestamp with time zone` | YES | `` |
| `seed_crawler_translation_tasks` | 0 | 1 | `candidate_id` | `bigint` | NO | `` |
| `seed_crawler_translation_tasks` | 0 | 2 | `locale` | `text` | NO | `` |
| `seed_crawler_translation_tasks` | 0 | 3 | `status` | `text` | NO | `'pending'::text` |
| `seed_crawler_translation_tasks` | 0 | 4 | `attempts` | `integer` | NO | `0` |
| `seed_crawler_translation_tasks` | 0 | 5 | `input_tokens` | `bigint` | NO | `0` |
| `seed_crawler_translation_tasks` | 0 | 6 | `output_tokens` | `bigint` | NO | `0` |
| `seed_crawler_translation_tasks` | 0 | 7 | `last_error` | `text` | NO | `''::text` |
| `seed_crawler_translation_tasks` | 0 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `shop_items` | 3 | 1 | `id` | `bigint` | NO | `nextval('shop_items_id_seq'::regclass)` |
| `shop_items` | 3 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `shop_items` | 3 | 3 | `code` | `text` | NO | `` |
| `shop_items` | 3 | 4 | `item_type` | `text` | NO | `` |
| `shop_items` | 3 | 5 | `name` | `text` | NO | `` |
| `shop_items` | 3 | 6 | `description` | `text` | NO | `''::text` |
| `shop_items` | 3 | 7 | `icon` | `text` | NO | `''::text` |
| `shop_items` | 3 | 8 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `shop_items` | 3 | 9 | `price_currency_id` | `bigint` | NO | `` |
| `shop_items` | 3 | 10 | `price_amount` | `bigint` | NO | `0` |
| `shop_items` | 3 | 11 | `purchase_permission` | `text` | NO | `''::text` |
| `shop_items` | 3 | 12 | `use_permission` | `text` | NO | `''::text` |
| `shop_items` | 3 | 13 | `config` | `jsonb` | NO | `'{}'::jsonb` |
| `shop_items` | 3 | 14 | `status` | `text` | NO | `'active'::text` |
| `shop_items` | 3 | 15 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `shop_items` | 3 | 16 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `shop_purchases` | 0 | 1 | `id` | `bigint` | NO | `nextval('shop_purchases_id_seq'::regclass)` |
| `shop_purchases` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `shop_purchases` | 0 | 3 | `shop_item_id` | `bigint` | NO | `` |
| `shop_purchases` | 0 | 4 | `currency_id` | `bigint` | NO | `` |
| `shop_purchases` | 0 | 5 | `quantity` | `integer` | NO | `` |
| `shop_purchases` | 0 | 6 | `unit_price` | `bigint` | NO | `` |
| `shop_purchases` | 0 | 7 | `total_price` | `bigint` | NO | `` |
| `shop_purchases` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `simple_project_gallery_images` | 0 | 1 | `id` | `bigint` | NO | `nextval('simple_project_gallery_images_id_seq'::regclass)` |
| `simple_project_gallery_images` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `simple_project_gallery_images` | 0 | 3 | `project_id` | `bigint` | NO | `` |
| `simple_project_gallery_images` | 0 | 4 | `oss_file_id` | `bigint` | NO | `` |
| `simple_project_gallery_images` | 0 | 5 | `display_order` | `integer` | NO | `0` |
| `simple_project_gallery_images` | 0 | 6 | `created_by` | `bigint` | YES | `` |
| `simple_project_gallery_images` | 0 | 7 | `published_revision_id` | `bigint` | YES | `` |
| `simple_project_gallery_images` | 0 | 8 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `simple_project_links` | 0 | 1 | `id` | `bigint` | NO | `nextval('simple_project_links_id_seq'::regclass)` |
| `simple_project_links` | 0 | 2 | `project_id` | `bigint` | NO | `` |
| `simple_project_links` | 0 | 3 | `link_type` | `text` | NO | `` |
| `simple_project_links` | 0 | 4 | `url` | `text` | NO | `` |
| `simple_project_links` | 0 | 5 | `note` | `text` | NO | `''::text` |
| `simple_project_links` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `simple_project_localizations` | 0 | 1 | `project_id` | `bigint` | NO | `` |
| `simple_project_localizations` | 0 | 2 | `locale` | `text` | NO | `` |
| `simple_project_localizations` | 0 | 3 | `name` | `text` | NO | `` |
| `simple_project_localizations` | 0 | 4 | `summary` | `text` | NO | `''::text` |
| `simple_project_localizations` | 0 | 5 | `body_markdown` | `text` | NO | `''::text` |
| `simple_project_parent_refs` | 0 | 1 | `id` | `bigint` | NO | `nextval('simple_project_parent_refs_id_seq'::regclass)` |
| `simple_project_parent_refs` | 0 | 2 | `project_id` | `bigint` | NO | `` |
| `simple_project_parent_refs` | 0 | 3 | `target_type` | `text` | NO | `` |
| `simple_project_parent_refs` | 0 | 4 | `target_id` | `bigint` | YES | `` |
| `simple_project_parent_refs` | 0 | 5 | `raw_identifier` | `text` | NO | `''::text` |
| `simple_project_parent_refs` | 0 | 6 | `display_order` | `integer` | NO | `0` |
| `simple_projects` | 0 | 1 | `id` | `bigint` | NO | `nextval('simple_projects_id_seq'::regclass)` |
| `simple_projects` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `simple_projects` | 0 | 3 | `project_type` | `text` | NO | `` |
| `simple_projects` | 0 | 4 | `slug` | `text` | NO | `` |
| `simple_projects` | 0 | 5 | `default_locale` | `text` | NO | `'zh-CN'::text` |
| `simple_projects` | 0 | 6 | `primary_name` | `text` | NO | `` |
| `simple_projects` | 0 | 7 | `summary` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 8 | `body_markdown` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 9 | `abbreviation` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 10 | `minecraft_versions` | `ARRAY` | NO | `'{}'::text[]` |
| `simple_projects` | 0 | 11 | `loaders` | `ARRAY` | NO | `'{}'::text[]` |
| `simple_projects` | 0 | 12 | `categories` | `ARRAY` | NO | `'{}'::text[]` |
| `simple_projects` | 0 | 13 | `features` | `ARRAY` | NO | `'{}'::text[]` |
| `simple_projects` | 0 | 14 | `resolution` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 15 | `performance` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 16 | `map_size` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 17 | `official_status` | `text` | NO | `'development'::text` |
| `simple_projects` | 0 | 18 | `source_status` | `text` | NO | `'unknown'::text` |
| `simple_projects` | 0 | 19 | `license` | `text` | NO | `'Custom'::text` |
| `simple_projects` | 0 | 20 | `curseforge_project_id` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 21 | `modrinth_project_id` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 22 | `icon_url` | `text` | NO | `''::text` |
| `simple_projects` | 0 | 23 | `search_keywords` | `ARRAY` | NO | `'{}'::text[]` |
| `simple_projects` | 0 | 24 | `submission_method` | `text` | NO | `'manual'::text` |
| `simple_projects` | 0 | 25 | `review_status` | `text` | NO | `'pending'::text` |
| `simple_projects` | 0 | 26 | `submitted_by` | `bigint` | YES | `` |
| `simple_projects` | 0 | 27 | `published_revision_id` | `bigint` | YES | `` |
| `simple_projects` | 0 | 28 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `simple_projects` | 0 | 29 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `simple_projects` | 0 | 30 | `published_at` | `timestamp with time zone` | YES | `` |
| `site_changelog_translations` | 0 | 1 | `changelog_id` | `bigint` | NO | `` |
| `site_changelog_translations` | 0 | 2 | `locale` | `text` | NO | `` |
| `site_changelog_translations` | 0 | 3 | `title` | `text` | NO | `` |
| `site_changelog_translations` | 0 | 4 | `body_markdown` | `text` | NO | `` |
| `site_changelog_translations` | 0 | 5 | `updated_by` | `bigint` | YES | `` |
| `site_changelog_translations` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `site_changelogs` | 0 | 1 | `id` | `bigint` | NO | `nextval('site_changelogs_id_seq'::regclass)` |
| `site_changelogs` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `site_changelogs` | 0 | 3 | `change_date` | `date` | NO | `` |
| `site_changelogs` | 0 | 4 | `status` | `text` | NO | `'draft'::text` |
| `site_changelogs` | 0 | 5 | `created_by` | `bigint` | YES | `` |
| `site_changelogs` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `site_changelogs` | 0 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `site_daily_active_users` | 1 | 1 | `activity_date` | `date` | NO | `` |
| `site_daily_active_users` | 1 | 2 | `user_id` | `bigint` | NO | `` |
| `site_daily_active_users` | 1 | 3 | `first_seen_at` | `timestamp with time zone` | NO | `` |
| `site_daily_metrics` | 30 | 1 | `metric_date` | `date` | NO | `` |
| `site_daily_metrics` | 30 | 2 | `active_users` | `bigint` | NO | `0` |
| `site_daily_metrics` | 30 | 3 | `views` | `bigint` | NO | `0` |
| `site_daily_metrics` | 30 | 4 | `actions` | `bigint` | NO | `0` |
| `site_daily_metrics` | 30 | 5 | `new_users` | `bigint` | NO | `0` |
| `site_daily_metrics` | 30 | 6 | `review_submissions` | `bigint` | NO | `0` |
| `site_daily_metrics` | 30 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `site_monthly_active_users` | 1 | 1 | `activity_month` | `date` | NO | `` |
| `site_monthly_active_users` | 1 | 2 | `user_id` | `bigint` | NO | `` |
| `site_monthly_active_users` | 1 | 3 | `last_active_date` | `date` | NO | `` |
| `site_page_translations` | 8 | 1 | `page_id` | `bigint` | NO | `` |
| `site_page_translations` | 8 | 2 | `locale` | `text` | NO | `` |
| `site_page_translations` | 8 | 3 | `title` | `text` | NO | `` |
| `site_page_translations` | 8 | 4 | `body_markdown` | `text` | NO | `` |
| `site_page_translations` | 8 | 5 | `status` | `text` | NO | `'draft'::text` |
| `site_page_translations` | 8 | 6 | `revision` | `bigint` | NO | `1` |
| `site_page_translations` | 8 | 7 | `updated_by` | `bigint` | YES | `` |
| `site_page_translations` | 8 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `site_pages` | 1 | 1 | `id` | `bigint` | NO | `nextval('site_pages_id_seq'::regclass)` |
| `site_pages` | 1 | 2 | `code` | `text` | NO | `` |
| `site_pages` | 1 | 3 | `status` | `text` | NO | `'draft'::text` |
| `site_pages` | 1 | 4 | `published_revision` | `bigint` | NO | `0` |
| `site_pages` | 1 | 5 | `updated_by` | `bigint` | YES | `` |
| `site_pages` | 1 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `site_pages` | 1 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `site_view_daily` | 1 | 1 | `metric_date` | `date` | NO | `CURRENT_DATE` |
| `site_view_daily` | 1 | 2 | `counter_shard` | `smallint` | NO | `` |
| `site_view_daily` | 1 | 3 | `views` | `bigint` | NO | `0` |
| `skin_asset_adoptions` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `skin_asset_adoptions` | 0 | 2 | `asset_id` | `bigint` | NO | `` |
| `skin_asset_adoptions` | 0 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `skin_assets` | 0 | 1 | `id` | `bigint` | NO | `nextval('skin_assets_id_seq'::regclass)` |
| `skin_assets` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `skin_assets` | 0 | 3 | `owner_id` | `bigint` | NO | `` |
| `skin_assets` | 0 | 4 | `blob_hash` | `text` | NO | `` |
| `skin_assets` | 0 | 5 | `kind` | `text` | NO | `` |
| `skin_assets` | 0 | 6 | `model` | `text` | NO | `'default'::text` |
| `skin_assets` | 0 | 7 | `display_name` | `text` | NO | `` |
| `skin_assets` | 0 | 8 | `description` | `text` | NO | `''::text` |
| `skin_assets` | 0 | 9 | `tags` | `ARRAY` | NO | `'{}'::text[]` |
| `skin_assets` | 0 | 10 | `visibility` | `text` | NO | `'private'::text` |
| `skin_assets` | 0 | 11 | `review_status` | `text` | NO | `'approved'::text` |
| `skin_assets` | 0 | 12 | `status` | `text` | NO | `'active'::text` |
| `skin_assets` | 0 | 13 | `downloads` | `bigint` | NO | `0` |
| `skin_assets` | 0 | 14 | `published_revision_id` | `bigint` | YES | `` |
| `skin_assets` | 0 | 15 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `skin_assets` | 0 | 16 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `skin_texture_blobs` | 0 | 1 | `hash` | `text` | NO | `` |
| `skin_texture_blobs` | 0 | 2 | `oss_file_id` | `bigint` | NO | `` |
| `skin_texture_blobs` | 0 | 3 | `object_key` | `text` | NO | `` |
| `skin_texture_blobs` | 0 | 4 | `width` | `integer` | NO | `` |
| `skin_texture_blobs` | 0 | 5 | `height` | `integer` | NO | `` |
| `skin_texture_blobs` | 0 | 6 | `size_bytes` | `bigint` | NO | `` |
| `skin_texture_blobs` | 0 | 7 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `skin_wardrobe` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `skin_wardrobe` | 0 | 2 | `asset_id` | `bigint` | NO | `` |
| `skin_wardrobe` | 0 | 3 | `added_at` | `timestamp with time zone` | NO | `now()` |
| `sticker_catalog_state` | 1 | 1 | `singleton` | `boolean` | NO | `true` |
| `sticker_catalog_state` | 1 | 2 | `version` | `bigint` | NO | `1` |
| `sticker_catalog_state` | 1 | 3 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `sticker_pack_translations` | 0 | 1 | `pack_id` | `bigint` | NO | `` |
| `sticker_pack_translations` | 0 | 2 | `locale` | `text` | NO | `` |
| `sticker_pack_translations` | 0 | 3 | `name` | `text` | NO | `` |
| `sticker_packs` | 0 | 1 | `id` | `bigint` | NO | `nextval('sticker_packs_id_seq'::regclass)` |
| `sticker_packs` | 0 | 2 | `code` | `text` | NO | `` |
| `sticker_packs` | 0 | 3 | `status` | `text` | NO | `'active'::text` |
| `sticker_packs` | 0 | 4 | `sort_order` | `integer` | NO | `0` |
| `sticker_packs` | 0 | 5 | `created_by` | `bigint` | YES | `` |
| `sticker_packs` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `sticker_packs` | 0 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `sticker_translations` | 0 | 1 | `sticker_id` | `bigint` | NO | `` |
| `sticker_translations` | 0 | 2 | `locale` | `text` | NO | `` |
| `sticker_translations` | 0 | 3 | `name` | `text` | NO | `` |
| `stickers` | 0 | 1 | `id` | `bigint` | NO | `nextval('stickers_id_seq'::regclass)` |
| `stickers` | 0 | 2 | `pack_id` | `bigint` | NO | `` |
| `stickers` | 0 | 3 | `code` | `text` | NO | `` |
| `stickers` | 0 | 4 | `image_file_id` | `bigint` | NO | `` |
| `stickers` | 0 | 5 | `mime_type` | `text` | NO | `` |
| `stickers` | 0 | 6 | `width` | `integer` | NO | `` |
| `stickers` | 0 | 7 | `height` | `integer` | NO | `` |
| `stickers` | 0 | 8 | `file_size` | `bigint` | NO | `` |
| `stickers` | 0 | 9 | `checksum` | `text` | NO | `` |
| `stickers` | 0 | 10 | `status` | `text` | NO | `'active'::text` |
| `stickers` | 0 | 11 | `sort_order` | `integer` | NO | `0` |
| `stickers` | 0 | 12 | `created_by` | `bigint` | YES | `` |
| `stickers` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `stickers` | 0 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `system_settings` | 2 | 1 | `key` | `text` | NO | `` |
| `system_settings` | 2 | 2 | `value` | `jsonb` | NO | `` |
| `system_settings` | 2 | 3 | `updated_by` | `bigint` | YES | `` |
| `system_settings` | 2 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `tag_import_members` | 0 | 1 | `tag_snapshot_id` | `text` | NO | `` |
| `tag_import_members` | 0 | 2 | `resource_id` | `bigint` | YES | `` |
| `tag_import_members` | 0 | 3 | `raw_member_id` | `text` | NO | `` |
| `tag_import_members` | 0 | 4 | `ordinal` | `integer` | NO | `` |
| `tag_import_snapshots` | 0 | 1 | `id` | `text` | NO | `` |
| `tag_import_snapshots` | 0 | 2 | `tag_id` | `bigint` | NO | `` |
| `tag_import_snapshots` | 0 | 3 | `revision_id` | `text` | NO | `` |
| `tag_import_snapshots` | 0 | 4 | `member_count` | `integer` | NO | `0` |
| `task_definitions` | 0 | 1 | `id` | `bigint` | NO | `nextval('task_definitions_id_seq'::regclass)` |
| `task_definitions` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `task_definitions` | 0 | 3 | `code` | `text` | NO | `` |
| `task_definitions` | 0 | 4 | `name` | `text` | NO | `` |
| `task_definitions` | 0 | 5 | `description` | `text` | NO | `''::text` |
| `task_definitions` | 0 | 6 | `icon` | `text` | NO | `''::text` |
| `task_definitions` | 0 | 7 | `translations` | `jsonb` | NO | `'{}'::jsonb` |
| `task_definitions` | 0 | 8 | `refresh_period` | `text` | NO | `'never'::text` |
| `task_definitions` | 0 | 9 | `condition` | `jsonb` | NO | `'{}'::jsonb` |
| `task_definitions` | 0 | 10 | `rewards` | `jsonb` | NO | `'{}'::jsonb` |
| `task_definitions` | 0 | 11 | `status` | `text` | NO | `'active'::text` |
| `task_definitions` | 0 | 12 | `created_by` | `bigint` | YES | `` |
| `task_definitions` | 0 | 13 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `task_definitions` | 0 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `top_level_project_catalog` | 0 | 1 | `object_route_id` | `bigint` | YES | `` |
| `top_level_project_catalog` | 0 | 2 | `public_id` | `character varying` | YES | `` |
| `top_level_project_catalog` | 0 | 3 | `entity_type` | `text` | YES | `` |
| `top_level_project_catalog` | 0 | 4 | `canonical_path` | `text` | YES | `` |
| `top_level_project_catalog` | 0 | 5 | `name` | `text` | YES | `` |
| `top_level_project_catalog` | 0 | 6 | `review_status` | `text` | YES | `` |
| `top_level_project_catalog` | 0 | 7 | `submitted_by` | `bigint` | YES | `` |
| `top_level_project_catalog` | 0 | 8 | `created_at` | `timestamp with time zone` | YES | `` |
| `top_level_project_catalog` | 0 | 9 | `updated_at` | `timestamp with time zone` | YES | `` |
| `unresolved_references` | 0 | 1 | `id` | `bigint` | NO | `nextval('unresolved_references_id_seq'::regclass)` |
| `unresolved_references` | 0 | 2 | `source_type` | `text` | NO | `` |
| `unresolved_references` | 0 | 3 | `source_id` | `bigint` | NO | `` |
| `unresolved_references` | 0 | 4 | `field_path` | `text` | NO | `''::text` |
| `unresolved_references` | 0 | 5 | `reference_type` | `text` | NO | `` |
| `unresolved_references` | 0 | 6 | `raw_identifier` | `text` | NO | `` |
| `unresolved_references` | 0 | 7 | `normalized_identifier` | `text` | NO | `` |
| `unresolved_references` | 0 | 8 | `resolved_type` | `text` | NO | `''::text` |
| `unresolved_references` | 0 | 9 | `resolved_id` | `bigint` | YES | `` |
| `unresolved_references` | 0 | 10 | `status` | `text` | NO | `'pending'::text` |
| `unresolved_references` | 0 | 11 | `metadata` | `jsonb` | NO | `'{}'::jsonb` |
| `unresolved_references` | 0 | 12 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `unresolved_references` | 0 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `unresolved_references` | 0 | 14 | `resolved_at` | `timestamp with time zone` | YES | `` |
| `unresolved_resource_references` | 0 | 1 | `id` | `bigint` | NO | `nextval('unresolved_resource_references_id_seq'::regclass)` |
| `unresolved_resource_references` | 0 | 2 | `source_entity_id` | `bigint` | NO | `` |
| `unresolved_resource_references` | 0 | 3 | `source_revision_id` | `text` | YES | `` |
| `unresolved_resource_references` | 0 | 4 | `field_path` | `text` | NO | `` |
| `unresolved_resource_references` | 0 | 5 | `kind_code` | `text` | NO | `` |
| `unresolved_resource_references` | 0 | 6 | `raw_resource_id` | `text` | NO | `` |
| `unresolved_resource_references` | 0 | 7 | `resolved_resource_id` | `bigint` | YES | `` |
| `unresolved_resource_references` | 0 | 8 | `status` | `text` | NO | `'pending'::text` |
| `unresolved_resource_references` | 0 | 9 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `unresolved_resource_references` | 0 | 10 | `resolved_at` | `timestamp with time zone` | YES | `` |
| `user_activity_events` | 36 | 1 | `id` | `bigint` | NO | `nextval('user_activity_events_id_seq'::regclass)` |
| `user_activity_events` | 36 | 2 | `user_id` | `bigint` | YES | `` |
| `user_activity_events` | 36 | 3 | `action_id` | `smallint` | NO | `` |
| `user_activity_events` | 36 | 4 | `object_type_id` | `smallint` | NO | `` |
| `user_activity_events` | 36 | 5 | `object_route_id` | `bigint` | YES | `` |
| `user_activity_events` | 36 | 6 | `markdown_added_bytes` | `integer` | NO | `0` |
| `user_activity_events` | 36 | 7 | `occurred_at` | `timestamp with time zone` | NO | `` |
| `user_activity_events` | 36 | 8 | `markdown_deleted_bytes` | `integer` | NO | `0` |
| `user_blocks` | 0 | 1 | `blocker_id` | `bigint` | NO | `` |
| `user_blocks` | 0 | 2 | `blocked_id` | `bigint` | NO | `` |
| `user_blocks` | 0 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_chat_presence` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_chat_presence` | 0 | 2 | `conversation_id` | `bigint` | NO | `` |
| `user_chat_presence` | 0 | 3 | `expires_at` | `timestamp with time zone` | NO | `` |
| `user_chat_presence` | 0 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_checkins` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_checkins` | 0 | 2 | `local_date` | `date` | NO | `` |
| `user_checkins` | 0 | 3 | `timezone` | `text` | NO | `` |
| `user_checkins` | 0 | 4 | `claimed_at` | `timestamp with time zone` | NO | `now()` |
| `user_content_creation_facts` | 0 | 1 | `content_type` | `text` | NO | `` |
| `user_content_creation_facts` | 0 | 2 | `object_key` | `text` | NO | `` |
| `user_content_creation_facts` | 0 | 3 | `user_id` | `bigint` | NO | `` |
| `user_content_creation_facts` | 0 | 4 | `review_status` | `text` | NO | `'pending'::text` |
| `user_content_creation_facts` | 0 | 5 | `current_exists` | `boolean` | NO | `true` |
| `user_content_creation_facts` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `` |
| `user_content_creation_facts` | 0 | 7 | `deleted_at` | `timestamp with time zone` | YES | `` |
| `user_content_creation_facts` | 0 | 8 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_currency_balances` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_currency_balances` | 0 | 2 | `currency_id` | `bigint` | NO | `` |
| `user_currency_balances` | 0 | 3 | `balance` | `bigint` | NO | `0` |
| `user_currency_balances` | 0 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_daily_contributions` | 2 | 1 | `user_id` | `bigint` | NO | `` |
| `user_daily_contributions` | 2 | 2 | `contribution_date` | `date` | NO | `` |
| `user_daily_contributions` | 2 | 3 | `contribution_count` | `integer` | NO | `` |
| `user_drafts` | 2 | 1 | `id` | `bigint` | NO | `nextval('user_drafts_id_seq'::regclass)` |
| `user_drafts` | 2 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `user_drafts` | 2 | 3 | `user_id` | `bigint` | NO | `` |
| `user_drafts` | 2 | 4 | `draft_key` | `text` | NO | `` |
| `user_drafts` | 2 | 5 | `project_key` | `text` | NO | `` |
| `user_drafts` | 2 | 6 | `project_title` | `text` | NO | `''::text` |
| `user_drafts` | 2 | 7 | `kind` | `text` | NO | `` |
| `user_drafts` | 2 | 8 | `title` | `text` | NO | `''::text` |
| `user_drafts` | 2 | 9 | `edit_url` | `text` | NO | `` |
| `user_drafts` | 2 | 10 | `target_url` | `text` | NO | `''::text` |
| `user_drafts` | 2 | 11 | `payload` | `jsonb` | NO | `` |
| `user_drafts` | 2 | 12 | `change_request_id` | `bigint` | YES | `` |
| `user_drafts` | 2 | 13 | `review_target_type` | `text` | NO | `''::text` |
| `user_drafts` | 2 | 14 | `review_target_id` | `bigint` | YES | `` |
| `user_drafts` | 2 | 15 | `submitted_status` | `text` | NO | `''::text` |
| `user_drafts` | 2 | 16 | `submitted_at` | `timestamp with time zone` | YES | `` |
| `user_drafts` | 2 | 17 | `expires_at` | `timestamp with time zone` | NO | `` |
| `user_drafts` | 2 | 18 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_drafts` | 2 | 19 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_experience` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_experience` | 0 | 2 | `experience` | `bigint` | NO | `0` |
| `user_experience` | 0 | 3 | `level` | `integer` | NO | `0` |
| `user_experience` | 0 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_follows` | 0 | 1 | `follower_id` | `bigint` | NO | `` |
| `user_follows` | 0 | 2 | `followed_id` | `bigint` | NO | `` |
| `user_follows` | 0 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_inventory` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_inventory` | 0 | 2 | `shop_item_id` | `bigint` | NO | `` |
| `user_inventory` | 0 | 3 | `quantity` | `integer` | NO | `0` |
| `user_inventory` | 0 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_login_logs` | 1 | 1 | `id` | `bigint` | NO | `nextval('user_login_logs_id_seq'::regclass)` |
| `user_login_logs` | 1 | 2 | `user_id` | `bigint` | YES | `` |
| `user_login_logs` | 1 | 3 | `account` | `text` | NO | `` |
| `user_login_logs` | 1 | 4 | `ip` | `text` | NO | `''::text` |
| `user_login_logs` | 1 | 5 | `user_agent` | `text` | NO | `''::text` |
| `user_login_logs` | 1 | 6 | `country_code` | `text` | NO | `''::text` |
| `user_login_logs` | 1 | 7 | `city` | `text` | NO | `''::text` |
| `user_login_logs` | 1 | 8 | `success` | `boolean` | NO | `` |
| `user_login_logs` | 1 | 9 | `reason` | `text` | NO | `''::text` |
| `user_login_logs` | 1 | 10 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_notification_settings` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_notification_settings` | 0 | 2 | `email_enabled` | `boolean` | NO | `false` |
| `user_notification_settings` | 0 | 3 | `project_updates_enabled` | `boolean` | NO | `true` |
| `user_notification_settings` | 0 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_permissions` | 121 | 1 | `user_id` | `bigint` | NO | `` |
| `user_permissions` | 121 | 2 | `permission_id` | `bigint` | NO | `` |
| `user_permissions` | 121 | 3 | `allow` | `boolean` | NO | `true` |
| `user_permissions` | 121 | 4 | `context` | `text` | NO | `''::text` |
| `user_permissions` | 121 | 5 | `expires_at` | `timestamp with time zone` | YES | `` |
| `user_permissions` | 121 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_permissions` | 121 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_presence_sessions` | 1 | 1 | `session_hash` | `bytea` | NO | `` |
| `user_presence_sessions` | 1 | 2 | `user_id` | `bigint` | NO | `` |
| `user_presence_sessions` | 1 | 3 | `last_active_at` | `timestamp with time zone` | NO | `now()` |
| `user_presence_sessions` | 1 | 4 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_registration_attempts` | 0 | 1 | `id` | `bigint` | NO | `nextval('user_registration_attempts_id_seq'::regclass)` |
| `user_registration_attempts` | 0 | 2 | `ip` | `text` | NO | `` |
| `user_registration_attempts` | 0 | 3 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_role_bindings` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_role_bindings` | 0 | 2 | `role_id` | `bigint` | NO | `` |
| `user_role_bindings` | 0 | 3 | `expires_at` | `timestamp with time zone` | YES | `` |
| `user_role_bindings` | 0 | 4 | `context` | `text` | NO | `''::text` |
| `user_role_bindings` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `user_statistics_daily` | 2 | 1 | `user_id` | `bigint` | NO | `` |
| `user_statistics_daily` | 2 | 2 | `stat_date` | `date` | NO | `` |
| `user_statistics_daily` | 2 | 3 | `action_count` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 4 | `view_count` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 5 | `edit_count` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 6 | `create_count` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 7 | `delete_count` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 8 | `markdown_added_bytes` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 9 | `markdown_deleted_bytes` | `bigint` | NO | `0` |
| `user_statistics_daily` | 2 | 10 | `action_counts` | `jsonb` | NO | `'{}'::jsonb` |
| `user_statistics_daily` | 2 | 11 | `first_activity_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_daily` | 2 | 12 | `last_activity_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_daily` | 2 | 13 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_statistics_totals` | 2 | 1 | `user_id` | `bigint` | NO | `` |
| `user_statistics_totals` | 2 | 2 | `action_count` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 3 | `view_count` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 4 | `edit_count` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 5 | `create_count` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 6 | `delete_count` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 7 | `markdown_added_bytes` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 8 | `markdown_deleted_bytes` | `bigint` | NO | `0` |
| `user_statistics_totals` | 2 | 9 | `action_counts` | `jsonb` | NO | `'{}'::jsonb` |
| `user_statistics_totals` | 2 | 10 | `first_activity_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_totals` | 2 | 11 | `last_activity_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_totals` | 2 | 12 | `last_edit_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_totals` | 2 | 13 | `last_comment_at` | `timestamp with time zone` | YES | `` |
| `user_statistics_totals` | 2 | 14 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_task_progress` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `user_task_progress` | 0 | 2 | `task_id` | `bigint` | NO | `` |
| `user_task_progress` | 0 | 3 | `period_key` | `text` | NO | `` |
| `user_task_progress` | 0 | 4 | `progress` | `bigint` | NO | `0` |
| `user_task_progress` | 0 | 5 | `completed_at` | `timestamp with time zone` | YES | `` |
| `user_task_progress` | 0 | 6 | `rewarded_at` | `timestamp with time zone` | YES | `` |
| `user_task_progress` | 0 | 7 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `user_timezone_changes` | 0 | 1 | `id` | `bigint` | NO | `nextval('user_timezone_changes_id_seq'::regclass)` |
| `user_timezone_changes` | 0 | 2 | `user_id` | `bigint` | NO | `` |
| `user_timezone_changes` | 0 | 3 | `old_timezone` | `text` | NO | `` |
| `user_timezone_changes` | 0 | 4 | `new_timezone` | `text` | NO | `` |
| `user_timezone_changes` | 0 | 5 | `changed_at` | `timestamp with time zone` | NO | `now()` |
| `users` | 10 | 1 | `id` | `bigint` | NO | `nextval('users_id_seq'::regclass)` |
| `users` | 10 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `users` | 10 | 3 | `username` | `text` | NO | `` |
| `users` | 10 | 4 | `email` | `text` | NO | `` |
| `users` | 10 | 5 | `password_hash` | `text` | NO | `` |
| `users` | 10 | 6 | `email_verified` | `boolean` | NO | `false` |
| `users` | 10 | 7 | `status` | `text` | NO | `'active'::text` |
| `users` | 10 | 8 | `country` | `text` | NO | `''::text` |
| `users` | 10 | 9 | `timezone` | `text` | NO | `'Asia/Shanghai'::text` |
| `users` | 10 | 10 | `preferred_content_language` | `text` | NO | `'zh-CN'::text` |
| `users` | 10 | 11 | `preferred_ui_language` | `text` | NO | `'en-US'::text` |
| `users` | 10 | 12 | `auth_version` | `bigint` | NO | `1` |
| `users` | 10 | 13 | `permission_version` | `bigint` | NO | `1` |
| `users` | 10 | 14 | `security_score` | `integer` | NO | `100` |
| `users` | 10 | 15 | `registration_ip` | `text` | NO | `''::text` |
| `users` | 10 | 16 | `registration_country_code` | `text` | NO | `''::text` |
| `users` | 10 | 17 | `registration_city` | `text` | NO | `''::text` |
| `users` | 10 | 18 | `signature` | `text` | NO | `''::text` |
| `users` | 10 | 19 | `avatar_url` | `text` | NO | `''::text` |
| `users` | 10 | 20 | `avatar_file_id` | `bigint` | YES | `` |
| `users` | 10 | 21 | `profile_revision_id` | `bigint` | YES | `` |
| `users` | 10 | 22 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `users` | 10 | 23 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `users` | 10 | 24 | `last_login_at` | `timestamp with time zone` | YES | `` |
| `users` | 10 | 25 | `secondary_content_language` | `text` | NO | `'en-US'::text` |
| `users` | 10 | 26 | `profile_background_url` | `text` | NO | `''::text` |
| `users` | 10 | 27 | `profile_background_file_id` | `bigint` | YES | `` |
| `users` | 10 | 28 | `show_online_status` | `boolean` | NO | `false` |
| `users` | 10 | 29 | `public_card_stat_slots` | `ARRAY` | NO | `ARRAY[''::text, ''::text, ''::text, ''::text, ''::text, ''::text]` |
| `yggdrasil_accounts` | 0 | 1 | `user_id` | `bigint` | NO | `` |
| `yggdrasil_accounts` | 0 | 2 | `account_uuid` | `uuid` | NO | `` |
| `yggdrasil_accounts` | 0 | 3 | `launcher_password_hash` | `text` | NO | `''::text` |
| `yggdrasil_accounts` | 0 | 4 | `enabled` | `boolean` | NO | `false` |
| `yggdrasil_accounts` | 0 | 5 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `yggdrasil_accounts` | 0 | 6 | `updated_at` | `timestamp with time zone` | NO | `now()` |
| `yggdrasil_join_sessions` | 0 | 1 | `server_id` | `text` | NO | `` |
| `yggdrasil_join_sessions` | 0 | 2 | `token_id` | `bigint` | NO | `` |
| `yggdrasil_join_sessions` | 0 | 3 | `player_profile_id` | `bigint` | NO | `` |
| `yggdrasil_join_sessions` | 0 | 4 | `client_ip` | `text` | NO | `''::text` |
| `yggdrasil_join_sessions` | 0 | 5 | `expires_at` | `timestamp with time zone` | NO | `` |
| `yggdrasil_join_sessions` | 0 | 6 | `created_at` | `timestamp with time zone` | NO | `now()` |
| `yggdrasil_tokens` | 0 | 1 | `id` | `bigint` | NO | `nextval('yggdrasil_tokens_id_seq'::regclass)` |
| `yggdrasil_tokens` | 0 | 2 | `public_id` | `text` | NO | `new_public_id()` |
| `yggdrasil_tokens` | 0 | 3 | `access_token_hash` | `text` | NO | `` |
| `yggdrasil_tokens` | 0 | 4 | `user_id` | `bigint` | NO | `` |
| `yggdrasil_tokens` | 0 | 5 | `player_profile_id` | `bigint` | YES | `` |
| `yggdrasil_tokens` | 0 | 6 | `client_token` | `text` | NO | `` |
| `yggdrasil_tokens` | 0 | 7 | `status` | `text` | NO | `'active'::text` |
| `yggdrasil_tokens` | 0 | 8 | `issued_at` | `timestamp with time zone` | NO | `now()` |
| `yggdrasil_tokens` | 0 | 9 | `expires_at` | `timestamp with time zone` | NO | `` |
| `yggdrasil_tokens` | 0 | 10 | `last_used_at` | `timestamp with time zone` | YES | `` |
| `yggdrasil_tokens` | 0 | 11 | `revoked_at` | `timestamp with time zone` | YES | `` |
| `yggdrasil_tokens` | 0 | 12 | `replaced_by_id` | `bigint` | YES | `` |
| `yggdrasil_tokens` | 0 | 13 | `ip` | `text` | NO | `''::text` |
| `yggdrasil_tokens` | 0 | 14 | `user_agent` | `text` | NO | `''::text` |

## 约束

| 表 | 名称 | 定义 |
| --- | --- | --- |
| `activity_actions` | `activity_actions_code_key` | `UNIQUE (code)` |
| `activity_actions` | `activity_actions_code_not_null` | `NOT NULL code` |
| `activity_actions` | `activity_actions_id_not_null` | `NOT NULL id` |
| `activity_actions` | `activity_actions_name_not_null` | `NOT NULL name` |
| `activity_actions` | `activity_actions_pkey` | `PRIMARY KEY (id)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_confirmation_hash_not_null` | `NOT NULL confirmation_hash` |
| `activity_cleanup_runs` | `activity_cleanup_runs_deleted_count_check` | `CHECK (deleted_count >= 0)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_deleted_count_not_null` | `NOT NULL deleted_count` |
| `activity_cleanup_runs` | `activity_cleanup_runs_error_message_not_null` | `NOT NULL error_message` |
| `activity_cleanup_runs` | `activity_cleanup_runs_filters_not_null` | `NOT NULL filters` |
| `activity_cleanup_runs` | `activity_cleanup_runs_id_not_null` | `NOT NULL id` |
| `activity_cleanup_runs` | `activity_cleanup_runs_initiated_by_fkey` | `FOREIGN KEY (initiated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `activity_cleanup_runs` | `activity_cleanup_runs_matched_count_check` | `CHECK (matched_count >= 0)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_matched_count_not_null` | `NOT NULL matched_count` |
| `activity_cleanup_runs` | `activity_cleanup_runs_pkey` | `PRIMARY KEY (id)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_public_id_key` | `UNIQUE (public_id)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_public_id_not_null` | `NOT NULL public_id` |
| `activity_cleanup_runs` | `activity_cleanup_runs_source_check` | `CHECK (source = ANY (ARRAY['automatic'::text, 'manual'::text]))` |
| `activity_cleanup_runs` | `activity_cleanup_runs_source_not_null` | `NOT NULL source` |
| `activity_cleanup_runs` | `activity_cleanup_runs_started_at_not_null` | `NOT NULL started_at` |
| `activity_cleanup_runs` | `activity_cleanup_runs_status_check` | `CHECK (status = ANY (ARRAY['preview'::text, 'running'::text, 'completed'::text, 'failed'::text]))` |
| `activity_cleanup_runs` | `activity_cleanup_runs_status_not_null` | `NOT NULL status` |
| `activity_event_outbox` | `activity_event_outbox_action_id_fkey` | `FOREIGN KEY (action_id) REFERENCES activity_actions(id) ON DELETE RESTRICT` |
| `activity_event_outbox` | `activity_event_outbox_action_id_not_null` | `NOT NULL action_id` |
| `activity_event_outbox` | `activity_event_outbox_attempts_check` | `CHECK (attempts >= 0)` |
| `activity_event_outbox` | `activity_event_outbox_attempts_not_null` | `NOT NULL attempts` |
| `activity_event_outbox` | `activity_event_outbox_available_at_not_null` | `NOT NULL available_at` |
| `activity_event_outbox` | `activity_event_outbox_created_at_not_null` | `NOT NULL created_at` |
| `activity_event_outbox` | `activity_event_outbox_id_not_null` | `NOT NULL id` |
| `activity_event_outbox` | `activity_event_outbox_last_error_not_null` | `NOT NULL last_error` |
| `activity_event_outbox` | `activity_event_outbox_markdown_added_bytes_check` | `CHECK (markdown_added_bytes >= 0)` |
| `activity_event_outbox` | `activity_event_outbox_markdown_added_bytes_not_null` | `NOT NULL markdown_added_bytes` |
| `activity_event_outbox` | `activity_event_outbox_markdown_deleted_bytes_check` | `CHECK (markdown_deleted_bytes >= 0)` |
| `activity_event_outbox` | `activity_event_outbox_markdown_deleted_bytes_not_null` | `NOT NULL markdown_deleted_bytes` |
| `activity_event_outbox` | `activity_event_outbox_object_entity_type_check` | `CHECK (length(object_entity_type) <= 64)` |
| `activity_event_outbox` | `activity_event_outbox_object_entity_type_not_null` | `NOT NULL object_entity_type` |
| `activity_event_outbox` | `activity_event_outbox_object_internal_id_check` | `CHECK (object_internal_id IS NULL OR object_internal_id > 0)` |
| `activity_event_outbox` | `activity_event_outbox_object_public_id_check` | `CHECK (length(object_public_id) <= 128)` |
| `activity_event_outbox` | `activity_event_outbox_object_public_id_not_null` | `NOT NULL object_public_id` |
| `activity_event_outbox` | `activity_event_outbox_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE SET NULL` |
| `activity_event_outbox` | `activity_event_outbox_object_type_id_fkey` | `FOREIGN KEY (object_type_id) REFERENCES activity_object_types(id) ON DELETE RESTRICT` |
| `activity_event_outbox` | `activity_event_outbox_object_type_id_not_null` | `NOT NULL object_type_id` |
| `activity_event_outbox` | `activity_event_outbox_occurred_at_not_null` | `NOT NULL occurred_at` |
| `activity_event_outbox` | `activity_event_outbox_pkey` | `PRIMARY KEY (id)` |
| `activity_event_outbox` | `activity_event_outbox_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `activity_event_outbox` | `activity_event_outbox_user_id_not_null` | `NOT NULL user_id` |
| `activity_object_types` | `activity_object_types_code_key` | `UNIQUE (code)` |
| `activity_object_types` | `activity_object_types_code_not_null` | `NOT NULL code` |
| `activity_object_types` | `activity_object_types_id_not_null` | `NOT NULL id` |
| `activity_object_types` | `activity_object_types_name_not_null` | `NOT NULL name` |
| `activity_object_types` | `activity_object_types_pkey` | `PRIMARY KEY (id)` |
| `ai_task_logs` | `ai_task_logs_created_at_not_null` | `NOT NULL created_at` |
| `ai_task_logs` | `ai_task_logs_event_not_null` | `NOT NULL event` |
| `ai_task_logs` | `ai_task_logs_id_not_null` | `NOT NULL id` |
| `ai_task_logs` | `ai_task_logs_level_not_null` | `NOT NULL level` |
| `ai_task_logs` | `ai_task_logs_message_not_null` | `NOT NULL message` |
| `ai_task_logs` | `ai_task_logs_payload_not_null` | `NOT NULL payload` |
| `ai_task_logs` | `ai_task_logs_pkey` | `PRIMARY KEY (id)` |
| `ai_task_logs` | `ai_task_logs_task_id_fkey` | `FOREIGN KEY (task_id) REFERENCES ai_tasks(id) ON DELETE CASCADE` |
| `ai_tasks` | `ai_tasks_concurrency_key_not_null` | `NOT NULL concurrency_key` |
| `ai_tasks` | `ai_tasks_cost_micros_not_null` | `NOT NULL cost_micros` |
| `ai_tasks` | `ai_tasks_created_at_not_null` | `NOT NULL created_at` |
| `ai_tasks` | `ai_tasks_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `ai_tasks` | `ai_tasks_error_not_null` | `NOT NULL error` |
| `ai_tasks` | `ai_tasks_id_not_null` | `NOT NULL id` |
| `ai_tasks` | `ai_tasks_input_tokens_not_null` | `NOT NULL input_tokens` |
| `ai_tasks` | `ai_tasks_model_not_null` | `NOT NULL model` |
| `ai_tasks` | `ai_tasks_output_tokens_not_null` | `NOT NULL output_tokens` |
| `ai_tasks` | `ai_tasks_payload_not_null` | `NOT NULL payload` |
| `ai_tasks` | `ai_tasks_pkey` | `PRIMARY KEY (id)` |
| `ai_tasks` | `ai_tasks_priority_not_null` | `NOT NULL priority` |
| `ai_tasks` | `ai_tasks_provider_not_null` | `NOT NULL provider` |
| `ai_tasks` | `ai_tasks_quota_reserved_tokens_not_null` | `NOT NULL quota_reserved_tokens` |
| `ai_tasks` | `ai_tasks_result_not_null` | `NOT NULL result` |
| `ai_tasks` | `ai_tasks_status_not_null` | `NOT NULL status` |
| `ai_tasks` | `ai_tasks_task_type_not_null` | `NOT NULL task_type` |
| `ai_tasks` | `ai_tasks_task_uid_key` | `UNIQUE (task_uid)` |
| `ai_tasks` | `ai_tasks_task_uid_not_null` | `NOT NULL task_uid` |
| `ai_tasks` | `ai_tasks_updated_at_not_null` | `NOT NULL updated_at` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_created_at_not_null` | `NOT NULL created_at` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_enabled_not_null` | `NOT NULL enabled` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_id_not_null` | `NOT NULL id` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_kind_check` | `CHECK (kind = ANY (ARRAY['allowed_bot'::text, 'monitoring_bot'::text, 'blocked_bot'::text, 'ip_allow'::text, 'ip_block'::text]))` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_kind_not_null` | `NOT NULL kind` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_label_not_null` | `NOT NULL label` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_matcher_not_null` | `NOT NULL matcher` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_pkey` | `PRIMARY KEY (id)` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_public_id_key` | `UNIQUE (public_id)` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_public_id_not_null` | `NOT NULL public_id` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_read_only_not_null` | `NOT NULL read_only` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_secret_hash_not_null` | `NOT NULL secret_hash` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_updated_at_not_null` | `NOT NULL updated_at` |
| `anti_abuse_challenges` | `anti_abuse_challenges_action_not_null` | `NOT NULL action` |
| `anti_abuse_challenges` | `anti_abuse_challenges_answer_hash_not_null` | `NOT NULL answer_hash` |
| `anti_abuse_challenges` | `anti_abuse_challenges_check` | `CHECK (expires_at > issued_at)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_expires_at_not_null` | `NOT NULL expires_at` |
| `anti_abuse_challenges` | `anti_abuse_challenges_failure_count_check` | `CHECK (failure_count >= 0)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_failure_count_not_null` | `NOT NULL failure_count` |
| `anti_abuse_challenges` | `anti_abuse_challenges_id_not_null` | `NOT NULL id` |
| `anti_abuse_challenges` | `anti_abuse_challenges_ip_hash_not_null` | `NOT NULL ip_hash` |
| `anti_abuse_challenges` | `anti_abuse_challenges_issued_at_not_null` | `NOT NULL issued_at` |
| `anti_abuse_challenges` | `anti_abuse_challenges_kind_check` | `CHECK (kind = ANY (ARRAY['form'::text, 'human'::text]))` |
| `anti_abuse_challenges` | `anti_abuse_challenges_kind_not_null` | `NOT NULL kind` |
| `anti_abuse_challenges` | `anti_abuse_challenges_metadata_not_null` | `NOT NULL metadata` |
| `anti_abuse_challenges` | `anti_abuse_challenges_object_key_not_null` | `NOT NULL object_key` |
| `anti_abuse_challenges` | `anti_abuse_challenges_pkey` | `PRIMARY KEY (id)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_provider_not_null` | `NOT NULL provider` |
| `anti_abuse_challenges` | `anti_abuse_challenges_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_public_id_key` | `UNIQUE (public_id)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_public_id_not_null` | `NOT NULL public_id` |
| `anti_abuse_challenges` | `anti_abuse_challenges_session_hash_not_null` | `NOT NULL session_hash` |
| `anti_abuse_challenges` | `anti_abuse_challenges_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'passed'::text, 'failed'::text, 'consumed'::text, 'expired'::text]))` |
| `anti_abuse_challenges` | `anti_abuse_challenges_status_not_null` | `NOT NULL status` |
| `anti_abuse_challenges` | `anti_abuse_challenges_token_hash_key` | `UNIQUE (token_hash)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_token_hash_not_null` | `NOT NULL token_hash` |
| `anti_abuse_challenges` | `anti_abuse_challenges_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `anti_abuse_challenges` | `anti_abuse_challenges_user_id_not_null` | `NOT NULL user_id` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_action_not_null` | `NOT NULL action` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_created_at_not_null` | `NOT NULL created_at` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_event_id_fkey` | `FOREIGN KEY (event_id) REFERENCES anti_abuse_events(id) ON DELETE SET NULL` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_exact_hash_not_null` | `NOT NULL exact_hash` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_id_not_null` | `NOT NULL id` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_ip_hash_not_null` | `NOT NULL ip_hash` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_object_key_not_null` | `NOT NULL object_key` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_pkey` | `PRIMARY KEY (id)` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_simhash_not_null` | `NOT NULL simhash` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_action_not_null` | `NOT NULL action` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_crawler_class_not_null` | `NOT NULL crawler_class` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_event_count_check` | `CHECK (event_count >= 0)` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_event_count_not_null` | `NOT NULL event_count` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_outcome_not_null` | `NOT NULL outcome` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_pkey` | `PRIMARY KEY (stat_date, action, outcome, crawler_class)` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_stat_date_not_null` | `NOT NULL stat_date` |
| `anti_abuse_events` | `anti_abuse_events_action_not_null` | `NOT NULL action` |
| `anti_abuse_events` | `anti_abuse_events_content_hash_not_null` | `NOT NULL content_hash` |
| `anti_abuse_events` | `anti_abuse_events_crawler_class_not_null` | `NOT NULL crawler_class` |
| `anti_abuse_events` | `anti_abuse_events_created_at_not_null` | `NOT NULL created_at` |
| `anti_abuse_events` | `anti_abuse_events_device_hash_not_null` | `NOT NULL device_hash` |
| `anti_abuse_events` | `anti_abuse_events_disposition_check` | `CHECK (disposition = ANY (ARRAY['unreviewed'::text, 'acknowledged'::text, 'false_positive'::text, 'confirmed_malicious'::text]))` |
| `anti_abuse_events` | `anti_abuse_events_disposition_not_null` | `NOT NULL disposition` |
| `anti_abuse_events` | `anti_abuse_events_id_not_null` | `NOT NULL id` |
| `anti_abuse_events` | `anti_abuse_events_ip_hash_not_null` | `NOT NULL ip_hash` |
| `anti_abuse_events` | `anti_abuse_events_metadata_not_null` | `NOT NULL metadata` |
| `anti_abuse_events` | `anti_abuse_events_object_key_not_null` | `NOT NULL object_key` |
| `anti_abuse_events` | `anti_abuse_events_object_type_not_null` | `NOT NULL object_type` |
| `anti_abuse_events` | `anti_abuse_events_outcome_check` | `CHECK (outcome = ANY (ARRAY['allow'::text, 'allow_with_log'::text, 'challenge'::text, 'delay'::text, 'moderation'::text, 'temp_block'::text, 'deny'::text, 'account_review'::text]))` |
| `anti_abuse_events` | `anti_abuse_events_outcome_not_null` | `NOT NULL outcome` |
| `anti_abuse_events` | `anti_abuse_events_pkey` | `PRIMARY KEY (id)` |
| `anti_abuse_events` | `anti_abuse_events_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `anti_abuse_events` | `anti_abuse_events_public_id_key` | `UNIQUE (public_id)` |
| `anti_abuse_events` | `anti_abuse_events_public_id_not_null` | `NOT NULL public_id` |
| `anti_abuse_events` | `anti_abuse_events_request_id_not_null` | `NOT NULL request_id` |
| `anti_abuse_events` | `anti_abuse_events_review_note_not_null` | `NOT NULL review_note` |
| `anti_abuse_events` | `anti_abuse_events_reviewed_by_fkey` | `FOREIGN KEY (reviewed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_events` | `anti_abuse_events_risk_score_check` | `CHECK (risk_score >= 0 AND risk_score <= 1000)` |
| `anti_abuse_events` | `anti_abuse_events_risk_score_not_null` | `NOT NULL risk_score` |
| `anti_abuse_events` | `anti_abuse_events_rule_codes_not_null` | `NOT NULL rule_codes` |
| `anti_abuse_events` | `anti_abuse_events_session_hash_not_null` | `NOT NULL session_hash` |
| `anti_abuse_events` | `anti_abuse_events_similar_event_id_fkey` | `FOREIGN KEY (similar_event_id) REFERENCES anti_abuse_events(id) ON DELETE SET NULL` |
| `anti_abuse_events` | `anti_abuse_events_similarity_check` | `CHECK (similarity >= 0 AND similarity <= 1000)` |
| `anti_abuse_events` | `anti_abuse_events_similarity_not_null` | `NOT NULL similarity` |
| `anti_abuse_events` | `anti_abuse_events_subnet_hash_not_null` | `NOT NULL subnet_hash` |
| `anti_abuse_events` | `anti_abuse_events_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_actions_not_null` | `NOT NULL actions` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_appeal_allowed_not_null` | `NOT NULL appeal_allowed` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_automatic_not_null` | `NOT NULL automatic` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_check` | `CHECK (user_id IS NOT NULL OR ip_hash <> ''::text OR device_hash <> ''::text)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_check1` | `CHECK (ends_at IS NULL OR ends_at > starts_at)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_created_at_not_null` | `NOT NULL created_at` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_device_hash_not_null` | `NOT NULL device_hash` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_id_not_null` | `NOT NULL id` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_ip_hash_not_null` | `NOT NULL ip_hash` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_lift_reason_not_null` | `NOT NULL lift_reason` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_lifted_by_fkey` | `FOREIGN KEY (lifted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_mode_check` | `CHECK (mode = ANY (ARRAY['cooldown'::text, 'challenge'::text, 'moderation'::text, 'no_comment'::text, 'no_review'::text, 'no_upload'::text, 'read_only'::text, 'temporary_ban'::text, 'permanent_ban'::text]))` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_mode_not_null` | `NOT NULL mode` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_pkey` | `PRIMARY KEY (id)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_public_id_key` | `UNIQUE (public_id)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_public_id_not_null` | `NOT NULL public_id` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_reason_not_null` | `NOT NULL reason` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_risk_score_check` | `CHECK (risk_score >= 0 AND risk_score <= 1000)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_risk_score_not_null` | `NOT NULL risk_score` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_rule_code_not_null` | `NOT NULL rule_code` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_source_check` | `CHECK (source = ANY (ARRAY['automatic'::text, 'administrator'::text, 'security_import'::text]))` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_source_not_null` | `NOT NULL source` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_starts_at_not_null` | `NOT NULL starts_at` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `anti_abuse_user_states` | `anti_abuse_user_states_hit_count_check` | `CHECK (hit_count >= 0)` |
| `anti_abuse_user_states` | `anti_abuse_user_states_hit_count_not_null` | `NOT NULL hit_count` |
| `anti_abuse_user_states` | `anti_abuse_user_states_manually_trusted_not_null` | `NOT NULL manually_trusted` |
| `anti_abuse_user_states` | `anti_abuse_user_states_pkey` | `PRIMARY KEY (user_id)` |
| `anti_abuse_user_states` | `anti_abuse_user_states_risk_score_check` | `CHECK (risk_score >= 0 AND risk_score <= 1000)` |
| `anti_abuse_user_states` | `anti_abuse_user_states_risk_score_not_null` | `NOT NULL risk_score` |
| `anti_abuse_user_states` | `anti_abuse_user_states_trust_level_check` | `CHECK (trust_level = ANY (ARRAY['new'::text, 'normal'::text, 'trusted'::text, 'high_risk'::text, 'restricted'::text]))` |
| `anti_abuse_user_states` | `anti_abuse_user_states_trust_level_not_null` | `NOT NULL trust_level` |
| `anti_abuse_user_states` | `anti_abuse_user_states_updated_at_not_null` | `NOT NULL updated_at` |
| `anti_abuse_user_states` | `anti_abuse_user_states_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `anti_abuse_user_states` | `anti_abuse_user_states_user_id_not_null` | `NOT NULL user_id` |
| `app_logs` | `app_logs_action_not_null` | `NOT NULL action` |
| `app_logs` | `app_logs_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL` |
| `app_logs` | `app_logs_category_not_null` | `NOT NULL category` |
| `app_logs` | `app_logs_created_at_not_null` | `NOT NULL created_at` |
| `app_logs` | `app_logs_id_not_null` | `NOT NULL id` |
| `app_logs` | `app_logs_ip_not_null` | `NOT NULL ip` |
| `app_logs` | `app_logs_latency_ms_not_null` | `NOT NULL latency_ms` |
| `app_logs` | `app_logs_level_not_null` | `NOT NULL level` |
| `app_logs` | `app_logs_method_not_null` | `NOT NULL method` |
| `app_logs` | `app_logs_path_not_null` | `NOT NULL path` |
| `app_logs` | `app_logs_payload_not_null` | `NOT NULL payload` |
| `app_logs` | `app_logs_pkey` | `PRIMARY KEY (id)` |
| `app_logs` | `app_logs_status_not_null` | `NOT NULL status` |
| `app_logs` | `app_logs_target_not_null` | `NOT NULL target` |
| `app_logs` | `app_logs_user_agent_not_null` | `NOT NULL user_agent` |
| `audit_events` | `audit_events_action_not_null` | `NOT NULL action` |
| `audit_events` | `audit_events_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL` |
| `audit_events` | `audit_events_actor_snapshot_not_null` | `NOT NULL actor_snapshot` |
| `audit_events` | `audit_events_after_hash_not_null` | `NOT NULL after_hash` |
| `audit_events` | `audit_events_aggregate_key_not_null` | `NOT NULL aggregate_key` |
| `audit_events` | `audit_events_aggregate_type_not_null` | `NOT NULL aggregate_type` |
| `audit_events` | `audit_events_before_hash_not_null` | `NOT NULL before_hash` |
| `audit_events` | `audit_events_check` | `CHECK ((entity_type IS NULL) = (entity_id IS NULL))` |
| `audit_events` | `audit_events_created_at_not_null` | `NOT NULL created_at` |
| `audit_events` | `audit_events_entity_type_entity_id_fkey` | `FOREIGN KEY (entity_type, entity_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE RESTRICT` |
| `audit_events` | `audit_events_id_not_null` | `NOT NULL id` |
| `audit_events` | `audit_events_ip_not_null` | `NOT NULL ip` |
| `audit_events` | `audit_events_metadata_not_null` | `NOT NULL metadata` |
| `audit_events` | `audit_events_pkey` | `PRIMARY KEY (id)` |
| `audit_events` | `audit_events_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `audit_events` | `audit_events_public_id_key` | `UNIQUE (public_id)` |
| `audit_events` | `audit_events_public_id_not_null` | `NOT NULL public_id` |
| `audit_events` | `audit_events_trace_id_not_null` | `NOT NULL trace_id` |
| `audit_events` | `audit_events_user_agent_not_null` | `NOT NULL user_agent` |
| `auth_sessions` | `auth_sessions_auth_version_check` | `CHECK (auth_version > 0)` |
| `auth_sessions` | `auth_sessions_auth_version_not_null` | `NOT NULL auth_version` |
| `auth_sessions` | `auth_sessions_created_at_not_null` | `NOT NULL created_at` |
| `auth_sessions` | `auth_sessions_expires_at_not_null` | `NOT NULL expires_at` |
| `auth_sessions` | `auth_sessions_id_not_null` | `NOT NULL id` |
| `auth_sessions` | `auth_sessions_pkey` | `PRIMARY KEY (id)` |
| `auth_sessions` | `auth_sessions_session_hash_check` | `CHECK (octet_length(session_hash) = 32)` |
| `auth_sessions` | `auth_sessions_session_hash_key` | `UNIQUE (session_hash)` |
| `auth_sessions` | `auth_sessions_session_hash_not_null` | `NOT NULL session_hash` |
| `auth_sessions` | `auth_sessions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `auth_sessions` | `auth_sessions_user_id_not_null` | `NOT NULL user_id` |
| `ban_reasons` | `ban_reasons_active_not_null` | `NOT NULL active` |
| `ban_reasons` | `ban_reasons_code_not_null` | `NOT NULL code` |
| `ban_reasons` | `ban_reasons_pkey` | `PRIMARY KEY (code)` |
| `ban_reasons` | `ban_reasons_sort_order_not_null` | `NOT NULL sort_order` |
| `ban_reasons` | `ban_reasons_translations_not_null` | `NOT NULL translations` |
| `ban_records` | `ban_records_avatar_snapshot_not_null` | `NOT NULL avatar_snapshot` |
| `ban_records` | `ban_records_check` | `CHECK (reason_code <> 'other'::text OR btrim(custom_reason) <> ''::text)` |
| `ban_records` | `ban_records_created_at_not_null` | `NOT NULL created_at` |
| `ban_records` | `ban_records_custom_reason_not_null` | `NOT NULL custom_reason` |
| `ban_records` | `ban_records_id_not_null` | `NOT NULL id` |
| `ban_records` | `ban_records_internal_note_not_null` | `NOT NULL internal_note` |
| `ban_records` | `ban_records_moderator_id_fkey` | `FOREIGN KEY (moderator_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `ban_records` | `ban_records_moderator_id_not_null` | `NOT NULL moderator_id` |
| `ban_records` | `ban_records_pkey` | `PRIMARY KEY (id)` |
| `ban_records` | `ban_records_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `ban_records` | `ban_records_public_id_key` | `UNIQUE (public_id)` |
| `ban_records` | `ban_records_public_id_not_null` | `NOT NULL public_id` |
| `ban_records` | `ban_records_public_record_markdown_not_null` | `NOT NULL public_record_markdown` |
| `ban_records` | `ban_records_reason_code_fkey` | `FOREIGN KEY (reason_code) REFERENCES ban_reasons(code)` |
| `ban_records` | `ban_records_reason_code_not_null` | `NOT NULL reason_code` |
| `ban_records` | `ban_records_report_id_fkey` | `FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE SET NULL` |
| `ban_records` | `ban_records_revoke_reason_not_null` | `NOT NULL revoke_reason` |
| `ban_records` | `ban_records_revoked_by_fkey` | `FOREIGN KEY (revoked_by) REFERENCES users(id) ON DELETE SET NULL` |
| `ban_records` | `ban_records_starts_at_not_null` | `NOT NULL starts_at` |
| `ban_records` | `ban_records_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'revoked'::text, 'expired'::text]))` |
| `ban_records` | `ban_records_status_not_null` | `NOT NULL status` |
| `ban_records` | `ban_records_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `ban_records` | `ban_records_user_id_not_null` | `NOT NULL user_id` |
| `ban_records` | `ban_records_username_snapshot_not_null` | `NOT NULL username_snapshot` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_block_entity_type_id_not_null` | `NOT NULL block_entity_type_id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_block_id_not_null` | `NOT NULL block_id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_block_resource_id_fkey` | `FOREIGN KEY (block_resource_id) REFERENCES game_resources(entity_id) ON DELETE CASCADE` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_block_resource_id_not_null` | `NOT NULL block_resource_id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_data_not_null` | `NOT NULL data` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_id_not_null` | `NOT NULL id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_model_available_not_null` | `NOT NULL model_available` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_model_source_not_null` | `NOT NULL model_source` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_pkey` | `PRIMARY KEY (id)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_resource_snapshot_id_fkey` | `FOREIGN KEY (resource_snapshot_id) REFERENCES resource_import_snapshots(id) ON DELETE CASCADE` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_resource_snapshot_id_key` | `UNIQUE (resource_snapshot_id)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_resource_snapshot_id_not_null` | `NOT NULL resource_snapshot_id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_revision_id_block_id_key` | `UNIQUE (revision_id, block_id)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_variant_count_not_null` | `NOT NULL variant_count` |
| `block_entity_model_variants` | `block_entity_model_variants_coordinate_space_not_null` | `NOT NULL coordinate_space` |
| `block_entity_model_variants` | `block_entity_model_variants_data_not_null` | `NOT NULL data` |
| `block_entity_model_variants` | `block_entity_model_variants_id_not_null` | `NOT NULL id` |
| `block_entity_model_variants` | `block_entity_model_variants_mesh_data_not_null` | `NOT NULL mesh_data` |
| `block_entity_model_variants` | `block_entity_model_variants_mesh_path_not_null` | `NOT NULL mesh_path` |
| `block_entity_model_variants` | `block_entity_model_variants_model_snapshot_id_fkey` | `FOREIGN KEY (model_snapshot_id) REFERENCES block_entity_model_snapshots(id) ON DELETE CASCADE` |
| `block_entity_model_variants` | `block_entity_model_variants_model_snapshot_id_not_null` | `NOT NULL model_snapshot_id` |
| `block_entity_model_variants` | `block_entity_model_variants_model_snapshot_id_variant_id_key` | `UNIQUE (model_snapshot_id, variant_id)` |
| `block_entity_model_variants` | `block_entity_model_variants_obj_path_not_null` | `NOT NULL obj_path` |
| `block_entity_model_variants` | `block_entity_model_variants_pkey` | `PRIMARY KEY (id)` |
| `block_entity_model_variants` | `block_entity_model_variants_quad_count_not_null` | `NOT NULL quad_count` |
| `block_entity_model_variants` | `block_entity_model_variants_textures_not_null` | `NOT NULL textures` |
| `block_entity_model_variants` | `block_entity_model_variants_uv_complete_not_null` | `NOT NULL uv_complete` |
| `block_entity_model_variants` | `block_entity_model_variants_uv_origin_not_null` | `NOT NULL uv_origin` |
| `block_entity_model_variants` | `block_entity_model_variants_uv_space_not_null` | `NOT NULL uv_space` |
| `block_entity_model_variants` | `block_entity_model_variants_variant_id_not_null` | `NOT NULL variant_id` |
| `block_entity_model_variants` | `block_entity_model_variants_vertex_count_not_null` | `NOT NULL vertex_count` |
| `blueprint_jobs` | `blueprint_jobs_attempts_not_null` | `NOT NULL attempts` |
| `blueprint_jobs` | `blueprint_jobs_blueprint_id_fkey` | `FOREIGN KEY (blueprint_id) REFERENCES blueprints(id) ON DELETE CASCADE` |
| `blueprint_jobs` | `blueprint_jobs_blueprint_id_not_null` | `NOT NULL blueprint_id` |
| `blueprint_jobs` | `blueprint_jobs_created_at_not_null` | `NOT NULL created_at` |
| `blueprint_jobs` | `blueprint_jobs_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `blueprint_jobs` | `blueprint_jobs_id_not_null` | `NOT NULL id` |
| `blueprint_jobs` | `blueprint_jobs_last_error_not_null` | `NOT NULL last_error` |
| `blueprint_jobs` | `blueprint_jobs_operation_check` | `CHECK (operation = ANY (ARRAY['normalize'::text, 'convert'::text]))` |
| `blueprint_jobs` | `blueprint_jobs_operation_not_null` | `NOT NULL operation` |
| `blueprint_jobs` | `blueprint_jobs_pkey` | `PRIMARY KEY (id)` |
| `blueprint_jobs` | `blueprint_jobs_progress_check` | `CHECK (progress >= 0 AND progress <= 100)` |
| `blueprint_jobs` | `blueprint_jobs_progress_not_null` | `NOT NULL progress` |
| `blueprint_jobs` | `blueprint_jobs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `blueprint_jobs` | `blueprint_jobs_public_id_key` | `UNIQUE (public_id)` |
| `blueprint_jobs` | `blueprint_jobs_public_id_not_null` | `NOT NULL public_id` |
| `blueprint_jobs` | `blueprint_jobs_status_check` | `CHECK (status = ANY (ARRAY['queued'::text, 'processing'::text, 'completed'::text, 'failed'::text]))` |
| `blueprint_jobs` | `blueprint_jobs_status_not_null` | `NOT NULL status` |
| `blueprint_jobs` | `blueprint_jobs_target_format_not_null` | `NOT NULL target_format` |
| `blueprint_jobs` | `blueprint_jobs_updated_at_not_null` | `NOT NULL updated_at` |
| `blueprint_materials` | `blueprint_materials_block_count_not_null` | `NOT NULL block_count` |
| `blueprint_materials` | `blueprint_materials_block_id_not_null` | `NOT NULL block_id` |
| `blueprint_materials` | `blueprint_materials_block_state_not_null` | `NOT NULL block_state` |
| `blueprint_materials` | `blueprint_materials_blueprint_id_fkey` | `FOREIGN KEY (blueprint_id) REFERENCES blueprints(id) ON DELETE CASCADE` |
| `blueprint_materials` | `blueprint_materials_blueprint_id_not_null` | `NOT NULL blueprint_id` |
| `blueprint_materials` | `blueprint_materials_pkey` | `PRIMARY KEY (blueprint_id, block_state)` |
| `blueprint_materials` | `blueprint_materials_properties_check` | `CHECK (jsonb_typeof(properties) = 'object'::text)` |
| `blueprint_materials` | `blueprint_materials_properties_not_null` | `NOT NULL properties` |
| `blueprint_mods` | `blueprint_mods_blueprint_id_fkey` | `FOREIGN KEY (blueprint_id) REFERENCES blueprints(id) ON DELETE CASCADE` |
| `blueprint_mods` | `blueprint_mods_blueprint_id_not_null` | `NOT NULL blueprint_id` |
| `blueprint_mods` | `blueprint_mods_created_at_not_null` | `NOT NULL created_at` |
| `blueprint_mods` | `blueprint_mods_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `blueprint_mods` | `blueprint_mods_mod_id_not_null` | `NOT NULL mod_id` |
| `blueprint_mods` | `blueprint_mods_pkey` | `PRIMARY KEY (blueprint_id, source_namespace)` |
| `blueprint_mods` | `blueprint_mods_source_namespace_not_null` | `NOT NULL source_namespace` |
| `blueprint_variants` | `blueprint_variants_blueprint_id_fkey` | `FOREIGN KEY (blueprint_id) REFERENCES blueprints(id) ON DELETE CASCADE` |
| `blueprint_variants` | `blueprint_variants_blueprint_id_not_null` | `NOT NULL blueprint_id` |
| `blueprint_variants` | `blueprint_variants_blueprint_id_object_key_key` | `UNIQUE (blueprint_id, object_key)` |
| `blueprint_variants` | `blueprint_variants_content_type_not_null` | `NOT NULL content_type` |
| `blueprint_variants` | `blueprint_variants_created_at_not_null` | `NOT NULL created_at` |
| `blueprint_variants` | `blueprint_variants_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `blueprint_variants` | `blueprint_variants_file_id_fkey` | `FOREIGN KEY (file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `blueprint_variants` | `blueprint_variants_format_not_null` | `NOT NULL format` |
| `blueprint_variants` | `blueprint_variants_id_not_null` | `NOT NULL id` |
| `blueprint_variants` | `blueprint_variants_object_key_not_null` | `NOT NULL object_key` |
| `blueprint_variants` | `blueprint_variants_original_name_not_null` | `NOT NULL original_name` |
| `blueprint_variants` | `blueprint_variants_original_not_null` | `NOT NULL original` |
| `blueprint_variants` | `blueprint_variants_pkey` | `PRIMARY KEY (id)` |
| `blueprint_variants` | `blueprint_variants_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `blueprint_variants` | `blueprint_variants_public_id_key` | `UNIQUE (public_id)` |
| `blueprint_variants` | `blueprint_variants_public_id_not_null` | `NOT NULL public_id` |
| `blueprint_variants` | `blueprint_variants_recommended_not_null` | `NOT NULL recommended` |
| `blueprint_variants` | `blueprint_variants_sha256_not_null` | `NOT NULL sha256` |
| `blueprint_variants` | `blueprint_variants_size_bytes_not_null` | `NOT NULL size_bytes` |
| `blueprint_variants` | `blueprint_variants_status_check` | `CHECK (status = ANY (ARRAY['queued'::text, 'processing'::text, 'ready'::text, 'failed'::text]))` |
| `blueprint_variants` | `blueprint_variants_status_not_null` | `NOT NULL status` |
| `blueprints` | `blueprints_block_count_not_null` | `NOT NULL block_count` |
| `blueprints` | `blueprints_cover_file_id_fkey` | `FOREIGN KEY (cover_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `blueprints` | `blueprints_cover_object_key_not_null` | `NOT NULL cover_object_key` |
| `blueprints` | `blueprints_created_at_not_null` | `NOT NULL created_at` |
| `blueprints` | `blueprints_data_version_not_null` | `NOT NULL data_version` |
| `blueprints` | `blueprints_description_markdown_not_null` | `NOT NULL description_markdown` |
| `blueprints` | `blueprints_entity_count_not_null` | `NOT NULL entity_count` |
| `blueprints` | `blueprints_id_not_null` | `NOT NULL id` |
| `blueprints` | `blueprints_last_error_not_null` | `NOT NULL last_error` |
| `blueprints` | `blueprints_normalized_object_key_not_null` | `NOT NULL normalized_object_key` |
| `blueprints` | `blueprints_original_file_id_fkey` | `FOREIGN KEY (original_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `blueprints` | `blueprints_original_object_key_not_null` | `NOT NULL original_object_key` |
| `blueprints` | `blueprints_owner_id_fkey` | `FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE` |
| `blueprints` | `blueprints_owner_id_not_null` | `NOT NULL owner_id` |
| `blueprints` | `blueprints_palette_count_not_null` | `NOT NULL palette_count` |
| `blueprints` | `blueprints_pkey` | `PRIMARY KEY (id)` |
| `blueprints` | `blueprints_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `blueprints` | `blueprints_public_id_key` | `UNIQUE (public_id)` |
| `blueprints` | `blueprints_public_id_not_null` | `NOT NULL public_id` |
| `blueprints` | `blueprints_review_status_check` | `CHECK (review_status = ANY (ARRAY['not_required'::text, 'pending'::text, 'approved'::text, 'rejected'::text]))` |
| `blueprints` | `blueprints_review_status_not_null` | `NOT NULL review_status` |
| `blueprints` | `blueprints_size_x_not_null` | `NOT NULL size_x` |
| `blueprints` | `blueprints_size_y_not_null` | `NOT NULL size_y` |
| `blueprints` | `blueprints_size_z_not_null` | `NOT NULL size_z` |
| `blueprints` | `blueprints_source_format_not_null` | `NOT NULL source_format` |
| `blueprints` | `blueprints_status_check` | `CHECK (status = ANY (ARRAY['uploading'::text, 'queued'::text, 'processing'::text, 'ready'::text, 'partial'::text, 'failed'::text, 'deleted'::text]))` |
| `blueprints` | `blueprints_status_not_null` | `NOT NULL status` |
| `blueprints` | `blueprints_title_not_null` | `NOT NULL title` |
| `blueprints` | `blueprints_updated_at_not_null` | `NOT NULL updated_at` |
| `blueprints` | `fk_blueprints_published_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `catalog_entities` | `catalog_entities_created_at_not_null` | `NOT NULL created_at` |
| `catalog_entities` | `catalog_entities_default_locale_not_null` | `NOT NULL default_locale` |
| `catalog_entities` | `catalog_entities_entity_type_check` | `CHECK (entity_type = ANY (ARRAY['resource'::text, 'recipe'::text, 'recipe_type'::text, 'recipe_template'::text, 'tag'::text, 'structure'::text, 'document'::text]))` |
| `catalog_entities` | `catalog_entities_entity_type_not_null` | `NOT NULL entity_type` |
| `catalog_entities` | `catalog_entities_id_entity_type_key` | `UNIQUE (id, entity_type)` |
| `catalog_entities` | `catalog_entities_id_not_null` | `NOT NULL id` |
| `catalog_entities` | `catalog_entities_identity_key_key` | `UNIQUE (identity_key)` |
| `catalog_entities` | `catalog_entities_identity_key_not_null` | `NOT NULL identity_key` |
| `catalog_entities` | `catalog_entities_pkey` | `PRIMARY KEY (id)` |
| `catalog_entities` | `catalog_entities_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `catalog_entities` | `catalog_entities_public_id_key` | `UNIQUE (public_id)` |
| `catalog_entities` | `catalog_entities_public_id_not_null` | `NOT NULL public_id` |
| `catalog_entities` | `catalog_entities_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'placeholder'::text, 'archived'::text]))` |
| `catalog_entities` | `catalog_entities_status_not_null` | `NOT NULL status` |
| `catalog_entities` | `catalog_entities_updated_at_not_null` | `NOT NULL updated_at` |
| `catalog_entities` | `fk_catalog_entities_published_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_asset_kind_not_null` | `NOT NULL asset_kind` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_asset_path_not_null` | `NOT NULL asset_path` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_byte_length_not_null` | `NOT NULL byte_length` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_content_type_not_null` | `NOT NULL content_type` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_data_not_null` | `NOT NULL data` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_id_not_null` | `NOT NULL id` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_revision_id_asset_path_key` | `UNIQUE (revision_id, asset_path)` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_sha256_not_null` | `NOT NULL sha256` |
| `catalog_import_capabilities` | `catalog_import_capabilities_capability_id_not_null` | `NOT NULL capability_id` |
| `catalog_import_capabilities` | `catalog_import_capabilities_data_not_null` | `NOT NULL data` |
| `catalog_import_capabilities` | `catalog_import_capabilities_pkey` | `PRIMARY KEY (revision_id, capability_id)` |
| `catalog_import_capabilities` | `catalog_import_capabilities_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_capabilities` | `catalog_import_capabilities_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_capabilities` | `catalog_import_capabilities_source_not_null` | `NOT NULL source` |
| `catalog_import_capabilities` | `catalog_import_capabilities_status_not_null` | `NOT NULL status` |
| `catalog_import_job_logs` | `catalog_import_job_logs_created_at_not_null` | `NOT NULL created_at` |
| `catalog_import_job_logs` | `catalog_import_job_logs_id_not_null` | `NOT NULL id` |
| `catalog_import_job_logs` | `catalog_import_job_logs_job_id_fkey` | `FOREIGN KEY (job_id) REFERENCES catalog_import_jobs(id) ON DELETE CASCADE` |
| `catalog_import_job_logs` | `catalog_import_job_logs_job_id_not_null` | `NOT NULL job_id` |
| `catalog_import_job_logs` | `catalog_import_job_logs_level_not_null` | `NOT NULL level` |
| `catalog_import_job_logs` | `catalog_import_job_logs_message_not_null` | `NOT NULL message` |
| `catalog_import_job_logs` | `catalog_import_job_logs_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_job_logs` | `catalog_import_job_logs_stage_not_null` | `NOT NULL stage` |
| `catalog_import_jobs` | `catalog_import_jobs_attempt_count_not_null` | `NOT NULL attempt_count` |
| `catalog_import_jobs` | `catalog_import_jobs_configured_modids_not_null` | `NOT NULL configured_modids` |
| `catalog_import_jobs` | `catalog_import_jobs_created_at_not_null` | `NOT NULL created_at` |
| `catalog_import_jobs` | `catalog_import_jobs_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `catalog_import_jobs` | `catalog_import_jobs_current_stage_not_null` | `NOT NULL current_stage` |
| `catalog_import_jobs` | `catalog_import_jobs_detected_modids_not_null` | `NOT NULL detected_modids` |
| `catalog_import_jobs` | `catalog_import_jobs_error_code_not_null` | `NOT NULL error_code` |
| `catalog_import_jobs` | `catalog_import_jobs_error_detail_not_null` | `NOT NULL error_detail` |
| `catalog_import_jobs` | `catalog_import_jobs_id_not_null` | `NOT NULL id` |
| `catalog_import_jobs` | `catalog_import_jobs_importer_version_not_null` | `NOT NULL importer_version` |
| `catalog_import_jobs` | `catalog_import_jobs_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `catalog_import_jobs` | `catalog_import_jobs_mod_id_not_null` | `NOT NULL mod_id` |
| `catalog_import_jobs` | `catalog_import_jobs_mod_id_package_id_importer_version_targ_key` | `UNIQUE (mod_id, package_id, importer_version, target_version_id, overwrite_existing)` |
| `catalog_import_jobs` | `catalog_import_jobs_modid_analysis_hash_not_null` | `NOT NULL modid_analysis_hash` |
| `catalog_import_jobs` | `catalog_import_jobs_modid_confirmation_required_not_null` | `NOT NULL modid_confirmation_required` |
| `catalog_import_jobs` | `catalog_import_jobs_modid_confirmed_by_fkey` | `FOREIGN KEY (modid_confirmed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `catalog_import_jobs` | `catalog_import_jobs_overwrite_existing_not_null` | `NOT NULL overwrite_existing` |
| `catalog_import_jobs` | `catalog_import_jobs_package_id_fkey` | `FOREIGN KEY (package_id) REFERENCES catalog_import_packages(id) ON DELETE CASCADE` |
| `catalog_import_jobs` | `catalog_import_jobs_package_id_not_null` | `NOT NULL package_id` |
| `catalog_import_jobs` | `catalog_import_jobs_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_jobs` | `catalog_import_jobs_primary_detected_modid_not_null` | `NOT NULL primary_detected_modid` |
| `catalog_import_jobs` | `catalog_import_jobs_progress_check` | `CHECK (progress >= 0 AND progress <= 100)` |
| `catalog_import_jobs` | `catalog_import_jobs_progress_not_null` | `NOT NULL progress` |
| `catalog_import_jobs` | `catalog_import_jobs_run_token_not_null` | `NOT NULL run_token` |
| `catalog_import_jobs` | `catalog_import_jobs_status_check` | `CHECK (status = ANY (ARRAY['queued'::text, 'validating'::text, 'confirmation_required'::text, 'importing'::text, 'ready'::text, 'partial'::text, 'failed'::text, 'cancelled'::text]))` |
| `catalog_import_jobs` | `catalog_import_jobs_status_not_null` | `NOT NULL status` |
| `catalog_import_jobs` | `catalog_import_jobs_target_version_id_not_null` | `NOT NULL target_version_id` |
| `catalog_import_jobs` | `catalog_import_jobs_updated_at_not_null` | `NOT NULL updated_at` |
| `catalog_import_jobs` | `fk_catalog_import_jobs_target_version` | `FOREIGN KEY (target_version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `catalog_import_locales` | `catalog_import_locales_locale_not_null` | `NOT NULL locale` |
| `catalog_import_locales` | `catalog_import_locales_pkey` | `PRIMARY KEY (revision_id, locale)` |
| `catalog_import_locales` | `catalog_import_locales_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_locales` | `catalog_import_locales_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_locales` | `catalog_import_locales_translation_count_not_null` | `NOT NULL translation_count` |
| `catalog_import_media` | `catalog_import_media_asset_path_not_null` | `NOT NULL asset_path` |
| `catalog_import_media` | `catalog_import_media_byte_length_not_null` | `NOT NULL byte_length` |
| `catalog_import_media` | `catalog_import_media_content_type_not_null` | `NOT NULL content_type` |
| `catalog_import_media` | `catalog_import_media_media_kind_not_null` | `NOT NULL media_kind` |
| `catalog_import_media` | `catalog_import_media_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `catalog_import_media` | `catalog_import_media_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `catalog_import_media` | `catalog_import_media_pkey` | `PRIMARY KEY (revision_id, asset_path)` |
| `catalog_import_media` | `catalog_import_media_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_media` | `catalog_import_media_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_media` | `catalog_import_media_sha256_not_null` | `NOT NULL sha256` |
| `catalog_import_packages` | `catalog_import_packages_archive_file_id_fkey` | `FOREIGN KEY (archive_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `catalog_import_packages` | `catalog_import_packages_archive_name_not_null` | `NOT NULL archive_name` |
| `catalog_import_packages` | `catalog_import_packages_exporter_version_not_null` | `NOT NULL exporter_version` |
| `catalog_import_packages` | `catalog_import_packages_id_not_null` | `NOT NULL id` |
| `catalog_import_packages` | `catalog_import_packages_loader_not_null` | `NOT NULL loader` |
| `catalog_import_packages` | `catalog_import_packages_manifest_not_null` | `NOT NULL manifest` |
| `catalog_import_packages` | `catalog_import_packages_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `catalog_import_packages` | `catalog_import_packages_namespaces_not_null` | `NOT NULL namespaces` |
| `catalog_import_packages` | `catalog_import_packages_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_packages` | `catalog_import_packages_profile_not_null` | `NOT NULL profile` |
| `catalog_import_packages` | `catalog_import_packages_schema_version_not_null` | `NOT NULL schema_version` |
| `catalog_import_packages` | `catalog_import_packages_sha256_check` | `CHECK (sha256 ~ '^[0-9a-f]{64}$'::text)` |
| `catalog_import_packages` | `catalog_import_packages_sha256_key` | `UNIQUE (sha256)` |
| `catalog_import_packages` | `catalog_import_packages_sha256_not_null` | `NOT NULL sha256` |
| `catalog_import_packages` | `catalog_import_packages_uploaded_at_not_null` | `NOT NULL uploaded_at` |
| `catalog_import_packages` | `catalog_import_packages_uploaded_by_fkey` | `FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_advancement_count_not_null` | `NOT NULL advancement_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_asset_count_not_null` | `NOT NULL asset_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_capability_statuses_not_null` | `NOT NULL capability_statuses` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_document_counts_not_null` | `NOT NULL document_counts` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_key_mapping_count_not_null` | `NOT NULL key_mapping_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_pkey` | `PRIMARY KEY (revision_id)` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_recipe_count_not_null` | `NOT NULL recipe_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_registry_counts_not_null` | `NOT NULL registry_counts` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_structure_count_not_null` | `NOT NULL structure_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_tag_count_not_null` | `NOT NULL tag_count` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_updated_at_not_null` | `NOT NULL updated_at` |
| `catalog_import_revisions` | `catalog_import_revisions_created_at_not_null` | `NOT NULL created_at` |
| `catalog_import_revisions` | `catalog_import_revisions_exporter_version_not_null` | `NOT NULL exporter_version` |
| `catalog_import_revisions` | `catalog_import_revisions_id_not_null` | `NOT NULL id` |
| `catalog_import_revisions` | `catalog_import_revisions_import_run_token_not_null` | `NOT NULL import_run_token` |
| `catalog_import_revisions` | `catalog_import_revisions_is_active_not_null` | `NOT NULL is_active` |
| `catalog_import_revisions` | `catalog_import_revisions_job_id_fkey` | `FOREIGN KEY (job_id) REFERENCES catalog_import_jobs(id) ON DELETE CASCADE` |
| `catalog_import_revisions` | `catalog_import_revisions_job_id_not_null` | `NOT NULL job_id` |
| `catalog_import_revisions` | `catalog_import_revisions_loader_not_null` | `NOT NULL loader` |
| `catalog_import_revisions` | `catalog_import_revisions_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `catalog_import_revisions` | `catalog_import_revisions_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `catalog_import_revisions` | `catalog_import_revisions_mod_id_not_null` | `NOT NULL mod_id` |
| `catalog_import_revisions` | `catalog_import_revisions_mod_id_target_version_id_source_ki_key` | `UNIQUE (mod_id, target_version_id, source_kind, source_namespace, revision_no)` |
| `catalog_import_revisions` | `catalog_import_revisions_package_id_fkey` | `FOREIGN KEY (package_id) REFERENCES catalog_import_packages(id) ON DELETE RESTRICT` |
| `catalog_import_revisions` | `catalog_import_revisions_package_id_not_null` | `NOT NULL package_id` |
| `catalog_import_revisions` | `catalog_import_revisions_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_revisions` | `catalog_import_revisions_revision_no_not_null` | `NOT NULL revision_no` |
| `catalog_import_revisions` | `catalog_import_revisions_source_kind_not_null` | `NOT NULL source_kind` |
| `catalog_import_revisions` | `catalog_import_revisions_source_metadata_not_null` | `NOT NULL source_metadata` |
| `catalog_import_revisions` | `catalog_import_revisions_source_namespace_not_null` | `NOT NULL source_namespace` |
| `catalog_import_revisions` | `catalog_import_revisions_status_check` | `CHECK (status = ANY (ARRAY['staging'::text, 'ready'::text, 'partial'::text, 'rejected'::text, 'superseded'::text]))` |
| `catalog_import_revisions` | `catalog_import_revisions_status_not_null` | `NOT NULL status` |
| `catalog_import_revisions` | `catalog_import_revisions_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `catalog_import_revisions` | `catalog_import_revisions_submitted_by_snapshot_not_null` | `NOT NULL submitted_by_snapshot` |
| `catalog_import_revisions` | `catalog_import_revisions_target_version_id_not_null` | `NOT NULL target_version_id` |
| `catalog_import_revisions` | `fk_catalog_import_revisions_target_version` | `FOREIGN KEY (target_version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `catalog_import_structures` | `catalog_import_structures_asset_path_not_null` | `NOT NULL asset_path` |
| `catalog_import_structures` | `catalog_import_structures_id_not_null` | `NOT NULL id` |
| `catalog_import_structures` | `catalog_import_structures_pkey` | `PRIMARY KEY (id)` |
| `catalog_import_structures` | `catalog_import_structures_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_structures` | `catalog_import_structures_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_structures` | `catalog_import_structures_revision_id_structure_id_key` | `UNIQUE (revision_id, structure_id)` |
| `catalog_import_structures` | `catalog_import_structures_source_format_not_null` | `NOT NULL source_format` |
| `catalog_import_structures` | `catalog_import_structures_structure_id_not_null` | `NOT NULL structure_id` |
| `catalog_import_structures` | `catalog_import_structures_summary_not_null` | `NOT NULL summary` |
| `catalog_import_structures` | `catalog_import_structures_template_blob_id_fkey` | `FOREIGN KEY (template_blob_id) REFERENCES catalog_import_binary_assets(id) ON DELETE CASCADE` |
| `catalog_import_structures` | `catalog_import_structures_template_blob_id_not_null` | `NOT NULL template_blob_id` |
| `catalog_import_text_assets` | `catalog_import_text_assets_asset_kind_not_null` | `NOT NULL asset_kind` |
| `catalog_import_text_assets` | `catalog_import_text_assets_asset_path_not_null` | `NOT NULL asset_path` |
| `catalog_import_text_assets` | `catalog_import_text_assets_byte_length_not_null` | `NOT NULL byte_length` |
| `catalog_import_text_assets` | `catalog_import_text_assets_check` | `CHECK (((text_content IS NOT NULL)::integer + (json_content IS NOT NULL)::integer) = 1)` |
| `catalog_import_text_assets` | `catalog_import_text_assets_content_type_not_null` | `NOT NULL content_type` |
| `catalog_import_text_assets` | `catalog_import_text_assets_pkey` | `PRIMARY KEY (revision_id, asset_path)` |
| `catalog_import_text_assets` | `catalog_import_text_assets_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `catalog_import_text_assets` | `catalog_import_text_assets_revision_id_not_null` | `NOT NULL revision_id` |
| `catalog_import_text_assets` | `catalog_import_text_assets_sha256_not_null` | `NOT NULL sha256` |
| `catalog_resource_definitions` | `catalog_resource_definitions_created_at_not_null` | `NOT NULL created_at` |
| `catalog_resource_definitions` | `catalog_resource_definitions_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `catalog_resource_definitions` | `catalog_resource_definitions_definition_not_null` | `NOT NULL definition` |
| `catalog_resource_definitions` | `catalog_resource_definitions_definition_schema_version_check` | `CHECK (definition_schema_version >= 1)` |
| `catalog_resource_definitions` | `catalog_resource_definitions_definition_schema_version_not_null` | `NOT NULL definition_schema_version` |
| `catalog_resource_definitions` | `catalog_resource_definitions_icon_file_id_fkey` | `FOREIGN KEY (icon_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `catalog_resource_definitions` | `catalog_resource_definitions_pkey` | `PRIMARY KEY (resource_id)` |
| `catalog_resource_definitions` | `catalog_resource_definitions_render_file_id_fkey` | `FOREIGN KEY (render_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `catalog_resource_definitions` | `catalog_resource_definitions_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE CASCADE` |
| `catalog_resource_definitions` | `catalog_resource_definitions_resource_id_not_null` | `NOT NULL resource_id` |
| `catalog_resource_definitions` | `catalog_resource_definitions_updated_at_not_null` | `NOT NULL updated_at` |
| `catalog_resource_definitions` | `catalog_resource_definitions_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `catalog_resource_definitions` | `fk_catalog_resource_definitions_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `catalog_tag_members` | `catalog_tag_members_ordinal_check` | `CHECK (ordinal >= 0)` |
| `catalog_tag_members` | `catalog_tag_members_ordinal_not_null` | `NOT NULL ordinal` |
| `catalog_tag_members` | `catalog_tag_members_pkey` | `PRIMARY KEY (tag_id, resource_id)` |
| `catalog_tag_members` | `catalog_tag_members_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE RESTRICT` |
| `catalog_tag_members` | `catalog_tag_members_resource_id_not_null` | `NOT NULL resource_id` |
| `catalog_tag_members` | `catalog_tag_members_tag_id_fkey` | `FOREIGN KEY (tag_id) REFERENCES catalog_tags(entity_id) ON DELETE CASCADE` |
| `catalog_tag_members` | `catalog_tag_members_tag_id_not_null` | `NOT NULL tag_id` |
| `catalog_tag_members` | `catalog_tag_members_tag_id_ordinal_key` | `UNIQUE (tag_id, ordinal)` |
| `catalog_tag_members` | `fk_catalog_tag_members_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `catalog_tags` | `catalog_tags_canonical_id_not_null` | `NOT NULL canonical_id` |
| `catalog_tags` | `catalog_tags_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `catalog_tags` | `catalog_tags_entity_id_not_null` | `NOT NULL entity_id` |
| `catalog_tags` | `catalog_tags_pkey` | `PRIMARY KEY (entity_id)` |
| `catalog_tags` | `catalog_tags_registry_canonical_id_key` | `UNIQUE (registry, canonical_id)` |
| `catalog_tags` | `catalog_tags_registry_not_null` | `NOT NULL registry` |
| `change_requests` | `change_requests_aggregate_key_not_null` | `NOT NULL aggregate_key` |
| `change_requests` | `change_requests_aggregate_type_not_null` | `NOT NULL aggregate_type` |
| `change_requests` | `change_requests_base_revision_id_fkey` | `FOREIGN KEY (base_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `change_requests` | `change_requests_check` | `CHECK ((entity_type IS NULL) = (entity_id IS NULL))` |
| `change_requests` | `change_requests_entity_type_entity_id_fkey` | `FOREIGN KEY (entity_type, entity_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE RESTRICT` |
| `change_requests` | `change_requests_id_not_null` | `NOT NULL id` |
| `change_requests` | `change_requests_metadata_not_null` | `NOT NULL metadata` |
| `change_requests` | `change_requests_pkey` | `PRIMARY KEY (id)` |
| `change_requests` | `change_requests_proposed_revision_id_fkey` | `FOREIGN KEY (proposed_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `change_requests` | `change_requests_proposed_revision_id_key` | `UNIQUE (proposed_revision_id)` |
| `change_requests` | `change_requests_proposed_revision_id_not_null` | `NOT NULL proposed_revision_id` |
| `change_requests` | `change_requests_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `change_requests` | `change_requests_public_id_key` | `UNIQUE (public_id)` |
| `change_requests` | `change_requests_public_id_not_null` | `NOT NULL public_id` |
| `change_requests` | `change_requests_reason_not_null` | `NOT NULL reason` |
| `change_requests` | `change_requests_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'conflicted'::text, 'withdrawn'::text]))` |
| `change_requests` | `change_requests_status_not_null` | `NOT NULL status` |
| `change_requests` | `change_requests_submitted_at_not_null` | `NOT NULL submitted_at` |
| `change_requests` | `change_requests_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `change_requests` | `change_requests_submitted_by_snapshot_not_null` | `NOT NULL submitted_by_snapshot` |
| `comment_attachments` | `comment_attachments_attachment_file_id_fkey` | `FOREIGN KEY (attachment_file_id) REFERENCES oss_files(id) ON DELETE CASCADE` |
| `comment_attachments` | `comment_attachments_attachment_file_id_not_null` | `NOT NULL attachment_file_id` |
| `comment_attachments` | `comment_attachments_comment_id_fkey` | `FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_attachments` | `comment_attachments_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_attachments` | `comment_attachments_created_at_not_null` | `NOT NULL created_at` |
| `comment_attachments` | `comment_attachments_kind_check` | `CHECK (kind = ANY (ARRAY['file'::text, 'log'::text]))` |
| `comment_attachments` | `comment_attachments_kind_not_null` | `NOT NULL kind` |
| `comment_attachments` | `comment_attachments_pkey` | `PRIMARY KEY (comment_id, attachment_file_id)` |
| `comment_attachments` | `comment_attachments_processing_status_check` | `CHECK (processing_status = ANY (ARRAY['ready'::text, 'processing'::text, 'failed'::text]))` |
| `comment_attachments` | `comment_attachments_processing_status_not_null` | `NOT NULL processing_status` |
| `comment_closure` | `comment_closure_ancestor_id_fkey` | `FOREIGN KEY (ancestor_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_closure` | `comment_closure_ancestor_id_not_null` | `NOT NULL ancestor_id` |
| `comment_closure` | `comment_closure_depth_check` | `CHECK (depth >= 0 AND depth <= 256)` |
| `comment_closure` | `comment_closure_depth_not_null` | `NOT NULL depth` |
| `comment_closure` | `comment_closure_descendant_id_fkey` | `FOREIGN KEY (descendant_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_closure` | `comment_closure_descendant_id_not_null` | `NOT NULL descendant_id` |
| `comment_closure` | `comment_closure_pkey` | `PRIMARY KEY (ancestor_id, descendant_id)` |
| `comment_floor_counters` | `comment_floor_counters_last_floor_check` | `CHECK (last_floor >= 0)` |
| `comment_floor_counters` | `comment_floor_counters_last_floor_not_null` | `NOT NULL last_floor` |
| `comment_floor_counters` | `comment_floor_counters_pkey` | `PRIMARY KEY (target_type, target_id, target_version_key)` |
| `comment_floor_counters` | `comment_floor_counters_target_id_not_null` | `NOT NULL target_id` |
| `comment_floor_counters` | `comment_floor_counters_target_type_not_null` | `NOT NULL target_type` |
| `comment_floor_counters` | `comment_floor_counters_target_version_key_not_null` | `NOT NULL target_version_key` |
| `comment_floor_counters` | `comment_floor_counters_updated_at_not_null` | `NOT NULL updated_at` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_attempts_check` | `CHECK (attempts >= 0)` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_attempts_not_null` | `NOT NULL attempts` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_available_at_not_null` | `NOT NULL available_at` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_comment_id_fkey` | `FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_last_error_not_null` | `NOT NULL last_error` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_pkey` | `PRIMARY KEY (comment_id)` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_updated_at_not_null` | `NOT NULL updated_at` |
| `comment_log_bindings` | `comment_log_bindings_attachment_file_id_not_null` | `NOT NULL attachment_file_id` |
| `comment_log_bindings` | `comment_log_bindings_comment_id_attachment_file_id_fkey` | `FOREIGN KEY (comment_id, attachment_file_id) REFERENCES comment_attachments(comment_id, attachment_file_id) ON DELETE CASCADE` |
| `comment_log_bindings` | `comment_log_bindings_comment_id_log_share_id_key` | `UNIQUE (comment_id, log_share_id)` |
| `comment_log_bindings` | `comment_log_bindings_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_log_bindings` | `comment_log_bindings_created_at_not_null` | `NOT NULL created_at` |
| `comment_log_bindings` | `comment_log_bindings_log_share_id_fkey` | `FOREIGN KEY (log_share_id) REFERENCES log_shares(id) ON DELETE CASCADE` |
| `comment_log_bindings` | `comment_log_bindings_log_share_id_not_null` | `NOT NULL log_share_id` |
| `comment_log_bindings` | `comment_log_bindings_pkey` | `PRIMARY KEY (comment_id, attachment_file_id)` |
| `comment_reactions` | `comment_reactions_comment_id_fkey` | `FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_reactions` | `comment_reactions_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_reactions` | `comment_reactions_created_at_not_null` | `NOT NULL created_at` |
| `comment_reactions` | `comment_reactions_pkey` | `PRIMARY KEY (comment_id, user_id, reaction)` |
| `comment_reactions` | `comment_reactions_reaction_check` | `CHECK (reaction = ANY (ARRAY['thumbs_up'::text, 'thumbs_down'::text, 'laugh'::text, 'hooray'::text, 'confused'::text, 'heart'::text, 'rocket'::text, 'eyes'::text]))` |
| `comment_reactions` | `comment_reactions_reaction_not_null` | `NOT NULL reaction` |
| `comment_reactions` | `comment_reactions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `comment_reactions` | `comment_reactions_user_id_not_null` | `NOT NULL user_id` |
| `comment_watch_replies` | `comment_watch_replies_comment_id_fkey` | `FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_watch_replies` | `comment_watch_replies_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_watch_replies` | `comment_watch_replies_created_at_not_null` | `NOT NULL created_at` |
| `comment_watch_replies` | `comment_watch_replies_notification_id_fkey` | `FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE SET NULL` |
| `comment_watch_replies` | `comment_watch_replies_pkey` | `PRIMARY KEY (watch_id, comment_id)` |
| `comment_watch_replies` | `comment_watch_replies_watch_id_fkey` | `FOREIGN KEY (watch_id) REFERENCES comment_watches(id) ON DELETE CASCADE` |
| `comment_watch_replies` | `comment_watch_replies_watch_id_not_null` | `NOT NULL watch_id` |
| `comment_watches` | `comment_watches_comment_id_fkey` | `FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE` |
| `comment_watches` | `comment_watches_comment_id_not_null` | `NOT NULL comment_id` |
| `comment_watches` | `comment_watches_created_at_not_null` | `NOT NULL created_at` |
| `comment_watches` | `comment_watches_id_not_null` | `NOT NULL id` |
| `comment_watches` | `comment_watches_last_activity_at_not_null` | `NOT NULL last_activity_at` |
| `comment_watches` | `comment_watches_last_read_comment_id_fkey` | `FOREIGN KEY (last_read_comment_id) REFERENCES comments(id) ON DELETE SET NULL` |
| `comment_watches` | `comment_watches_muted_forever_not_null` | `NOT NULL muted_forever` |
| `comment_watches` | `comment_watches_pkey` | `PRIMARY KEY (id)` |
| `comment_watches` | `comment_watches_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `comment_watches` | `comment_watches_public_id_key` | `UNIQUE (public_id)` |
| `comment_watches` | `comment_watches_public_id_not_null` | `NOT NULL public_id` |
| `comment_watches` | `comment_watches_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'cancelled'::text]))` |
| `comment_watches` | `comment_watches_status_not_null` | `NOT NULL status` |
| `comment_watches` | `comment_watches_unread_count_check` | `CHECK (unread_count >= 0)` |
| `comment_watches` | `comment_watches_unread_count_not_null` | `NOT NULL unread_count` |
| `comment_watches` | `comment_watches_updated_at_not_null` | `NOT NULL updated_at` |
| `comment_watches` | `comment_watches_user_id_comment_id_key` | `UNIQUE (user_id, comment_id)` |
| `comment_watches` | `comment_watches_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `comment_watches` | `comment_watches_user_id_not_null` | `NOT NULL user_id` |
| `comment_watches` | `comment_watches_watched_reply_count_check` | `CHECK (watched_reply_count >= 0)` |
| `comment_watches` | `comment_watches_watched_reply_count_not_null` | `NOT NULL watched_reply_count` |
| `comments` | `comments_author_id_fkey` | `FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `comments` | `comments_author_id_not_null` | `NOT NULL author_id` |
| `comments` | `comments_body_not_null` | `NOT NULL body` |
| `comments` | `comments_check` | `CHECK (target_type = 'mod_resource'::text AND target_version_id IS NOT NULL OR target_type <> 'mod_resource'::text AND target_version_id IS NULL)` |
| `comments` | `comments_check1` | `CHECK (parent_id IS NULL AND root_id IS NULL AND depth = 0 OR parent_id IS NOT NULL AND root_id IS NOT NULL AND depth > 0)` |
| `comments` | `comments_child_count_check` | `CHECK (child_count >= 0)` |
| `comments` | `comments_child_count_not_null` | `NOT NULL child_count` |
| `comments` | `comments_created_at_not_null` | `NOT NULL created_at` |
| `comments` | `comments_depth_check` | `CHECK (depth >= 0 AND depth <= 256)` |
| `comments` | `comments_depth_not_null` | `NOT NULL depth` |
| `comments` | `comments_descendant_count_check` | `CHECK (descendant_count >= 0)` |
| `comments` | `comments_descendant_count_not_null` | `NOT NULL descendant_count` |
| `comments` | `comments_hot_score_check` | `CHECK (hot_score >= 0::numeric)` |
| `comments` | `comments_hot_score_not_null` | `NOT NULL hot_score` |
| `comments` | `comments_id_not_null` | `NOT NULL id` |
| `comments` | `comments_idempotency_key_not_null` | `NOT NULL idempotency_key` |
| `comments` | `comments_like_count_check` | `CHECK (like_count >= 0)` |
| `comments` | `comments_like_count_not_null` | `NOT NULL like_count` |
| `comments` | `comments_parent_id_fkey` | `FOREIGN KEY (parent_id) REFERENCES comments(id) ON DELETE RESTRICT` |
| `comments` | `comments_pinned_by_fkey` | `FOREIGN KEY (pinned_by) REFERENCES users(id) ON DELETE SET NULL` |
| `comments` | `comments_pkey` | `PRIMARY KEY (id)` |
| `comments` | `comments_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `comments` | `comments_public_id_key` | `UNIQUE (public_id)` |
| `comments` | `comments_public_id_not_null` | `NOT NULL public_id` |
| `comments` | `comments_quality_score_check` | `CHECK (quality_score >= 0::numeric AND quality_score <= 1::numeric)` |
| `comments` | `comments_quality_score_not_null` | `NOT NULL quality_score` |
| `comments` | `comments_root_id_fkey` | `FOREIGN KEY (root_id) REFERENCES comments(id) ON DELETE RESTRICT` |
| `comments` | `comments_status_check` | `CHECK (status = ANY (ARRAY['published'::text, 'pending'::text, 'hidden'::text, 'deleted'::text, 'spam'::text]))` |
| `comments` | `comments_status_not_null` | `NOT NULL status` |
| `comments` | `comments_target_id_not_null` | `NOT NULL target_id` |
| `comments` | `comments_target_type_check` | `CHECK (target_type ~ '^[a-z][a-z0-9_]{1,63}$'::text)` |
| `comments` | `comments_target_type_not_null` | `NOT NULL target_type` |
| `comments` | `comments_target_version_id_fkey` | `FOREIGN KEY (target_version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `comments` | `comments_unique_reply_users_check` | `CHECK (unique_reply_users >= 0)` |
| `comments` | `comments_unique_reply_users_not_null` | `NOT NULL unique_reply_users` |
| `comments` | `comments_updated_at_not_null` | `NOT NULL updated_at` |
| `comments` | `comments_watch_count_check` | `CHECK (watch_count >= 0)` |
| `comments` | `comments_watch_count_not_null` | `NOT NULL watch_count` |
| `community_post_bounties` | `community_post_bounties_amount_check` | `CHECK (amount > 0)` |
| `community_post_bounties` | `community_post_bounties_amount_not_null` | `NOT NULL amount` |
| `community_post_bounties` | `community_post_bounties_check` | `CHECK (status = 'held'::text OR settled_at IS NOT NULL)` |
| `community_post_bounties` | `community_post_bounties_check1` | `CHECK (status <> 'awarded'::text OR recipient_id IS NOT NULL)` |
| `community_post_bounties` | `community_post_bounties_created_at_not_null` | `NOT NULL created_at` |
| `community_post_bounties` | `community_post_bounties_currency_id_fkey` | `FOREIGN KEY (currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `community_post_bounties` | `community_post_bounties_currency_id_not_null` | `NOT NULL currency_id` |
| `community_post_bounties` | `community_post_bounties_net_amount_check` | `CHECK (net_amount >= 0)` |
| `community_post_bounties` | `community_post_bounties_net_amount_not_null` | `NOT NULL net_amount` |
| `community_post_bounties` | `community_post_bounties_pkey` | `PRIMARY KEY (post_id)` |
| `community_post_bounties` | `community_post_bounties_post_id_fkey` | `FOREIGN KEY (post_id) REFERENCES community_posts(id) ON DELETE CASCADE` |
| `community_post_bounties` | `community_post_bounties_post_id_not_null` | `NOT NULL post_id` |
| `community_post_bounties` | `community_post_bounties_recipient_id_fkey` | `FOREIGN KEY (recipient_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `community_post_bounties` | `community_post_bounties_status_check` | `CHECK (status = ANY (ARRAY['held'::text, 'awarded'::text, 'refunded'::text]))` |
| `community_post_bounties` | `community_post_bounties_status_not_null` | `NOT NULL status` |
| `community_post_bounties` | `community_post_bounties_tax_amount_check` | `CHECK (tax_amount >= 0)` |
| `community_post_bounties` | `community_post_bounties_tax_amount_not_null` | `NOT NULL tax_amount` |
| `community_post_project_refs` | `community_post_project_refs_check` | `CHECK ((target_id IS NULL) <> (raw_identifier = ''::text))` |
| `community_post_project_refs` | `community_post_project_refs_display_order_not_null` | `NOT NULL display_order` |
| `community_post_project_refs` | `community_post_project_refs_id_not_null` | `NOT NULL id` |
| `community_post_project_refs` | `community_post_project_refs_pkey` | `PRIMARY KEY (id)` |
| `community_post_project_refs` | `community_post_project_refs_post_id_fkey` | `FOREIGN KEY (post_id) REFERENCES community_posts(id) ON DELETE CASCADE` |
| `community_post_project_refs` | `community_post_project_refs_post_id_not_null` | `NOT NULL post_id` |
| `community_post_project_refs` | `community_post_project_refs_raw_identifier_not_null` | `NOT NULL raw_identifier` |
| `community_post_project_refs` | `community_post_project_refs_target_type_not_null` | `NOT NULL target_type` |
| `community_post_project_refs` | `community_post_project_refs_target_type_target_id_fkey` | `FOREIGN KEY (target_type, target_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE RESTRICT` |
| `community_post_resource_refs` | `community_post_resource_refs_check` | `CHECK ((resource_id IS NULL) <> (raw_resource_id = ''::text))` |
| `community_post_resource_refs` | `community_post_resource_refs_display_order_not_null` | `NOT NULL display_order` |
| `community_post_resource_refs` | `community_post_resource_refs_id_not_null` | `NOT NULL id` |
| `community_post_resource_refs` | `community_post_resource_refs_kind_code_not_null` | `NOT NULL kind_code` |
| `community_post_resource_refs` | `community_post_resource_refs_pkey` | `PRIMARY KEY (id)` |
| `community_post_resource_refs` | `community_post_resource_refs_post_id_fkey` | `FOREIGN KEY (post_id) REFERENCES community_posts(id) ON DELETE CASCADE` |
| `community_post_resource_refs` | `community_post_resource_refs_post_id_not_null` | `NOT NULL post_id` |
| `community_post_resource_refs` | `community_post_resource_refs_raw_resource_id_not_null` | `NOT NULL raw_resource_id` |
| `community_post_resource_refs` | `community_post_resource_refs_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES catalog_entities(id) ON DELETE RESTRICT` |
| `community_post_translations` | `community_post_translations_ai_task_id_fkey` | `FOREIGN KEY (ai_task_id) REFERENCES ai_tasks(id) ON DELETE SET NULL` |
| `community_post_translations` | `community_post_translations_body_markdown_not_null` | `NOT NULL body_markdown` |
| `community_post_translations` | `community_post_translations_created_at_not_null` | `NOT NULL created_at` |
| `community_post_translations` | `community_post_translations_locale_not_null` | `NOT NULL locale` |
| `community_post_translations` | `community_post_translations_pkey` | `PRIMARY KEY (post_id, locale)` |
| `community_post_translations` | `community_post_translations_post_id_fkey` | `FOREIGN KEY (post_id) REFERENCES community_posts(id) ON DELETE CASCADE` |
| `community_post_translations` | `community_post_translations_post_id_not_null` | `NOT NULL post_id` |
| `community_post_translations` | `community_post_translations_source_revision_id_fkey` | `FOREIGN KEY (source_revision_id) REFERENCES content_revisions(id) ON DELETE CASCADE` |
| `community_post_translations` | `community_post_translations_source_revision_id_not_null` | `NOT NULL source_revision_id` |
| `community_post_translations` | `community_post_translations_title_not_null` | `NOT NULL title` |
| `community_post_translations` | `community_post_translations_updated_at_not_null` | `NOT NULL updated_at` |
| `community_posts` | `community_posts_accepted_comment_id_fkey` | `FOREIGN KEY (accepted_comment_id) REFERENCES comments(id) ON DELETE RESTRICT` |
| `community_posts` | `community_posts_author_id_fkey` | `FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `community_posts` | `community_posts_author_id_not_null` | `NOT NULL author_id` |
| `community_posts` | `community_posts_body_markdown_not_null` | `NOT NULL body_markdown` |
| `community_posts` | `community_posts_category_not_null` | `NOT NULL category` |
| `community_posts` | `community_posts_check` | `CHECK (kind = 'issue'::text OR severity = ''::text AND mod_version_min = ''::text AND mod_version_max = ''::text AND NOT has_fix AND issue_url = ''::text)` |
| `community_posts` | `community_posts_check1` | `CHECK (kind <> 'issue'::text OR severity <> ''::text)` |
| `community_posts` | `community_posts_check2` | `CHECK (kind = 'discussion'::text OR resolution_status = 'open'::text AND accepted_comment_id IS NULL AND resolved_at IS NULL)` |
| `community_posts` | `community_posts_check3` | `CHECK (resolution_status = 'answered'::text OR accepted_comment_id IS NULL)` |
| `community_posts` | `community_posts_check4` | `CHECK ((resolution_status = 'open'::text) = (resolved_at IS NULL))` |
| `community_posts` | `community_posts_cover_file_id_fkey` | `FOREIGN KEY (cover_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `community_posts` | `community_posts_created_at_not_null` | `NOT NULL created_at` |
| `community_posts` | `community_posts_has_fix_not_null` | `NOT NULL has_fix` |
| `community_posts` | `community_posts_id_not_null` | `NOT NULL id` |
| `community_posts` | `community_posts_issue_url_not_null` | `NOT NULL issue_url` |
| `community_posts` | `community_posts_kind_check` | `CHECK (kind = ANY (ARRAY['tutorial'::text, 'issue'::text, 'news'::text, 'discussion'::text]))` |
| `community_posts` | `community_posts_kind_not_null` | `NOT NULL kind` |
| `community_posts` | `community_posts_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `community_posts` | `community_posts_mod_version_max_not_null` | `NOT NULL mod_version_max` |
| `community_posts` | `community_posts_mod_version_min_not_null` | `NOT NULL mod_version_min` |
| `community_posts` | `community_posts_pkey` | `PRIMARY KEY (id)` |
| `community_posts` | `community_posts_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `community_posts` | `community_posts_public_id_key` | `UNIQUE (public_id)` |
| `community_posts` | `community_posts_public_id_not_null` | `NOT NULL public_id` |
| `community_posts` | `community_posts_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `community_posts` | `community_posts_resolution_status_check` | `CHECK (resolution_status = ANY (ARRAY['open'::text, 'answered'::text, 'self_solved'::text]))` |
| `community_posts` | `community_posts_resolution_status_not_null` | `NOT NULL resolution_status` |
| `community_posts` | `community_posts_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `community_posts` | `community_posts_review_status_not_null` | `NOT NULL review_status` |
| `community_posts` | `community_posts_severity_check` | `CHECK (severity = ANY (ARRAY[''::text, 'client'::text, 'harmless'::text, 'minor'::text, 'harmful'::text, 'severe'::text, 'fatal'::text]))` |
| `community_posts` | `community_posts_severity_not_null` | `NOT NULL severity` |
| `community_posts` | `community_posts_source_locale_not_null` | `NOT NULL source_locale` |
| `community_posts` | `community_posts_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text]))` |
| `community_posts` | `community_posts_status_not_null` | `NOT NULL status` |
| `community_posts` | `community_posts_title_not_null` | `NOT NULL title` |
| `community_posts` | `community_posts_updated_at_not_null` | `NOT NULL updated_at` |
| `content_change_items` | `content_change_items_id_not_null` | `NOT NULL id` |
| `content_change_items` | `content_change_items_operation_check` | `CHECK (operation = ANY (ARRAY['add'::text, 'remove'::text, 'replace'::text]))` |
| `content_change_items` | `content_change_items_operation_not_null` | `NOT NULL operation` |
| `content_change_items` | `content_change_items_path_not_null` | `NOT NULL path` |
| `content_change_items` | `content_change_items_pkey` | `PRIMARY KEY (id)` |
| `content_change_items` | `content_change_items_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `content_change_items` | `content_change_items_revision_id_not_null` | `NOT NULL revision_id` |
| `content_creator_bindings` | `content_creator_bindings_approved_by_fkey` | `FOREIGN KEY (approved_by) REFERENCES users(id) ON DELETE SET NULL` |
| `content_creator_bindings` | `content_creator_bindings_created_at_not_null` | `NOT NULL created_at` |
| `content_creator_bindings` | `content_creator_bindings_creator_id_not_null` | `NOT NULL creator_id` |
| `content_creator_bindings` | `content_creator_bindings_display_order_not_null` | `NOT NULL display_order` |
| `content_creator_bindings` | `content_creator_bindings_id_not_null` | `NOT NULL id` |
| `content_creator_bindings` | `content_creator_bindings_name_snapshot_not_null` | `NOT NULL name_snapshot` |
| `content_creator_bindings` | `content_creator_bindings_permission_granting_not_null` | `NOT NULL permission_granting` |
| `content_creator_bindings` | `content_creator_bindings_pkey` | `PRIMARY KEY (id)` |
| `content_creator_bindings` | `content_creator_bindings_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `content_creator_bindings` | `content_creator_bindings_public_id_key` | `UNIQUE (public_id)` |
| `content_creator_bindings` | `content_creator_bindings_public_id_not_null` | `NOT NULL public_id` |
| `content_creator_bindings` | `content_creator_bindings_role_snapshot_not_null` | `NOT NULL role_snapshot` |
| `content_creator_bindings` | `content_creator_bindings_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'revoked'::text]))` |
| `content_creator_bindings` | `content_creator_bindings_status_not_null` | `NOT NULL status` |
| `content_creator_bindings` | `content_creator_bindings_subject_id_not_null` | `NOT NULL subject_id` |
| `content_creator_bindings` | `content_creator_bindings_subject_type_not_null` | `NOT NULL subject_type` |
| `content_creator_bindings` | `content_creator_bindings_subject_type_subject_id_fkey` | `FOREIGN KEY (subject_type, subject_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE CASCADE` |
| `content_creator_bindings` | `fk_content_creator_bindings_creator` | `FOREIGN KEY (creator_id) REFERENCES creators(id) ON DELETE RESTRICT` |
| `content_creator_bindings` | `fk_content_creator_bindings_role` | `FOREIGN KEY (role_id) REFERENCES creator_role_definitions(id) ON DELETE SET NULL` |
| `content_download_counters` | `content_download_counters_downloads_check` | `CHECK (downloads >= 0)` |
| `content_download_counters` | `content_download_counters_downloads_not_null` | `NOT NULL downloads` |
| `content_download_counters` | `content_download_counters_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_download_counters` | `content_download_counters_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_download_counters` | `content_download_counters_owner_id_fkey` | `FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE` |
| `content_download_counters` | `content_download_counters_owner_id_not_null` | `NOT NULL owner_id` |
| `content_download_counters` | `content_download_counters_pkey` | `PRIMARY KEY (object_route_id, owner_id)` |
| `content_download_reward_counters` | `content_download_reward_counters_currency_id_fkey` | `FOREIGN KEY (currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `content_download_reward_counters` | `content_download_reward_counters_currency_id_not_null` | `NOT NULL currency_id` |
| `content_download_reward_counters` | `content_download_reward_counters_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_download_reward_counters` | `content_download_reward_counters_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_download_reward_counters` | `content_download_reward_counters_owner_id_fkey` | `FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE` |
| `content_download_reward_counters` | `content_download_reward_counters_owner_id_not_null` | `NOT NULL owner_id` |
| `content_download_reward_counters` | `content_download_reward_counters_pkey` | `PRIMARY KEY (object_route_id, owner_id, currency_id)` |
| `content_download_reward_counters` | `content_download_reward_counters_rewarded_steps_check` | `CHECK (rewarded_steps >= 0)` |
| `content_download_reward_counters` | `content_download_reward_counters_rewarded_steps_not_null` | `NOT NULL rewarded_steps` |
| `content_download_reward_counters` | `content_download_reward_counters_updated_at_not_null` | `NOT NULL updated_at` |
| `content_heat_promotions` | `content_heat_promotions_applied_by_fkey` | `FOREIGN KEY (applied_by) REFERENCES users(id) ON DELETE RESTRICT` |
| `content_heat_promotions` | `content_heat_promotions_applied_by_not_null` | `NOT NULL applied_by` |
| `content_heat_promotions` | `content_heat_promotions_base_power_check` | `CHECK (base_power > 0::numeric)` |
| `content_heat_promotions` | `content_heat_promotions_base_power_not_null` | `NOT NULL base_power` |
| `content_heat_promotions` | `content_heat_promotions_check` | `CHECK (expires_at > started_at)` |
| `content_heat_promotions` | `content_heat_promotions_effective_power_check` | `CHECK (effective_power > 0::numeric)` |
| `content_heat_promotions` | `content_heat_promotions_effective_power_not_null` | `NOT NULL effective_power` |
| `content_heat_promotions` | `content_heat_promotions_expires_at_not_null` | `NOT NULL expires_at` |
| `content_heat_promotions` | `content_heat_promotions_half_life_hours_check` | `CHECK (half_life_hours >= 1 AND half_life_hours <= 8760)` |
| `content_heat_promotions` | `content_heat_promotions_half_life_hours_not_null` | `NOT NULL half_life_hours` |
| `content_heat_promotions` | `content_heat_promotions_id_not_null` | `NOT NULL id` |
| `content_heat_promotions` | `content_heat_promotions_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_heat_promotions` | `content_heat_promotions_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_heat_promotions` | `content_heat_promotions_pkey` | `PRIMARY KEY (id)` |
| `content_heat_promotions` | `content_heat_promotions_promotion_kind_check` | `CHECK (promotion_kind = ANY (ARRAY['project'::text, 'server'::text]))` |
| `content_heat_promotions` | `content_heat_promotions_promotion_kind_not_null` | `NOT NULL promotion_kind` |
| `content_heat_promotions` | `content_heat_promotions_sequence_no_check` | `CHECK (sequence_no > 0)` |
| `content_heat_promotions` | `content_heat_promotions_sequence_no_not_null` | `NOT NULL sequence_no` |
| `content_heat_promotions` | `content_heat_promotions_shop_item_id_fkey` | `FOREIGN KEY (shop_item_id) REFERENCES shop_items(id) ON DELETE RESTRICT` |
| `content_heat_promotions` | `content_heat_promotions_shop_item_id_not_null` | `NOT NULL shop_item_id` |
| `content_heat_promotions` | `content_heat_promotions_started_at_not_null` | `NOT NULL started_at` |
| `content_localizations` | `content_localizations_ai_task_id_fkey` | `FOREIGN KEY (ai_task_id) REFERENCES ai_tasks(id) ON DELETE SET NULL` |
| `content_localizations` | `content_localizations_catalog_entity_id_fkey` | `FOREIGN KEY (catalog_entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `content_localizations` | `content_localizations_catalog_entity_id_subject_type_fkey` | `FOREIGN KEY (catalog_entity_id, subject_type) REFERENCES catalog_entities(id, entity_type) ON DELETE CASCADE` |
| `content_localizations` | `content_localizations_check` | `CHECK (catalog_entity_id IS NULL OR catalog_entity_id = subject_id)` |
| `content_localizations` | `content_localizations_content_markdown_not_null` | `NOT NULL content_markdown` |
| `content_localizations` | `content_localizations_created_at_not_null` | `NOT NULL created_at` |
| `content_localizations` | `content_localizations_editable_not_null` | `NOT NULL editable` |
| `content_localizations` | `content_localizations_locale_check` | `CHECK (locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'::text)` |
| `content_localizations` | `content_localizations_locale_not_null` | `NOT NULL locale` |
| `content_localizations` | `content_localizations_name_not_null` | `NOT NULL name` |
| `content_localizations` | `content_localizations_pkey` | `PRIMARY KEY (subject_type, subject_id, locale)` |
| `content_localizations` | `content_localizations_provenance_check` | `CHECK (provenance = ANY (ARRAY['human'::text, 'import'::text, 'ai'::text, 'human_corrected'::text]))` |
| `content_localizations` | `content_localizations_provenance_not_null` | `NOT NULL provenance` |
| `content_localizations` | `content_localizations_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `content_localizations` | `content_localizations_review_status_not_null` | `NOT NULL review_status` |
| `content_localizations` | `content_localizations_revision_no_check` | `CHECK (revision_no > 0)` |
| `content_localizations` | `content_localizations_revision_no_not_null` | `NOT NULL revision_no` |
| `content_localizations` | `content_localizations_source_locale_not_null` | `NOT NULL source_locale` |
| `content_localizations` | `content_localizations_subject_id_not_null` | `NOT NULL subject_id` |
| `content_localizations` | `content_localizations_subject_type_not_null` | `NOT NULL subject_type` |
| `content_localizations` | `content_localizations_subject_type_subject_id_fkey` | `FOREIGN KEY (subject_type, subject_id) REFERENCES content_subjects(subject_type, subject_id) ON DELETE CASCADE` |
| `content_localizations` | `content_localizations_summary_not_null` | `NOT NULL summary` |
| `content_localizations` | `content_localizations_updated_at_not_null` | `NOT NULL updated_at` |
| `content_localizations` | `content_localizations_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `content_localizations` | `fk_content_localizations_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_comment_count_check` | `CHECK (comment_count >= 0)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_comment_count_not_null` | `NOT NULL comment_count` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_download_count_check` | `CHECK (download_count >= 0)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_download_count_not_null` | `NOT NULL download_count` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_favorite_count_check` | `CHECK (favorite_count >= 0)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_favorite_count_not_null` | `NOT NULL favorite_count` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_heat_score_check` | `CHECK (heat_score >= 0::numeric)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_heat_score_not_null` | `NOT NULL heat_score` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_metric_date_not_null` | `NOT NULL metric_date` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_pkey` | `PRIMARY KEY (object_route_id, metric_date)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_rating_count_check` | `CHECK (rating_count >= 0)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_rating_count_not_null` | `NOT NULL rating_count` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_updated_at_not_null` | `NOT NULL updated_at` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_view_count_check` | `CHECK (view_count >= 0)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_view_count_not_null` | `NOT NULL view_count` |
| `content_popularity_events_daily` | `content_popularity_events_daily_comment_value_not_null` | `NOT NULL comment_value` |
| `content_popularity_events_daily` | `content_popularity_events_daily_event_date_not_null` | `NOT NULL event_date` |
| `content_popularity_events_daily` | `content_popularity_events_daily_favorite_value_not_null` | `NOT NULL favorite_value` |
| `content_popularity_events_daily` | `content_popularity_events_daily_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_popularity_events_daily` | `content_popularity_events_daily_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_popularity_events_daily` | `content_popularity_events_daily_pkey` | `PRIMARY KEY (object_route_id, event_date)` |
| `content_popularity_events_daily` | `content_popularity_events_daily_rating_value_not_null` | `NOT NULL rating_value` |
| `content_popularity_events_daily` | `content_popularity_events_daily_release_value_not_null` | `NOT NULL release_value` |
| `content_popularity_stats` | `content_popularity_stats_bayesian_rating_check` | `CHECK (bayesian_rating >= 0::numeric AND bayesian_rating <= 5::numeric)` |
| `content_popularity_stats` | `content_popularity_stats_bayesian_rating_not_null` | `NOT NULL bayesian_rating` |
| `content_popularity_stats` | `content_popularity_stats_comment_count_check` | `CHECK (comment_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_comment_count_not_null` | `NOT NULL comment_count` |
| `content_popularity_stats` | `content_popularity_stats_dimension_averages_not_null` | `NOT NULL dimension_averages` |
| `content_popularity_stats` | `content_popularity_stats_download_count_check` | `CHECK (download_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_download_count_not_null` | `NOT NULL download_count` |
| `content_popularity_stats` | `content_popularity_stats_effective_commenter_count_check` | `CHECK (effective_commenter_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_effective_commenter_count_not_null` | `NOT NULL effective_commenter_count` |
| `content_popularity_stats` | `content_popularity_stats_effective_view_score_not_null` | `NOT NULL effective_view_score` |
| `content_popularity_stats` | `content_popularity_stats_favorite_count_check` | `CHECK (favorite_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_favorite_count_not_null` | `NOT NULL favorite_count` |
| `content_popularity_stats` | `content_popularity_stats_heat_score_check` | `CHECK (heat_score >= 0::numeric)` |
| `content_popularity_stats` | `content_popularity_stats_heat_score_not_null` | `NOT NULL heat_score` |
| `content_popularity_stats` | `content_popularity_stats_long_term_score_not_null` | `NOT NULL long_term_score` |
| `content_popularity_stats` | `content_popularity_stats_new_project_boost_not_null` | `NOT NULL new_project_boost` |
| `content_popularity_stats` | `content_popularity_stats_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_popularity_stats` | `content_popularity_stats_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_popularity_stats` | `content_popularity_stats_page_count_check` | `CHECK (page_count > 0)` |
| `content_popularity_stats` | `content_popularity_stats_page_count_not_null` | `NOT NULL page_count` |
| `content_popularity_stats` | `content_popularity_stats_pkey` | `PRIMARY KEY (object_route_id)` |
| `content_popularity_stats` | `content_popularity_stats_promotion_score_not_null` | `NOT NULL promotion_score` |
| `content_popularity_stats` | `content_popularity_stats_quality_modifier_not_null` | `NOT NULL quality_modifier` |
| `content_popularity_stats` | `content_popularity_stats_rating_average_check` | `CHECK (rating_average >= 0::numeric AND rating_average <= 5::numeric)` |
| `content_popularity_stats` | `content_popularity_stats_rating_average_not_null` | `NOT NULL rating_average` |
| `content_popularity_stats` | `content_popularity_stats_rating_count_check` | `CHECK (rating_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_rating_count_not_null` | `NOT NULL rating_count` |
| `content_popularity_stats` | `content_popularity_stats_rating_sum_check` | `CHECK (rating_sum >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_rating_sum_not_null` | `NOT NULL rating_sum` |
| `content_popularity_stats` | `content_popularity_stats_trend_score_not_null` | `NOT NULL trend_score` |
| `content_popularity_stats` | `content_popularity_stats_unique_view_count_check` | `CHECK (unique_view_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_unique_view_count_not_null` | `NOT NULL unique_view_count` |
| `content_popularity_stats` | `content_popularity_stats_updated_at_not_null` | `NOT NULL updated_at` |
| `content_popularity_stats` | `content_popularity_stats_view_count_check` | `CHECK (view_count >= 0)` |
| `content_popularity_stats` | `content_popularity_stats_view_count_not_null` | `NOT NULL view_count` |
| `content_popularity_thresholds` | `content_popularity_thresholds_commenter_threshold_check` | `CHECK (commenter_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_commenter_threshold_not_null` | `NOT NULL commenter_threshold` |
| `content_popularity_thresholds` | `content_popularity_thresholds_download_threshold_check` | `CHECK (download_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_download_threshold_not_null` | `NOT NULL download_threshold` |
| `content_popularity_thresholds` | `content_popularity_thresholds_entity_type_not_null` | `NOT NULL entity_type` |
| `content_popularity_thresholds` | `content_popularity_thresholds_favorite_threshold_check` | `CHECK (favorite_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_favorite_threshold_not_null` | `NOT NULL favorite_threshold` |
| `content_popularity_thresholds` | `content_popularity_thresholds_pkey` | `PRIMARY KEY (entity_type)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_rating_threshold_check` | `CHECK (rating_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_rating_threshold_not_null` | `NOT NULL rating_threshold` |
| `content_popularity_thresholds` | `content_popularity_thresholds_trend_threshold_check` | `CHECK (trend_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_trend_threshold_not_null` | `NOT NULL trend_threshold` |
| `content_popularity_thresholds` | `content_popularity_thresholds_view_threshold_check` | `CHECK (view_threshold > 0::numeric)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_view_threshold_not_null` | `NOT NULL view_threshold` |
| `content_project_pages` | `content_project_pages_first_seen_at_not_null` | `NOT NULL first_seen_at` |
| `content_project_pages` | `content_project_pages_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_project_pages` | `content_project_pages_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_project_pages` | `content_project_pages_page_hash_not_null` | `NOT NULL page_hash` |
| `content_project_pages` | `content_project_pages_pkey` | `PRIMARY KEY (object_route_id, page_hash)` |
| `content_rating_global_stats` | `content_rating_global_stats_average_rating_check` | `CHECK (average_rating >= 0::numeric AND average_rating <= 5::numeric)` |
| `content_rating_global_stats` | `content_rating_global_stats_average_rating_not_null` | `NOT NULL average_rating` |
| `content_rating_global_stats` | `content_rating_global_stats_entity_type_not_null` | `NOT NULL entity_type` |
| `content_rating_global_stats` | `content_rating_global_stats_pkey` | `PRIMARY KEY (entity_type)` |
| `content_rating_global_stats` | `content_rating_global_stats_rating_count_check` | `CHECK (rating_count >= 0)` |
| `content_rating_global_stats` | `content_rating_global_stats_rating_count_not_null` | `NOT NULL rating_count` |
| `content_rating_global_stats` | `content_rating_global_stats_rating_sum_check` | `CHECK (rating_sum >= 0)` |
| `content_rating_global_stats` | `content_rating_global_stats_rating_sum_not_null` | `NOT NULL rating_sum` |
| `content_rating_global_stats` | `content_rating_global_stats_updated_at_not_null` | `NOT NULL updated_at` |
| `content_rating_scores` | `content_rating_scores_dimension_code_check` | `CHECK (dimension_code ~ '^[a-z][a-z0-9_]{1,63}$'::text)` |
| `content_rating_scores` | `content_rating_scores_dimension_code_not_null` | `NOT NULL dimension_code` |
| `content_rating_scores` | `content_rating_scores_pkey` | `PRIMARY KEY (rating_id, dimension_code)` |
| `content_rating_scores` | `content_rating_scores_rating_id_fkey` | `FOREIGN KEY (rating_id) REFERENCES content_ratings(id) ON DELETE CASCADE` |
| `content_rating_scores` | `content_rating_scores_rating_id_not_null` | `NOT NULL rating_id` |
| `content_rating_scores` | `content_rating_scores_score_check` | `CHECK (score >= 1 AND score <= 5)` |
| `content_rating_scores` | `content_rating_scores_score_not_null` | `NOT NULL score` |
| `content_ratings` | `content_ratings_author_id_fkey` | `FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE CASCADE` |
| `content_ratings` | `content_ratings_author_id_not_null` | `NOT NULL author_id` |
| `content_ratings` | `content_ratings_created_at_not_null` | `NOT NULL created_at` |
| `content_ratings` | `content_ratings_id_not_null` | `NOT NULL id` |
| `content_ratings` | `content_ratings_message_check` | `CHECK (char_length(message) <= 2000)` |
| `content_ratings` | `content_ratings_message_not_null` | `NOT NULL message` |
| `content_ratings` | `content_ratings_object_route_id_author_id_key` | `UNIQUE (object_route_id, author_id)` |
| `content_ratings` | `content_ratings_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_ratings` | `content_ratings_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_ratings` | `content_ratings_overall_score_check` | `CHECK (overall_score >= 1 AND overall_score <= 5)` |
| `content_ratings` | `content_ratings_overall_score_not_null` | `NOT NULL overall_score` |
| `content_ratings` | `content_ratings_pkey` | `PRIMARY KEY (id)` |
| `content_ratings` | `content_ratings_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `content_ratings` | `content_ratings_public_id_key` | `UNIQUE (public_id)` |
| `content_ratings` | `content_ratings_public_id_not_null` | `NOT NULL public_id` |
| `content_ratings` | `content_ratings_status_check` | `CHECK (status = ANY (ARRAY['published'::text, 'hidden'::text]))` |
| `content_ratings` | `content_ratings_status_not_null` | `NOT NULL status` |
| `content_ratings` | `content_ratings_updated_at_not_null` | `NOT NULL updated_at` |
| `content_revisions` | `content_revisions_aggregate_key_not_null` | `NOT NULL aggregate_key` |
| `content_revisions` | `content_revisions_aggregate_type_aggregate_key_revision_no_key` | `UNIQUE (aggregate_type, aggregate_key, revision_no)` |
| `content_revisions` | `content_revisions_aggregate_type_not_null` | `NOT NULL aggregate_type` |
| `content_revisions` | `content_revisions_base_revision_id_fkey` | `FOREIGN KEY (base_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `content_revisions` | `content_revisions_check` | `CHECK ((entity_type IS NULL) = (entity_id IS NULL))` |
| `content_revisions` | `content_revisions_created_at_not_null` | `NOT NULL created_at` |
| `content_revisions` | `content_revisions_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `content_revisions` | `content_revisions_created_by_snapshot_not_null` | `NOT NULL created_by_snapshot` |
| `content_revisions` | `content_revisions_entity_type_entity_id_fkey` | `FOREIGN KEY (entity_type, entity_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE RESTRICT` |
| `content_revisions` | `content_revisions_id_not_null` | `NOT NULL id` |
| `content_revisions` | `content_revisions_pkey` | `PRIMARY KEY (id)` |
| `content_revisions` | `content_revisions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `content_revisions` | `content_revisions_public_id_key` | `UNIQUE (public_id)` |
| `content_revisions` | `content_revisions_public_id_not_null` | `NOT NULL public_id` |
| `content_revisions` | `content_revisions_revision_no_not_null` | `NOT NULL revision_no` |
| `content_revisions` | `content_revisions_schema_version_not_null` | `NOT NULL schema_version` |
| `content_revisions` | `content_revisions_snapshot_hash_not_null` | `NOT NULL snapshot_hash` |
| `content_revisions` | `content_revisions_snapshot_not_null` | `NOT NULL snapshot` |
| `content_revisions` | `content_revisions_source_not_null` | `NOT NULL source` |
| `content_route_metrics` | `content_route_metrics_child_view_count_check` | `CHECK (child_view_count >= 0)` |
| `content_route_metrics` | `content_route_metrics_child_view_count_not_null` | `NOT NULL child_view_count` |
| `content_route_metrics` | `content_route_metrics_created_at_not_null` | `NOT NULL created_at` |
| `content_route_metrics` | `content_route_metrics_direct_view_count_check` | `CHECK (direct_view_count >= 0)` |
| `content_route_metrics` | `content_route_metrics_direct_view_count_not_null` | `NOT NULL direct_view_count` |
| `content_route_metrics` | `content_route_metrics_edit_count_check` | `CHECK (edit_count >= 0)` |
| `content_route_metrics` | `content_route_metrics_edit_count_not_null` | `NOT NULL edit_count` |
| `content_route_metrics` | `content_route_metrics_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_route_metrics` | `content_route_metrics_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_route_metrics` | `content_route_metrics_pkey` | `PRIMARY KEY (object_route_id)` |
| `content_route_metrics` | `content_route_metrics_total_view_count_check` | `CHECK (total_view_count >= 0)` |
| `content_route_metrics` | `content_route_metrics_total_view_count_not_null` | `NOT NULL total_view_count` |
| `content_route_metrics` | `content_route_metrics_updated_at_not_null` | `NOT NULL updated_at` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_attempts_check` | `CHECK (attempts >= 0)` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_attempts_not_null` | `NOT NULL attempts` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_available_at_not_null` | `NOT NULL available_at` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_created_at_not_null` | `NOT NULL created_at` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_last_error_not_null` | `NOT NULL last_error` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_pkey` | `PRIMARY KEY (object_route_id)` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_refresh_metrics_not_null` | `NOT NULL refresh_metrics` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_refresh_popularity_not_null` | `NOT NULL refresh_popularity` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_updated_at_not_null` | `NOT NULL updated_at` |
| `content_subjects` | `content_subjects_created_at_not_null` | `NOT NULL created_at` |
| `content_subjects` | `content_subjects_default_locale_check` | `CHECK (default_locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'::text)` |
| `content_subjects` | `content_subjects_default_locale_not_null` | `NOT NULL default_locale` |
| `content_subjects` | `content_subjects_pkey` | `PRIMARY KEY (subject_type, subject_id)` |
| `content_subjects` | `content_subjects_subject_id_not_null` | `NOT NULL subject_id` |
| `content_subjects` | `content_subjects_subject_type_not_null` | `NOT NULL subject_type` |
| `content_subjects` | `content_subjects_subject_type_subject_id_fkey` | `FOREIGN KEY (subject_type, subject_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE CASCADE` |
| `content_subjects` | `content_subjects_updated_at_not_null` | `NOT NULL updated_at` |
| `content_unique_views` | `content_unique_views_first_seen_at_not_null` | `NOT NULL first_seen_at` |
| `content_unique_views` | `content_unique_views_last_seen_at_not_null` | `NOT NULL last_seen_at` |
| `content_unique_views` | `content_unique_views_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_unique_views` | `content_unique_views_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_unique_views` | `content_unique_views_pkey` | `PRIMARY KEY (object_route_id, viewer_hash)` |
| `content_unique_views` | `content_unique_views_viewer_hash_not_null` | `NOT NULL viewer_hash` |
| `content_unique_views` | `content_unique_views_viewer_user_id_fkey` | `FOREIGN KEY (viewer_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `content_view_daily` | `content_view_daily_counter_shard_check` | `CHECK (counter_shard >= 0 AND counter_shard <= 31)` |
| `content_view_daily` | `content_view_daily_counter_shard_not_null` | `NOT NULL counter_shard` |
| `content_view_daily` | `content_view_daily_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `content_view_daily` | `content_view_daily_object_route_id_not_null` | `NOT NULL object_route_id` |
| `content_view_daily` | `content_view_daily_pkey` | `PRIMARY KEY (object_route_id, view_date, counter_shard)` |
| `content_view_daily` | `content_view_daily_view_date_not_null` | `NOT NULL view_date` |
| `content_view_daily` | `content_view_daily_views_check` | `CHECK (views >= 0)` |
| `content_view_daily` | `content_view_daily_views_not_null` | `NOT NULL views` |
| `creator_claim_attachments` | `creator_claim_attachments_claim_id_fkey` | `FOREIGN KEY (claim_id) REFERENCES creator_claims(id) ON DELETE CASCADE` |
| `creator_claim_attachments` | `creator_claim_attachments_claim_id_not_null` | `NOT NULL claim_id` |
| `creator_claim_attachments` | `creator_claim_attachments_created_at_not_null` | `NOT NULL created_at` |
| `creator_claim_attachments` | `creator_claim_attachments_display_order_not_null` | `NOT NULL display_order` |
| `creator_claim_attachments` | `creator_claim_attachments_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `creator_claim_attachments` | `creator_claim_attachments_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `creator_claim_attachments` | `creator_claim_attachments_pkey` | `PRIMARY KEY (claim_id, oss_file_id)` |
| `creator_claims` | `creator_claims_created_at_not_null` | `NOT NULL created_at` |
| `creator_claims` | `creator_claims_creator_id_fkey` | `FOREIGN KEY (creator_id) REFERENCES creators(id) ON DELETE CASCADE` |
| `creator_claims` | `creator_claims_creator_id_not_null` | `NOT NULL creator_id` |
| `creator_claims` | `creator_claims_id_not_null` | `NOT NULL id` |
| `creator_claims` | `creator_claims_pkey` | `PRIMARY KEY (id)` |
| `creator_claims` | `creator_claims_proof_markdown_not_null` | `NOT NULL proof_markdown` |
| `creator_claims` | `creator_claims_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `creator_claims` | `creator_claims_public_id_key` | `UNIQUE (public_id)` |
| `creator_claims` | `creator_claims_public_id_not_null` | `NOT NULL public_id` |
| `creator_claims` | `creator_claims_review_note_not_null` | `NOT NULL review_note` |
| `creator_claims` | `creator_claims_reviewed_by_fkey` | `FOREIGN KEY (reviewed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `creator_claims` | `creator_claims_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'withdrawn'::text, 'revoked'::text]))` |
| `creator_claims` | `creator_claims_status_not_null` | `NOT NULL status` |
| `creator_claims` | `creator_claims_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `creator_claims` | `creator_claims_user_id_not_null` | `NOT NULL user_id` |
| `creator_links` | `creator_links_created_at_not_null` | `NOT NULL created_at` |
| `creator_links` | `creator_links_creator_id_fkey` | `FOREIGN KEY (creator_id) REFERENCES creators(id) ON DELETE CASCADE` |
| `creator_links` | `creator_links_creator_id_link_type_url_key` | `UNIQUE (creator_id, link_type, url)` |
| `creator_links` | `creator_links_creator_id_not_null` | `NOT NULL creator_id` |
| `creator_links` | `creator_links_display_order_not_null` | `NOT NULL display_order` |
| `creator_links` | `creator_links_id_not_null` | `NOT NULL id` |
| `creator_links` | `creator_links_label_not_null` | `NOT NULL label` |
| `creator_links` | `creator_links_link_type_not_null` | `NOT NULL link_type` |
| `creator_links` | `creator_links_pkey` | `PRIMARY KEY (id)` |
| `creator_links` | `creator_links_url_not_null` | `NOT NULL url` |
| `creator_role_definitions` | `creator_role_definitions_code_key` | `UNIQUE (code)` |
| `creator_role_definitions` | `creator_role_definitions_code_not_null` | `NOT NULL code` |
| `creator_role_definitions` | `creator_role_definitions_created_at_not_null` | `NOT NULL created_at` |
| `creator_role_definitions` | `creator_role_definitions_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `creator_role_definitions` | `creator_role_definitions_description_not_null` | `NOT NULL description` |
| `creator_role_definitions` | `creator_role_definitions_id_not_null` | `NOT NULL id` |
| `creator_role_definitions` | `creator_role_definitions_is_custom_not_null` | `NOT NULL is_custom` |
| `creator_role_definitions` | `creator_role_definitions_name_not_null` | `NOT NULL name` |
| `creator_role_definitions` | `creator_role_definitions_permission_granting_not_null` | `NOT NULL permission_granting` |
| `creator_role_definitions` | `creator_role_definitions_pkey` | `PRIMARY KEY (id)` |
| `creator_role_definitions` | `creator_role_definitions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `creator_role_definitions` | `creator_role_definitions_public_id_key` | `UNIQUE (public_id)` |
| `creator_role_definitions` | `creator_role_definitions_public_id_not_null` | `NOT NULL public_id` |
| `creator_role_definitions` | `creator_role_definitions_translations_not_null` | `NOT NULL translations` |
| `creator_role_definitions` | `creator_role_definitions_updated_at_not_null` | `NOT NULL updated_at` |
| `creator_team_members` | `creator_team_members_approved_by_fkey` | `FOREIGN KEY (approved_by) REFERENCES users(id) ON DELETE SET NULL` |
| `creator_team_members` | `creator_team_members_check` | `CHECK (team_id <> member_creator_id)` |
| `creator_team_members` | `creator_team_members_created_at_not_null` | `NOT NULL created_at` |
| `creator_team_members` | `creator_team_members_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `creator_team_members` | `creator_team_members_display_order_not_null` | `NOT NULL display_order` |
| `creator_team_members` | `creator_team_members_member_creator_id_fkey` | `FOREIGN KEY (member_creator_id) REFERENCES creators(id) ON DELETE CASCADE` |
| `creator_team_members` | `creator_team_members_member_creator_id_not_null` | `NOT NULL member_creator_id` |
| `creator_team_members` | `creator_team_members_pkey` | `PRIMARY KEY (team_id, member_creator_id, role_id)` |
| `creator_team_members` | `creator_team_members_role_id_fkey` | `FOREIGN KEY (role_id) REFERENCES creator_role_definitions(id) ON DELETE RESTRICT` |
| `creator_team_members` | `creator_team_members_role_id_not_null` | `NOT NULL role_id` |
| `creator_team_members` | `creator_team_members_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'revoked'::text]))` |
| `creator_team_members` | `creator_team_members_status_not_null` | `NOT NULL status` |
| `creator_team_members` | `creator_team_members_team_id_fkey` | `FOREIGN KEY (team_id) REFERENCES creators(id) ON DELETE CASCADE` |
| `creator_team_members` | `creator_team_members_team_id_not_null` | `NOT NULL team_id` |
| `creator_team_members` | `creator_team_members_title_not_null` | `NOT NULL title` |
| `creator_team_members` | `creator_team_members_updated_at_not_null` | `NOT NULL updated_at` |
| `creators` | `creators_avatar_file_id_fkey` | `FOREIGN KEY (avatar_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `creators` | `creators_avatar_url_not_null` | `NOT NULL avatar_url` |
| `creators` | `creators_created_at_not_null` | `NOT NULL created_at` |
| `creators` | `creators_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `creators` | `creators_description_markdown_not_null` | `NOT NULL description_markdown` |
| `creators` | `creators_id_not_null` | `NOT NULL id` |
| `creators` | `creators_kind_check` | `CHECK (kind = ANY (ARRAY['author'::text, 'team'::text]))` |
| `creators` | `creators_kind_not_null` | `NOT NULL kind` |
| `creators` | `creators_name_not_null` | `NOT NULL name` |
| `creators` | `creators_normalized_name_not_null` | `NOT NULL normalized_name` |
| `creators` | `creators_pkey` | `PRIMARY KEY (id)` |
| `creators` | `creators_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `creators` | `creators_public_id_key` | `UNIQUE (public_id)` |
| `creators` | `creators_public_id_not_null` | `NOT NULL public_id` |
| `creators` | `creators_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `creators` | `creators_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `creators` | `creators_review_status_not_null` | `NOT NULL review_status` |
| `creators` | `creators_updated_at_not_null` | `NOT NULL updated_at` |
| `currencies` | `currencies_code_key` | `UNIQUE (code)` |
| `currencies` | `currencies_code_not_null` | `NOT NULL code` |
| `currencies` | `currencies_created_at_not_null` | `NOT NULL created_at` |
| `currencies` | `currencies_description_not_null` | `NOT NULL description` |
| `currencies` | `currencies_display_order_not_null` | `NOT NULL display_order` |
| `currencies` | `currencies_icon_not_null` | `NOT NULL icon` |
| `currencies` | `currencies_id_not_null` | `NOT NULL id` |
| `currencies` | `currencies_name_not_null` | `NOT NULL name` |
| `currencies` | `currencies_pkey` | `PRIMARY KEY (id)` |
| `currencies` | `currencies_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `currencies` | `currencies_public_id_key` | `UNIQUE (public_id)` |
| `currencies` | `currencies_public_id_not_null` | `NOT NULL public_id` |
| `currencies` | `currencies_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'disabled'::text]))` |
| `currencies` | `currencies_status_not_null` | `NOT NULL status` |
| `currencies` | `currencies_transfer_tax_bps_check` | `CHECK (transfer_tax_bps >= 0 AND transfer_tax_bps <= 10000)` |
| `currencies` | `currencies_transfer_tax_bps_not_null` | `NOT NULL transfer_tax_bps` |
| `currencies` | `currencies_translations_not_null` | `NOT NULL translations` |
| `currencies` | `currencies_updated_at_not_null` | `NOT NULL updated_at` |
| `currency_transactions` | `currency_transactions_amount_delta_not_null` | `NOT NULL amount_delta` |
| `currency_transactions` | `currency_transactions_balance_after_check` | `CHECK (balance_after >= 0)` |
| `currency_transactions` | `currency_transactions_balance_after_not_null` | `NOT NULL balance_after` |
| `currency_transactions` | `currency_transactions_counterparty_user_id_fkey` | `FOREIGN KEY (counterparty_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `currency_transactions` | `currency_transactions_created_at_not_null` | `NOT NULL created_at` |
| `currency_transactions` | `currency_transactions_currency_id_fkey` | `FOREIGN KEY (currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `currency_transactions` | `currency_transactions_currency_id_not_null` | `NOT NULL currency_id` |
| `currency_transactions` | `currency_transactions_id_not_null` | `NOT NULL id` |
| `currency_transactions` | `currency_transactions_metadata_not_null` | `NOT NULL metadata` |
| `currency_transactions` | `currency_transactions_pkey` | `PRIMARY KEY (id)` |
| `currency_transactions` | `currency_transactions_reference_key_not_null` | `NOT NULL reference_key` |
| `currency_transactions` | `currency_transactions_reference_type_not_null` | `NOT NULL reference_type` |
| `currency_transactions` | `currency_transactions_transaction_type_not_null` | `NOT NULL transaction_type` |
| `currency_transactions` | `currency_transactions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `currency_transactions` | `currency_transactions_user_id_not_null` | `NOT NULL user_id` |
| `dead_letter_events` | `dead_letter_events_attempts_not_null` | `NOT NULL attempts` |
| `dead_letter_events` | `dead_letter_events_event_id_failure_stage_key` | `UNIQUE (event_id, failure_stage)` |
| `dead_letter_events` | `dead_letter_events_event_id_not_null` | `NOT NULL event_id` |
| `dead_letter_events` | `dead_letter_events_event_type_not_null` | `NOT NULL event_type` |
| `dead_letter_events` | `dead_letter_events_failed_at_not_null` | `NOT NULL failed_at` |
| `dead_letter_events` | `dead_letter_events_failure_stage_not_null` | `NOT NULL failure_stage` |
| `dead_letter_events` | `dead_letter_events_id_not_null` | `NOT NULL id` |
| `dead_letter_events` | `dead_letter_events_last_error_not_null` | `NOT NULL last_error` |
| `dead_letter_events` | `dead_letter_events_payload_not_null` | `NOT NULL payload` |
| `dead_letter_events` | `dead_letter_events_pkey` | `PRIMARY KEY (id)` |
| `dead_letter_events` | `dead_letter_events_subject_not_null` | `NOT NULL subject` |
| `direct_conversations` | `direct_conversations_check` | `CHECK (user_low_id < user_high_id)` |
| `direct_conversations` | `direct_conversations_created_at_not_null` | `NOT NULL created_at` |
| `direct_conversations` | `direct_conversations_id_not_null` | `NOT NULL id` |
| `direct_conversations` | `direct_conversations_pkey` | `PRIMARY KEY (id)` |
| `direct_conversations` | `direct_conversations_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `direct_conversations` | `direct_conversations_public_id_key` | `UNIQUE (public_id)` |
| `direct_conversations` | `direct_conversations_public_id_not_null` | `NOT NULL public_id` |
| `direct_conversations` | `direct_conversations_updated_at_not_null` | `NOT NULL updated_at` |
| `direct_conversations` | `direct_conversations_user_high_id_fkey` | `FOREIGN KEY (user_high_id) REFERENCES users(id) ON DELETE CASCADE` |
| `direct_conversations` | `direct_conversations_user_high_id_not_null` | `NOT NULL user_high_id` |
| `direct_conversations` | `direct_conversations_user_low_id_fkey` | `FOREIGN KEY (user_low_id) REFERENCES users(id) ON DELETE CASCADE` |
| `direct_conversations` | `direct_conversations_user_low_id_not_null` | `NOT NULL user_low_id` |
| `direct_conversations` | `direct_conversations_user_low_id_user_high_id_key` | `UNIQUE (user_low_id, user_high_id)` |
| `direct_messages` | `direct_messages_body_not_null` | `NOT NULL body` |
| `direct_messages` | `direct_messages_conversation_id_fkey` | `FOREIGN KEY (conversation_id) REFERENCES direct_conversations(id) ON DELETE CASCADE` |
| `direct_messages` | `direct_messages_conversation_id_not_null` | `NOT NULL conversation_id` |
| `direct_messages` | `direct_messages_created_at_not_null` | `NOT NULL created_at` |
| `direct_messages` | `direct_messages_id_not_null` | `NOT NULL id` |
| `direct_messages` | `direct_messages_pkey` | `PRIMARY KEY (id)` |
| `direct_messages` | `direct_messages_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `direct_messages` | `direct_messages_public_id_key` | `UNIQUE (public_id)` |
| `direct_messages` | `direct_messages_public_id_not_null` | `NOT NULL public_id` |
| `direct_messages` | `direct_messages_recipient_id_fkey` | `FOREIGN KEY (recipient_id) REFERENCES users(id) ON DELETE CASCADE` |
| `direct_messages` | `direct_messages_recipient_id_not_null` | `NOT NULL recipient_id` |
| `direct_messages` | `direct_messages_sender_id_fkey` | `FOREIGN KEY (sender_id) REFERENCES users(id) ON DELETE CASCADE` |
| `direct_messages` | `direct_messages_sender_id_not_null` | `NOT NULL sender_id` |
| `email_verification_codes` | `email_verification_codes_code_hash_not_null` | `NOT NULL code_hash` |
| `email_verification_codes` | `email_verification_codes_created_at_not_null` | `NOT NULL created_at` |
| `email_verification_codes` | `email_verification_codes_email_not_null` | `NOT NULL email` |
| `email_verification_codes` | `email_verification_codes_expires_at_not_null` | `NOT NULL expires_at` |
| `email_verification_codes` | `email_verification_codes_id_not_null` | `NOT NULL id` |
| `email_verification_codes` | `email_verification_codes_pkey` | `PRIMARY KEY (id)` |
| `email_verification_codes` | `email_verification_codes_purpose_not_null` | `NOT NULL purpose` |
| `email_verification_codes` | `email_verification_codes_request_ip_not_null` | `NOT NULL request_ip` |
| `experience_transactions` | `experience_transactions_amount_delta_not_null` | `NOT NULL amount_delta` |
| `experience_transactions` | `experience_transactions_created_at_not_null` | `NOT NULL created_at` |
| `experience_transactions` | `experience_transactions_experience_after_check` | `CHECK (experience_after >= 0)` |
| `experience_transactions` | `experience_transactions_experience_after_not_null` | `NOT NULL experience_after` |
| `experience_transactions` | `experience_transactions_id_not_null` | `NOT NULL id` |
| `experience_transactions` | `experience_transactions_metadata_not_null` | `NOT NULL metadata` |
| `experience_transactions` | `experience_transactions_pkey` | `PRIMARY KEY (id)` |
| `experience_transactions` | `experience_transactions_reason_not_null` | `NOT NULL reason` |
| `experience_transactions` | `experience_transactions_reference_key_not_null` | `NOT NULL reference_key` |
| `experience_transactions` | `experience_transactions_reference_type_not_null` | `NOT NULL reference_type` |
| `experience_transactions` | `experience_transactions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `experience_transactions` | `experience_transactions_user_id_not_null` | `NOT NULL user_id` |
| `external_release_bindings` | `external_release_bindings_created_at_not_null` | `NOT NULL created_at` |
| `external_release_bindings` | `external_release_bindings_external_body_hash_not_null` | `NOT NULL external_body_hash` |
| `external_release_bindings` | `external_release_bindings_external_release_id_not_null` | `NOT NULL external_release_id` |
| `external_release_bindings` | `external_release_bindings_external_url_not_null` | `NOT NULL external_url` |
| `external_release_bindings` | `external_release_bindings_id_not_null` | `NOT NULL id` |
| `external_release_bindings` | `external_release_bindings_manual_override_not_null` | `NOT NULL manual_override` |
| `external_release_bindings` | `external_release_bindings_metadata_not_null` | `NOT NULL metadata` |
| `external_release_bindings` | `external_release_bindings_pkey` | `PRIMARY KEY (id)` |
| `external_release_bindings` | `external_release_bindings_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `external_release_bindings` | `external_release_bindings_project_route_id_not_null` | `NOT NULL project_route_id` |
| `external_release_bindings` | `external_release_bindings_source_managed_not_null` | `NOT NULL source_managed` |
| `external_release_bindings` | `external_release_bindings_source_type_external_release_id_key` | `UNIQUE (source_type, external_release_id)` |
| `external_release_bindings` | `external_release_bindings_source_type_not_null` | `NOT NULL source_type` |
| `external_release_bindings` | `external_release_bindings_updated_at_not_null` | `NOT NULL updated_at` |
| `favorite_collection_items` | `favorite_collection_items_collection_id_entity_type_entity__key` | `UNIQUE (collection_id, entity_type, entity_id)` |
| `favorite_collection_items` | `favorite_collection_items_collection_id_fkey` | `FOREIGN KEY (collection_id) REFERENCES favorite_collections(id) ON DELETE CASCADE` |
| `favorite_collection_items` | `favorite_collection_items_collection_id_not_null` | `NOT NULL collection_id` |
| `favorite_collection_items` | `favorite_collection_items_created_at_not_null` | `NOT NULL created_at` |
| `favorite_collection_items` | `favorite_collection_items_entity_id_not_null` | `NOT NULL entity_id` |
| `favorite_collection_items` | `favorite_collection_items_entity_type_entity_id_fkey` | `FOREIGN KEY (entity_type, entity_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE CASCADE` |
| `favorite_collection_items` | `favorite_collection_items_entity_type_not_null` | `NOT NULL entity_type` |
| `favorite_collection_items` | `favorite_collection_items_id_not_null` | `NOT NULL id` |
| `favorite_collection_items` | `favorite_collection_items_pkey` | `PRIMARY KEY (id)` |
| `favorite_collections` | `favorite_collections_created_at_not_null` | `NOT NULL created_at` |
| `favorite_collections` | `favorite_collections_id_not_null` | `NOT NULL id` |
| `favorite_collections` | `favorite_collections_is_default_not_null` | `NOT NULL is_default` |
| `favorite_collections` | `favorite_collections_is_public_not_null` | `NOT NULL is_public` |
| `favorite_collections` | `favorite_collections_name_not_null` | `NOT NULL name` |
| `favorite_collections` | `favorite_collections_pkey` | `PRIMARY KEY (id)` |
| `favorite_collections` | `favorite_collections_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `favorite_collections` | `favorite_collections_public_id_key` | `UNIQUE (public_id)` |
| `favorite_collections` | `favorite_collections_public_id_not_null` | `NOT NULL public_id` |
| `favorite_collections` | `favorite_collections_updated_at_not_null` | `NOT NULL updated_at` |
| `favorite_collections` | `favorite_collections_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `favorite_collections` | `favorite_collections_user_id_name_key` | `UNIQUE (user_id, name)` |
| `favorite_collections` | `favorite_collections_user_id_not_null` | `NOT NULL user_id` |
| `favorite_modpack_export_items` | `favorite_modpack_export_ite_source_project_name_snapsh_not_null` | `NOT NULL source_project_name_snapshot` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_created_at_not_null` | `NOT NULL created_at` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_dependency_of_not_null` | `NOT NULL dependency_of` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_download_url_not_null` | `NOT NULL download_url` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_env_client_check` | `CHECK (env_client = ANY (ARRAY[''::text, 'required'::text, 'optional'::text, 'unsupported'::text]))` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_env_client_not_null` | `NOT NULL env_client` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_env_server_check` | `CHECK (env_server = ANY (ARRAY[''::text, 'required'::text, 'optional'::text, 'unsupported'::text]))` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_env_server_not_null` | `NOT NULL env_server` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_file_size_not_null` | `NOT NULL file_size` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_id_not_null` | `NOT NULL id` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_loader_not_null` | `NOT NULL loader` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_modrinth_project_id_not_null` | `NOT NULL modrinth_project_id` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_modrinth_version_id_not_null` | `NOT NULL modrinth_version_id` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_pkey` | `PRIMARY KEY (id)` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_reason_code_not_null` | `NOT NULL reason_code` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_reason_detail_not_null` | `NOT NULL reason_detail` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_release_type_not_null` | `NOT NULL release_type` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_result_type_check` | `CHECK (result_type = ANY (ARRAY['exported'::text, 'auto_dependency'::text, 'skipped'::text, 'failed'::text]))` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_result_type_not_null` | `NOT NULL result_type` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_selected_file_name_not_null` | `NOT NULL selected_file_name` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_selected_version_name_not_null` | `NOT NULL selected_version_name` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_sha1_not_null` | `NOT NULL sha1` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_sha512_not_null` | `NOT NULL sha512` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_source_project_route_id_fkey` | `FOREIGN KEY (source_project_route_id) REFERENCES public_routes(id) ON DELETE SET NULL` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_source_project_type_not_null` | `NOT NULL source_project_type` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_task_id_fkey` | `FOREIGN KEY (task_id) REFERENCES favorite_modpack_export_tasks(id) ON DELETE CASCADE` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_task_id_not_null` | `NOT NULL task_id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_allow_compatible_only_not_null` | `NOT NULL allow_compatible_only` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_attempt_count_not_null` | `NOT NULL attempt_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_auto_dependency_count_not_null` | `NOT NULL auto_dependency_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_check` | `CHECK (pack_version_id <> minecraft_version)` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_collection_id_fkey` | `FOREIGN KEY (collection_id) REFERENCES favorite_collections(id) ON DELETE CASCADE` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_collection_id_not_null` | `NOT NULL collection_id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_collection_item_count_not_null` | `NOT NULL collection_item_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_created_at_not_null` | `NOT NULL created_at` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_error_code_not_null` | `NOT NULL error_code` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_error_detail_not_null` | `NOT NULL error_detail` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_exported_mod_count_not_null` | `NOT NULL exported_mod_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_failed_item_count_not_null` | `NOT NULL failed_item_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_final_file_count_not_null` | `NOT NULL final_file_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_id_not_null` | `NOT NULL id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_lease_token_not_null` | `NOT NULL lease_token` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_loader_type_check` | `CHECK (loader_type = ANY (ARRAY['neoforge'::text, 'fabric'::text, 'forge'::text]))` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_loader_type_not_null` | `NOT NULL loader_type` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_loader_version_not_null` | `NOT NULL loader_version` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_owner_user_id_fkey` | `FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_owner_user_id_not_null` | `NOT NULL owner_user_id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_pack_name_not_null` | `NOT NULL pack_name` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_pack_version_id_not_null` | `NOT NULL pack_version_id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_pkey` | `PRIMARY KEY (id)` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_public_id_key` | `UNIQUE (public_id)` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_public_id_not_null` | `NOT NULL public_id` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_report_snapshot_not_null` | `NOT NULL report_snapshot` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_report_version_not_null` | `NOT NULL report_version` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_result_file_id_fkey` | `FOREIGN KEY (result_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_result_file_size_not_null` | `NOT NULL result_file_size` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_result_sha256_not_null` | `NOT NULL result_sha256` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_skipped_item_count_not_null` | `NOT NULL skipped_item_count` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_stage_not_null` | `NOT NULL stage` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'processing'::text, 'ready'::text, 'failed'::text, 'expired'::text, 'cancelled'::text]))` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_status_not_null` | `NOT NULL status` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_updated_at_not_null` | `NOT NULL updated_at` |
| `game_resource_aliases` | `game_resource_aliases_alias_id_not_null` | `NOT NULL alias_id` |
| `game_resource_aliases` | `game_resource_aliases_created_at_not_null` | `NOT NULL created_at` |
| `game_resource_aliases` | `game_resource_aliases_kind_code_fkey` | `FOREIGN KEY (kind_code) REFERENCES resource_kinds(code)` |
| `game_resource_aliases` | `game_resource_aliases_kind_code_not_null` | `NOT NULL kind_code` |
| `game_resource_aliases` | `game_resource_aliases_pkey` | `PRIMARY KEY (kind_code, alias_id)` |
| `game_resource_aliases` | `game_resource_aliases_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE CASCADE` |
| `game_resource_aliases` | `game_resource_aliases_resource_id_not_null` | `NOT NULL resource_id` |
| `game_resource_aliases` | `game_resource_aliases_source_not_null` | `NOT NULL source` |
| `game_resource_aliases` | `game_resource_aliases_updated_at_not_null` | `NOT NULL updated_at` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_block_resource_id_fkey` | `FOREIGN KEY (block_resource_id) REFERENCES game_resources(entity_id) ON DELETE SET NULL` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_blockstate_path_not_null` | `NOT NULL blockstate_path` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_item_model_path_not_null` | `NOT NULL item_model_path` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_item_resource_id_fkey` | `FOREIGN KEY (item_resource_id) REFERENCES game_resources(entity_id) ON DELETE SET NULL` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_model_paths_not_null` | `NOT NULL model_paths` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_pkey` | `PRIMARY KEY (snapshot_id)` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_snapshot_id_fkey` | `FOREIGN KEY (snapshot_id) REFERENCES resource_import_snapshots(id) ON DELETE CASCADE` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_snapshot_id_not_null` | `NOT NULL snapshot_id` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_texture_paths_not_null` | `NOT NULL texture_paths` |
| `game_resources` | `game_resources_canonical_id_not_null` | `NOT NULL canonical_id` |
| `game_resources` | `game_resources_created_at_not_null` | `NOT NULL created_at` |
| `game_resources` | `game_resources_created_from_revision_id_fkey` | `FOREIGN KEY (created_from_revision_id) REFERENCES catalog_import_revisions(id) ON DELETE SET NULL` |
| `game_resources` | `game_resources_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `game_resources` | `game_resources_entity_id_not_null` | `NOT NULL entity_id` |
| `game_resources` | `game_resources_kind_code_canonical_id_key` | `UNIQUE (kind_code, canonical_id)` |
| `game_resources` | `game_resources_kind_code_fkey` | `FOREIGN KEY (kind_code) REFERENCES resource_kinds(code)` |
| `game_resources` | `game_resources_kind_code_not_null` | `NOT NULL kind_code` |
| `game_resources` | `game_resources_namespace_not_null` | `NOT NULL namespace` |
| `game_resources` | `game_resources_owner_mod_id_fkey` | `FOREIGN KEY (owner_mod_id) REFERENCES mods(id) ON DELETE SET NULL` |
| `game_resources` | `game_resources_pkey` | `PRIMARY KEY (entity_id)` |
| `game_resources` | `game_resources_resolved_not_null` | `NOT NULL resolved` |
| `game_resources` | `game_resources_resource_path_not_null` | `NOT NULL resource_path` |
| `game_resources` | `game_resources_updated_at_not_null` | `NOT NULL updated_at` |
| `knowledge_pages` | `fk_knowledge_pages_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `knowledge_pages` | `knowledge_pages_content_markdown_not_null` | `NOT NULL content_markdown` |
| `knowledge_pages` | `knowledge_pages_created_at_not_null` | `NOT NULL created_at` |
| `knowledge_pages` | `knowledge_pages_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `knowledge_pages` | `knowledge_pages_entity_id_not_null` | `NOT NULL entity_id` |
| `knowledge_pages` | `knowledge_pages_locale_not_null` | `NOT NULL locale` |
| `knowledge_pages` | `knowledge_pages_pkey` | `PRIMARY KEY (entity_id, locale)` |
| `knowledge_pages` | `knowledge_pages_updated_at_not_null` | `NOT NULL updated_at` |
| `knowledge_pages` | `knowledge_pages_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `level_system_config` | `level_system_config_level_thresholds_not_null` | `NOT NULL level_thresholds` |
| `level_system_config` | `level_system_config_pkey` | `PRIMARY KEY (singleton)` |
| `level_system_config` | `level_system_config_role_track_code_fkey` | `FOREIGN KEY (role_track_code) REFERENCES permission_role_tracks(code) ON DELETE SET NULL` |
| `level_system_config` | `level_system_config_singleton_check` | `CHECK (singleton)` |
| `level_system_config` | `level_system_config_singleton_not_null` | `NOT NULL singleton` |
| `level_system_config` | `level_system_config_updated_at_not_null` | `NOT NULL updated_at` |
| `level_system_config` | `level_system_config_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `license_policies` | `license_policies_notes_not_null` | `NOT NULL notes` |
| `license_policies` | `license_policies_pkey` | `PRIMARY KEY (spdx_id)` |
| `license_policies` | `license_policies_redistribution_allowed_not_null` | `NOT NULL redistribution_allowed` |
| `license_policies` | `license_policies_spdx_id_not_null` | `NOT NULL spdx_id` |
| `license_policies` | `license_policies_updated_at_not_null` | `NOT NULL updated_at` |
| `license_policies` | `license_policies_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `log_share_entries` | `log_share_entries_byte_size_check` | `CHECK (byte_size >= 0)` |
| `log_share_entries` | `log_share_entries_byte_size_not_null` | `NOT NULL byte_size` |
| `log_share_entries` | `log_share_entries_check` | `CHECK ((sanitized_text IS NOT NULL) <> (sanitized_object_key <> ''::text))` |
| `log_share_entries` | `log_share_entries_checksum_not_null` | `NOT NULL checksum` |
| `log_share_entries` | `log_share_entries_content_type_not_null` | `NOT NULL content_type` |
| `log_share_entries` | `log_share_entries_entry_index_check` | `CHECK (entry_index >= 0)` |
| `log_share_entries` | `log_share_entries_entry_index_not_null` | `NOT NULL entry_index` |
| `log_share_entries` | `log_share_entries_id_not_null` | `NOT NULL id` |
| `log_share_entries` | `log_share_entries_line_count_check` | `CHECK (line_count >= 0)` |
| `log_share_entries` | `log_share_entries_line_count_not_null` | `NOT NULL line_count` |
| `log_share_entries` | `log_share_entries_log_share_id_entry_index_key` | `UNIQUE (log_share_id, entry_index)` |
| `log_share_entries` | `log_share_entries_log_share_id_fkey` | `FOREIGN KEY (log_share_id) REFERENCES log_shares(id) ON DELETE CASCADE` |
| `log_share_entries` | `log_share_entries_log_share_id_not_null` | `NOT NULL log_share_id` |
| `log_share_entries` | `log_share_entries_original_name_not_null` | `NOT NULL original_name` |
| `log_share_entries` | `log_share_entries_pkey` | `PRIMARY KEY (id)` |
| `log_share_entries` | `log_share_entries_safe_display_name_not_null` | `NOT NULL safe_display_name` |
| `log_share_entries` | `log_share_entries_sanitized_object_key_not_null` | `NOT NULL sanitized_object_key` |
| `log_share_entries` | `log_share_entries_status_check` | `CHECK (status = ANY (ARRAY['processing'::text, 'ready'::text, 'failed'::text, 'removed'::text]))` |
| `log_share_entries` | `log_share_entries_status_not_null` | `NOT NULL status` |
| `log_shares` | `log_shares_check` | `CHECK (source_type = 'file'::text AND owner_user_id IS NOT NULL OR source_type = 'paste'::text AND source_file_id IS NULL)` |
| `log_shares` | `log_shares_created_at_not_null` | `NOT NULL created_at` |
| `log_shares` | `log_shares_expires_at_not_null` | `NOT NULL expires_at` |
| `log_shares` | `log_shares_id_not_null` | `NOT NULL id` |
| `log_shares` | `log_shares_original_name_not_null` | `NOT NULL original_name` |
| `log_shares` | `log_shares_owner_user_id_fkey` | `FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `log_shares` | `log_shares_pkey` | `PRIMARY KEY (id)` |
| `log_shares` | `log_shares_public_code_check` | `CHECK (public_code ~ '^[A-Za-z0-9_-]{12,32}$'::text)` |
| `log_shares` | `log_shares_public_code_key` | `UNIQUE (public_code)` |
| `log_shares` | `log_shares_public_code_not_null` | `NOT NULL public_code` |
| `log_shares` | `log_shares_redaction_counts_not_null` | `NOT NULL redaction_counts` |
| `log_shares` | `log_shares_redaction_version_not_null` | `NOT NULL redaction_version` |
| `log_shares` | `log_shares_source_file_id_fkey` | `FOREIGN KEY (source_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `log_shares` | `log_shares_source_type_check` | `CHECK (source_type = ANY (ARRAY['file'::text, 'paste'::text]))` |
| `log_shares` | `log_shares_source_type_not_null` | `NOT NULL source_type` |
| `log_shares` | `log_shares_status_check` | `CHECK (status = ANY (ARRAY['processing'::text, 'ready'::text, 'failed'::text, 'expired'::text, 'deleted'::text, 'source_deleted'::text]))` |
| `log_shares` | `log_shares_status_not_null` | `NOT NULL status` |
| `log_shares` | `log_shares_title_not_null` | `NOT NULL title` |
| `markdown_playground_drafts` | `markdown_playground_drafts_content_not_null` | `NOT NULL content` |
| `markdown_playground_drafts` | `markdown_playground_drafts_pkey` | `PRIMARY KEY (user_id)` |
| `markdown_playground_drafts` | `markdown_playground_drafts_updated_at_not_null` | `NOT NULL updated_at` |
| `markdown_playground_drafts` | `markdown_playground_drafts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `markdown_playground_drafts` | `markdown_playground_drafts_user_id_not_null` | `NOT NULL user_id` |
| `minecraft_server_links` | `minecraft_server_links_display_order_not_null` | `NOT NULL display_order` |
| `minecraft_server_links` | `minecraft_server_links_id_not_null` | `NOT NULL id` |
| `minecraft_server_links` | `minecraft_server_links_kind_check` | `CHECK (kind = ANY (ARRAY['website'::text, 'forum'::text, 'discord'::text, 'qq'::text, 'bilibili'::text, 'other'::text]))` |
| `minecraft_server_links` | `minecraft_server_links_kind_not_null` | `NOT NULL kind` |
| `minecraft_server_links` | `minecraft_server_links_label_not_null` | `NOT NULL label` |
| `minecraft_server_links` | `minecraft_server_links_pkey` | `PRIMARY KEY (id)` |
| `minecraft_server_links` | `minecraft_server_links_server_id_fkey` | `FOREIGN KEY (server_id) REFERENCES minecraft_servers(id) ON DELETE CASCADE` |
| `minecraft_server_links` | `minecraft_server_links_server_id_not_null` | `NOT NULL server_id` |
| `minecraft_server_links` | `minecraft_server_links_url_check` | `CHECK (char_length(url) >= 1 AND char_length(url) <= 2048)` |
| `minecraft_server_links` | `minecraft_server_links_url_not_null` | `NOT NULL url` |
| `minecraft_server_mods` | `minecraft_server_mods_confidence_check` | `CHECK (confidence = ANY (ARRAY['exact'::text, 'high'::text, 'inferred'::text, 'declared'::text]))` |
| `minecraft_server_mods` | `minecraft_server_mods_confidence_not_null` | `NOT NULL confidence` |
| `minecraft_server_mods` | `minecraft_server_mods_created_at_not_null` | `NOT NULL created_at` |
| `minecraft_server_mods` | `minecraft_server_mods_id_not_null` | `NOT NULL id` |
| `minecraft_server_mods` | `minecraft_server_mods_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE SET NULL` |
| `minecraft_server_mods` | `minecraft_server_mods_pkey` | `PRIMARY KEY (id)` |
| `minecraft_server_mods` | `minecraft_server_mods_raw_mod_id_not_null` | `NOT NULL raw_mod_id` |
| `minecraft_server_mods` | `minecraft_server_mods_server_id_fkey` | `FOREIGN KEY (server_id) REFERENCES minecraft_servers(id) ON DELETE CASCADE` |
| `minecraft_server_mods` | `minecraft_server_mods_server_id_not_null` | `NOT NULL server_id` |
| `minecraft_server_mods` | `minecraft_server_mods_server_id_raw_mod_id_key` | `UNIQUE (server_id, raw_mod_id)` |
| `minecraft_server_mods` | `minecraft_server_mods_source_check` | `CHECK (source = ANY (ARRAY['forge_status'::text, 'configuration'::text, 'agent'::text, 'manual'::text]))` |
| `minecraft_server_mods` | `minecraft_server_mods_source_not_null` | `NOT NULL source` |
| `minecraft_server_mods` | `minecraft_server_mods_version_not_null` | `NOT NULL version` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_created_at_not_null` | `NOT NULL created_at` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_display_order_not_null` | `NOT NULL display_order` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_pkey` | `PRIMARY KEY (server_id, oss_file_id)` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_server_id_fkey` | `FOREIGN KEY (server_id) REFERENCES minecraft_servers(id) ON DELETE CASCADE` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_server_id_not_null` | `NOT NULL server_id` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_checked_at_not_null` | `NOT NULL checked_at` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_error_not_null` | `NOT NULL error` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_id_not_null` | `NOT NULL id` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_latency_ms_check` | `CHECK (latency_ms IS NULL OR latency_ms >= 0)` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_online_not_null` | `NOT NULL online` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_pkey` | `PRIMARY KEY (id)` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_players_max_check` | `CHECK (players_max IS NULL OR players_max >= 0)` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_players_online_check` | `CHECK (players_online IS NULL OR players_online >= 0)` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_server_id_fkey` | `FOREIGN KEY (server_id) REFERENCES minecraft_servers(id) ON DELETE CASCADE` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_server_id_not_null` | `NOT NULL server_id` |
| `minecraft_servers` | `minecraft_servers_address_not_null` | `NOT NULL address` |
| `minecraft_servers` | `minecraft_servers_body_markdown_check` | `CHECK (char_length(body_markdown) <= 100000)` |
| `minecraft_servers` | `minecraft_servers_body_markdown_not_null` | `NOT NULL body_markdown` |
| `minecraft_servers` | `minecraft_servers_connect_host_not_null` | `NOT NULL connect_host` |
| `minecraft_servers` | `minecraft_servers_connect_port_check` | `CHECK (connect_port >= 1 AND connect_port <= 65535)` |
| `minecraft_servers` | `minecraft_servers_connect_port_not_null` | `NOT NULL connect_port` |
| `minecraft_servers` | `minecraft_servers_created_at_not_null` | `NOT NULL created_at` |
| `minecraft_servers` | `minecraft_servers_dedicated_client_not_null` | `NOT NULL dedicated_client` |
| `minecraft_servers` | `minecraft_servers_handshake_host_not_null` | `NOT NULL handshake_host` |
| `minecraft_servers` | `minecraft_servers_has_whitelist_not_null` | `NOT NULL has_whitelist` |
| `minecraft_servers` | `minecraft_servers_icon_data_uri_check` | `CHECK (char_length(icon_data_uri) <= 1048576)` |
| `minecraft_servers` | `minecraft_servers_icon_data_uri_not_null` | `NOT NULL icon_data_uri` |
| `minecraft_servers` | `minecraft_servers_id_not_null` | `NOT NULL id` |
| `minecraft_servers` | `minecraft_servers_languages_not_null` | `NOT NULL languages` |
| `minecraft_servers` | `minecraft_servers_last_error_not_null` | `NOT NULL last_error` |
| `minecraft_servers` | `minecraft_servers_last_latency_ms_check` | `CHECK (last_latency_ms IS NULL OR last_latency_ms >= 0)` |
| `minecraft_servers` | `minecraft_servers_last_minecraft_version_not_null` | `NOT NULL last_minecraft_version` |
| `minecraft_servers` | `minecraft_servers_last_motd_not_null` | `NOT NULL last_motd` |
| `minecraft_servers` | `minecraft_servers_last_online_not_null` | `NOT NULL last_online` |
| `minecraft_servers` | `minecraft_servers_last_players_max_check` | `CHECK (last_players_max >= 0)` |
| `minecraft_servers` | `minecraft_servers_last_players_max_not_null` | `NOT NULL last_players_max` |
| `minecraft_servers` | `minecraft_servers_last_players_online_check` | `CHECK (last_players_online >= 0)` |
| `minecraft_servers` | `minecraft_servers_last_players_online_not_null` | `NOT NULL last_players_online` |
| `minecraft_servers` | `minecraft_servers_loader_not_null` | `NOT NULL loader` |
| `minecraft_servers` | `minecraft_servers_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `minecraft_servers` | `minecraft_servers_mod_list_complete_not_null` | `NOT NULL mod_list_complete` |
| `minecraft_servers` | `minecraft_servers_modded_not_null` | `NOT NULL modded` |
| `minecraft_servers` | `minecraft_servers_name_check` | `CHECK (char_length(name) >= 1 AND char_length(name) <= 80)` |
| `minecraft_servers` | `minecraft_servers_name_not_null` | `NOT NULL name` |
| `minecraft_servers` | `minecraft_servers_next_probe_at_not_null` | `NOT NULL next_probe_at` |
| `minecraft_servers` | `minecraft_servers_normalized_address_not_null` | `NOT NULL normalized_address` |
| `minecraft_servers` | `minecraft_servers_online_mode_not_null` | `NOT NULL online_mode` |
| `minecraft_servers` | `minecraft_servers_pkey` | `PRIMARY KEY (id)` |
| `minecraft_servers` | `minecraft_servers_primary_tag_check` | `CHECK (primary_tag = ANY (ARRAY['survival'::text, 'casual'::text, 'adventure'::text, 'creative'::text, 'war'::text, 'rpg'::text, 'minigame'::text, 'technology'::text]))` |
| `minecraft_servers` | `minecraft_servers_primary_tag_not_null` | `NOT NULL primary_tag` |
| `minecraft_servers` | `minecraft_servers_proof_text_check` | `CHECK (char_length(proof_text) <= 10000)` |
| `minecraft_servers` | `minecraft_servers_proof_text_not_null` | `NOT NULL proof_text` |
| `minecraft_servers` | `minecraft_servers_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `minecraft_servers` | `minecraft_servers_public_id_key` | `UNIQUE (public_id)` |
| `minecraft_servers` | `minecraft_servers_public_id_not_null` | `NOT NULL public_id` |
| `minecraft_servers` | `minecraft_servers_review_note_not_null` | `NOT NULL review_note` |
| `minecraft_servers` | `minecraft_servers_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `minecraft_servers` | `minecraft_servers_review_status_not_null` | `NOT NULL review_status` |
| `minecraft_servers` | `minecraft_servers_reviewed_by_fkey` | `FOREIGN KEY (reviewed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `minecraft_servers` | `minecraft_servers_short_description_check` | `CHECK (char_length(short_description) <= 240)` |
| `minecraft_servers` | `minecraft_servers_short_description_not_null` | `NOT NULL short_description` |
| `minecraft_servers` | `minecraft_servers_slug_key` | `UNIQUE (slug)` |
| `minecraft_servers` | `minecraft_servers_slug_not_null` | `NOT NULL slug` |
| `minecraft_servers` | `minecraft_servers_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE RESTRICT` |
| `minecraft_servers` | `minecraft_servers_submitted_by_not_null` | `NOT NULL submitted_by` |
| `minecraft_servers` | `minecraft_servers_updated_at_not_null` | `NOT NULL updated_at` |
| `mirrored_project_files` | `mirrored_project_files_byte_size_check` | `CHECK (byte_size > 0)` |
| `mirrored_project_files` | `mirrored_project_files_byte_size_not_null` | `NOT NULL byte_size` |
| `mirrored_project_files` | `mirrored_project_files_created_at_not_null` | `NOT NULL created_at` |
| `mirrored_project_files` | `mirrored_project_files_external_file_id_not_null` | `NOT NULL external_file_id` |
| `mirrored_project_files` | `mirrored_project_files_file_sha256_not_null` | `NOT NULL file_sha256` |
| `mirrored_project_files` | `mirrored_project_files_id_not_null` | `NOT NULL id` |
| `mirrored_project_files` | `mirrored_project_files_metadata_not_null` | `NOT NULL metadata` |
| `mirrored_project_files` | `mirrored_project_files_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `mirrored_project_files` | `mirrored_project_files_pkey` | `PRIMARY KEY (id)` |
| `mirrored_project_files` | `mirrored_project_files_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `mirrored_project_files` | `mirrored_project_files_project_route_id_not_null` | `NOT NULL project_route_id` |
| `mirrored_project_files` | `mirrored_project_files_source_type_external_file_id_key` | `UNIQUE (source_type, external_file_id)` |
| `mirrored_project_files` | `mirrored_project_files_source_type_file_sha256_byte_size_key` | `UNIQUE (source_type, file_sha256, byte_size)` |
| `mirrored_project_files` | `mirrored_project_files_source_type_not_null` | `NOT NULL source_type` |
| `mirrored_project_files` | `mirrored_project_files_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'scanning'::text, 'ready'::text, 'review'::text, 'failed'::text, 'source_changed'::text]))` |
| `mirrored_project_files` | `mirrored_project_files_status_not_null` | `NOT NULL status` |
| `mod_content_section_localizations` | `mod_content_section_localizations_description_not_null` | `NOT NULL description` |
| `mod_content_section_localizations` | `mod_content_section_localizations_locale_check` | `CHECK (locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'::text)` |
| `mod_content_section_localizations` | `mod_content_section_localizations_locale_not_null` | `NOT NULL locale` |
| `mod_content_section_localizations` | `mod_content_section_localizations_name_not_null` | `NOT NULL name` |
| `mod_content_section_localizations` | `mod_content_section_localizations_pkey` | `PRIMARY KEY (section_id, locale)` |
| `mod_content_section_localizations` | `mod_content_section_localizations_section_id_fkey` | `FOREIGN KEY (section_id) REFERENCES mod_content_sections(id) ON DELETE CASCADE` |
| `mod_content_section_localizations` | `mod_content_section_localizations_section_id_not_null` | `NOT NULL section_id` |
| `mod_content_section_resources` | `mod_content_section_resources_created_at_not_null` | `NOT NULL created_at` |
| `mod_content_section_resources` | `mod_content_section_resources_ordinal_check` | `CHECK (ordinal >= 0)` |
| `mod_content_section_resources` | `mod_content_section_resources_ordinal_not_null` | `NOT NULL ordinal` |
| `mod_content_section_resources` | `mod_content_section_resources_pkey` | `PRIMARY KEY (section_id, version_id, resource_id)` |
| `mod_content_section_resources` | `mod_content_section_resources_placement_identity_key_not_null` | `NOT NULL placement_identity_key` |
| `mod_content_section_resources` | `mod_content_section_resources_placement_source_check` | `CHECK (placement_source = ANY (ARRAY['manual'::text, 'import'::text]))` |
| `mod_content_section_resources` | `mod_content_section_resources_placement_source_not_null` | `NOT NULL placement_source` |
| `mod_content_section_resources` | `mod_content_section_resources_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE RESTRICT` |
| `mod_content_section_resources` | `mod_content_section_resources_resource_id_not_null` | `NOT NULL resource_id` |
| `mod_content_section_resources` | `mod_content_section_resources_section_id_not_null` | `NOT NULL section_id` |
| `mod_content_section_resources` | `mod_content_section_resources_section_id_version_id_fkey` | `FOREIGN KEY (section_id, version_id) REFERENCES mod_content_sections(id, version_id) ON DELETE CASCADE` |
| `mod_content_section_resources` | `mod_content_section_resources_section_id_version_id_ordinal_key` | `UNIQUE (section_id, version_id, ordinal)` |
| `mod_content_section_resources` | `mod_content_section_resources_similar_group_id_check` | `CHECK (similar_group_id = ''::text OR similar_group_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_content_section_resources` | `mod_content_section_resources_similar_group_id_not_null` | `NOT NULL similar_group_id` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_fkey` | `FOREIGN KEY (version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_not_null` | `NOT NULL version_id` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_placement_identity_key` | `UNIQUE (version_id, placement_identity_key)` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_resource_id_key` | `UNIQUE (version_id, resource_id)` |
| `mod_content_sections` | `mod_content_sections_created_at_not_null` | `NOT NULL created_at` |
| `mod_content_sections` | `mod_content_sections_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_content_sections` | `mod_content_sections_default_locale_not_null` | `NOT NULL default_locale` |
| `mod_content_sections` | `mod_content_sections_definition_override_check` | `CHECK (definition_override IS NULL OR jsonb_typeof(definition_override) = 'object'::text)` |
| `mod_content_sections` | `mod_content_sections_definition_version_check` | `CHECK (definition_version > 0)` |
| `mod_content_sections` | `mod_content_sections_definition_version_not_null` | `NOT NULL definition_version` |
| `mod_content_sections` | `mod_content_sections_display_mode_check` | `CHECK (display_mode = ANY (ARRAY['compact'::text, 'large'::text]))` |
| `mod_content_sections` | `mod_content_sections_display_mode_not_null` | `NOT NULL display_mode` |
| `mod_content_sections` | `mod_content_sections_id_not_null` | `NOT NULL id` |
| `mod_content_sections` | `mod_content_sections_id_version_id_key` | `UNIQUE (id, version_id)` |
| `mod_content_sections` | `mod_content_sections_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_content_sections` | `mod_content_sections_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_content_sections` | `mod_content_sections_ordinal_check` | `CHECK (ordinal >= 0)` |
| `mod_content_sections` | `mod_content_sections_ordinal_not_null` | `NOT NULL ordinal` |
| `mod_content_sections` | `mod_content_sections_parent_id_fkey` | `FOREIGN KEY (parent_id) REFERENCES mod_content_sections(id) ON DELETE CASCADE` |
| `mod_content_sections` | `mod_content_sections_pkey` | `PRIMARY KEY (id)` |
| `mod_content_sections` | `mod_content_sections_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_content_sections` | `mod_content_sections_public_id_key` | `UNIQUE (public_id)` |
| `mod_content_sections` | `mod_content_sections_public_id_not_null` | `NOT NULL public_id` |
| `mod_content_sections` | `mod_content_sections_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mod_content_sections` | `mod_content_sections_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'pending'::text, 'archived'::text]))` |
| `mod_content_sections` | `mod_content_sections_status_not_null` | `NOT NULL status` |
| `mod_content_sections` | `mod_content_sections_system_key_not_null` | `NOT NULL system_key` |
| `mod_content_sections` | `mod_content_sections_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES mod_content_templates(id) ON DELETE RESTRICT` |
| `mod_content_sections` | `mod_content_sections_template_id_not_null` | `NOT NULL template_id` |
| `mod_content_sections` | `mod_content_sections_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_content_sections` | `mod_content_sections_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_content_sections` | `mod_content_sections_version_id_fkey` | `FOREIGN KEY (version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `mod_content_sections` | `mod_content_sections_version_id_not_null` | `NOT NULL version_id` |
| `mod_content_sections` | `mod_content_sections_version_id_parent_id_ordinal_key` | `UNIQUE (version_id, parent_id, ordinal)` |
| `mod_content_template_localizations` | `mod_content_template_localizations_description_not_null` | `NOT NULL description` |
| `mod_content_template_localizations` | `mod_content_template_localizations_locale_check` | `CHECK (locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'::text)` |
| `mod_content_template_localizations` | `mod_content_template_localizations_locale_not_null` | `NOT NULL locale` |
| `mod_content_template_localizations` | `mod_content_template_localizations_name_not_null` | `NOT NULL name` |
| `mod_content_template_localizations` | `mod_content_template_localizations_pkey` | `PRIMARY KEY (template_id, locale)` |
| `mod_content_template_localizations` | `mod_content_template_localizations_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES mod_content_templates(id) ON DELETE CASCADE` |
| `mod_content_template_localizations` | `mod_content_template_localizations_template_id_not_null` | `NOT NULL template_id` |
| `mod_content_templates` | `mod_content_templates_builtin_not_null` | `NOT NULL builtin` |
| `mod_content_templates` | `mod_content_templates_code_check` | `CHECK (code ~ '^[a-z][a-z0-9_]{1,63}$'::text)` |
| `mod_content_templates` | `mod_content_templates_code_not_null` | `NOT NULL code` |
| `mod_content_templates` | `mod_content_templates_created_at_not_null` | `NOT NULL created_at` |
| `mod_content_templates` | `mod_content_templates_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_content_templates` | `mod_content_templates_default_display_mode_check` | `CHECK (default_display_mode = ANY (ARRAY['compact'::text, 'large'::text]))` |
| `mod_content_templates` | `mod_content_templates_default_display_mode_not_null` | `NOT NULL default_display_mode` |
| `mod_content_templates` | `mod_content_templates_default_locale_not_null` | `NOT NULL default_locale` |
| `mod_content_templates` | `mod_content_templates_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `mod_content_templates` | `mod_content_templates_definition_not_null` | `NOT NULL definition` |
| `mod_content_templates` | `mod_content_templates_i18n_key_not_null` | `NOT NULL i18n_key` |
| `mod_content_templates` | `mod_content_templates_id_not_null` | `NOT NULL id` |
| `mod_content_templates` | `mod_content_templates_owner_mod_id_fkey` | `FOREIGN KEY (owner_mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_content_templates` | `mod_content_templates_pkey` | `PRIMARY KEY (id)` |
| `mod_content_templates` | `mod_content_templates_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_content_templates` | `mod_content_templates_public_id_key` | `UNIQUE (public_id)` |
| `mod_content_templates` | `mod_content_templates_public_id_not_null` | `NOT NULL public_id` |
| `mod_content_templates` | `mod_content_templates_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mod_content_templates` | `mod_content_templates_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'pending'::text, 'archived'::text]))` |
| `mod_content_templates` | `mod_content_templates_status_not_null` | `NOT NULL status` |
| `mod_content_templates` | `mod_content_templates_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_content_versions` | `mod_content_versions_created_at_not_null` | `NOT NULL created_at` |
| `mod_content_versions` | `mod_content_versions_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_content_versions` | `mod_content_versions_id_not_null` | `NOT NULL id` |
| `mod_content_versions` | `mod_content_versions_label_not_null` | `NOT NULL label` |
| `mod_content_versions` | `mod_content_versions_loaders_not_null` | `NOT NULL loaders` |
| `mod_content_versions` | `mod_content_versions_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `mod_content_versions` | `mod_content_versions_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_content_versions` | `mod_content_versions_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_content_versions` | `mod_content_versions_mod_version_not_null` | `NOT NULL mod_version` |
| `mod_content_versions` | `mod_content_versions_pkey` | `PRIMARY KEY (id)` |
| `mod_content_versions` | `mod_content_versions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_content_versions` | `mod_content_versions_public_id_key` | `UNIQUE (public_id)` |
| `mod_content_versions` | `mod_content_versions_public_id_not_null` | `NOT NULL public_id` |
| `mod_content_versions` | `mod_content_versions_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mod_content_versions` | `mod_content_versions_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'pending'::text, 'superseded'::text, 'archived'::text]))` |
| `mod_content_versions` | `mod_content_versions_status_not_null` | `NOT NULL status` |
| `mod_content_versions` | `mod_content_versions_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_content_versions` | `mod_content_versions_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_gallery_images` | `fk_mod_gallery_images_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mod_gallery_images` | `mod_gallery_images_created_at_not_null` | `NOT NULL created_at` |
| `mod_gallery_images` | `mod_gallery_images_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_gallery_images` | `mod_gallery_images_display_order_not_null` | `NOT NULL display_order` |
| `mod_gallery_images` | `mod_gallery_images_id_not_null` | `NOT NULL id` |
| `mod_gallery_images` | `mod_gallery_images_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_gallery_images` | `mod_gallery_images_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_gallery_images` | `mod_gallery_images_mod_id_oss_file_id_key` | `UNIQUE (mod_id, oss_file_id)` |
| `mod_gallery_images` | `mod_gallery_images_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `mod_gallery_images` | `mod_gallery_images_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `mod_gallery_images` | `mod_gallery_images_pkey` | `PRIMARY KEY (id)` |
| `mod_gallery_images` | `mod_gallery_images_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_gallery_images` | `mod_gallery_images_public_id_key` | `UNIQUE (public_id)` |
| `mod_gallery_images` | `mod_gallery_images_public_id_not_null` | `NOT NULL public_id` |
| `mod_identifiers` | `mod_identifiers_created_at_not_null` | `NOT NULL created_at` |
| `mod_identifiers` | `mod_identifiers_display_order_not_null` | `NOT NULL display_order` |
| `mod_identifiers` | `mod_identifiers_id_not_null` | `NOT NULL id` |
| `mod_identifiers` | `mod_identifiers_identifier_check` | `CHECK (identifier ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$'::text)` |
| `mod_identifiers` | `mod_identifiers_identifier_not_null` | `NOT NULL identifier` |
| `mod_identifiers` | `mod_identifiers_is_primary_not_null` | `NOT NULL is_primary` |
| `mod_identifiers` | `mod_identifiers_minecraft_version_max_not_null` | `NOT NULL minecraft_version_max` |
| `mod_identifiers` | `mod_identifiers_minecraft_version_min_not_null` | `NOT NULL minecraft_version_min` |
| `mod_identifiers` | `mod_identifiers_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `mod_identifiers` | `mod_identifiers_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_identifiers` | `mod_identifiers_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_identifiers` | `mod_identifiers_pkey` | `PRIMARY KEY (id)` |
| `mod_identifiers` | `mod_identifiers_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_links` | `mod_links_created_at_not_null` | `NOT NULL created_at` |
| `mod_links` | `mod_links_display_order_not_null` | `NOT NULL display_order` |
| `mod_links` | `mod_links_id_not_null` | `NOT NULL id` |
| `mod_links` | `mod_links_link_type_not_null` | `NOT NULL link_type` |
| `mod_links` | `mod_links_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_links` | `mod_links_mod_id_link_type_url_key` | `UNIQUE (mod_id, link_type, url)` |
| `mod_links` | `mod_links_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_links` | `mod_links_note_not_null` | `NOT NULL note` |
| `mod_links` | `mod_links_pkey` | `PRIMARY KEY (id)` |
| `mod_links` | `mod_links_url_not_null` | `NOT NULL url` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_created_at_not_null` | `NOT NULL created_at` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_loader_not_null` | `NOT NULL loader` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_pkey` | `PRIMARY KEY (mod_id, loader, minecraft_version)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_created_at_not_null` | `NOT NULL created_at` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_error_not_null` | `NOT NULL error` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_id_not_null` | `NOT NULL id` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_pkey` | `PRIMARY KEY (id)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_progress_check` | `CHECK (progress >= 0 AND progress <= 100)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_progress_not_null` | `NOT NULL progress` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_project_type_check` | `CHECK (project_type = ANY (ARRAY['mod'::text, 'modpack'::text, 'plugin'::text, 'map'::text, 'resource_pack'::text, 'shader_pack'::text, 'datapack'::text, 'addon'::text]))` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_project_type_not_null` | `NOT NULL project_type` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_provider_check` | `CHECK (provider = ANY (ARRAY['modrinth'::text, 'curseforge'::text, 'github'::text]))` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_provider_not_null` | `NOT NULL provider` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_public_id_key` | `UNIQUE (public_id)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_public_id_not_null` | `NOT NULL public_id` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_source_url_not_null` | `NOT NULL source_url` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_status_check` | `CHECK (status = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'failed'::text]))` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_status_not_null` | `NOT NULL status` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_user_id_not_null` | `NOT NULL user_id` |
| `mod_relationship_groups` | `mod_relationship_groups_created_at_not_null` | `NOT NULL created_at` |
| `mod_relationship_groups` | `mod_relationship_groups_display_order_not_null` | `NOT NULL display_order` |
| `mod_relationship_groups` | `mod_relationship_groups_id_not_null` | `NOT NULL id` |
| `mod_relationship_groups` | `mod_relationship_groups_label_not_null` | `NOT NULL label` |
| `mod_relationship_groups` | `mod_relationship_groups_loader_not_null` | `NOT NULL loader` |
| `mod_relationship_groups` | `mod_relationship_groups_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `mod_relationship_groups` | `mod_relationship_groups_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_relationship_groups` | `mod_relationship_groups_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_relationship_groups` | `mod_relationship_groups_mod_version_not_null` | `NOT NULL mod_version` |
| `mod_relationship_groups` | `mod_relationship_groups_pkey` | `PRIMARY KEY (id)` |
| `mod_relationships` | `fk_mod_relationships_group` | `FOREIGN KEY (group_id) REFERENCES mod_relationship_groups(id) ON DELETE CASCADE` |
| `mod_relationships` | `mod_relationships_check` | `CHECK (related_mod_id IS NOT NULL OR related_mod_name <> ''::text OR related_mod_identifier <> ''::text)` |
| `mod_relationships` | `mod_relationships_created_at_not_null` | `NOT NULL created_at` |
| `mod_relationships` | `mod_relationships_display_order_not_null` | `NOT NULL display_order` |
| `mod_relationships` | `mod_relationships_id_not_null` | `NOT NULL id` |
| `mod_relationships` | `mod_relationships_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_relationships` | `mod_relationships_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_relationships` | `mod_relationships_pkey` | `PRIMARY KEY (id)` |
| `mod_relationships` | `mod_relationships_related_mod_id_fkey` | `FOREIGN KEY (related_mod_id) REFERENCES mods(id) ON DELETE SET NULL` |
| `mod_relationships` | `mod_relationships_related_mod_identifier_not_null` | `NOT NULL related_mod_identifier` |
| `mod_relationships` | `mod_relationships_related_mod_name_not_null` | `NOT NULL related_mod_name` |
| `mod_relationships` | `mod_relationships_relation_type_check` | `CHECK (relation_type = ANY (ARRAY['dependency'::text, 'integration'::text, 'conflict'::text]))` |
| `mod_relationships` | `mod_relationships_relation_type_not_null` | `NOT NULL relation_type` |
| `mod_resource_bindings` | `mod_resource_bindings_created_at_not_null` | `NOT NULL created_at` |
| `mod_resource_bindings` | `mod_resource_bindings_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_resource_bindings` | `mod_resource_bindings_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_resource_bindings` | `mod_resource_bindings_pkey` | `PRIMARY KEY (resource_id)` |
| `mod_resource_bindings` | `mod_resource_bindings_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE CASCADE` |
| `mod_resource_bindings` | `mod_resource_bindings_resource_id_not_null` | `NOT NULL resource_id` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localiz_resource_id_version_id_fkey` | `FOREIGN KEY (resource_id, version_id) REFERENCES mod_resource_version_details(resource_id, version_id) ON DELETE CASCADE` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizat_content_markdown_not_null` | `NOT NULL content_markdown` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_locale_check` | `CHECK (locale ~ '^[A-Za-z]{2,8}(-[A-Za-z0-9]{1,8})*$'::text)` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_locale_not_null` | `NOT NULL locale` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_name_not_null` | `NOT NULL name` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_pkey` | `PRIMARY KEY (resource_id, version_id, locale)` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_provenance_check` | `CHECK (provenance = ANY (ARRAY['human'::text, 'import'::text, 'ai'::text, 'human_corrected'::text]))` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_provenance_not_null` | `NOT NULL provenance` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_resource_id_not_null` | `NOT NULL resource_id` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_summary_not_null` | `NOT NULL summary` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_version_id_not_null` | `NOT NULL version_id` |
| `mod_resource_version_details` | `mod_resource_version_details_created_at_not_null` | `NOT NULL created_at` |
| `mod_resource_version_details` | `mod_resource_version_details_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_resource_version_details` | `mod_resource_version_details_default_locale_not_null` | `NOT NULL default_locale` |
| `mod_resource_version_details` | `mod_resource_version_details_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `mod_resource_version_details` | `mod_resource_version_details_definition_not_null` | `NOT NULL definition` |
| `mod_resource_version_details` | `mod_resource_version_details_definition_schema_version_check` | `CHECK (definition_schema_version >= 1)` |
| `mod_resource_version_details` | `mod_resource_version_details_definition_schema_version_not_null` | `NOT NULL definition_schema_version` |
| `mod_resource_version_details` | `mod_resource_version_details_entry_type_code_check` | `CHECK (entry_type_code ~ '^[a-z][a-z0-9_]{1,63}$'::text)` |
| `mod_resource_version_details` | `mod_resource_version_details_entry_type_code_not_null` | `NOT NULL entry_type_code` |
| `mod_resource_version_details` | `mod_resource_version_details_icon_file_id_fkey` | `FOREIGN KEY (icon_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `mod_resource_version_details` | `mod_resource_version_details_icon_small_file_id_fkey` | `FOREIGN KEY (icon_small_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `mod_resource_version_details` | `mod_resource_version_details_pkey` | `PRIMARY KEY (resource_id, version_id)` |
| `mod_resource_version_details` | `mod_resource_version_details_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mod_resource_version_details` | `mod_resource_version_details_render_file_id_fkey` | `FOREIGN KEY (render_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `mod_resource_version_details` | `mod_resource_version_details_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES mod_resource_bindings(resource_id) ON DELETE CASCADE` |
| `mod_resource_version_details` | `mod_resource_version_details_resource_id_not_null` | `NOT NULL resource_id` |
| `mod_resource_version_details` | `mod_resource_version_details_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'pending'::text, 'archived'::text]))` |
| `mod_resource_version_details` | `mod_resource_version_details_status_not_null` | `NOT NULL status` |
| `mod_resource_version_details` | `mod_resource_version_details_updated_at_not_null` | `NOT NULL updated_at` |
| `mod_resource_version_details` | `mod_resource_version_details_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mod_resource_version_details` | `mod_resource_version_details_version_id_fkey` | `FOREIGN KEY (version_id) REFERENCES mod_content_versions(id) ON DELETE CASCADE` |
| `mod_resource_version_details` | `mod_resource_version_details_version_id_not_null` | `NOT NULL version_id` |
| `mod_tags` | `mod_tags_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE CASCADE` |
| `mod_tags` | `mod_tags_mod_id_not_null` | `NOT NULL mod_id` |
| `mod_tags` | `mod_tags_pkey` | `PRIMARY KEY (mod_id, tag)` |
| `mod_tags` | `mod_tags_tag_not_null` | `NOT NULL tag` |
| `moderation_actions` | `moderation_actions_action_type_not_null` | `NOT NULL action_type` |
| `moderation_actions` | `moderation_actions_action_type_target_type_target_public_id_key` | `UNIQUE (action_type, target_type, target_public_id, idempotency_key)` |
| `moderation_actions` | `moderation_actions_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `moderation_actions` | `moderation_actions_actor_id_not_null` | `NOT NULL actor_id` |
| `moderation_actions` | `moderation_actions_after_summary_not_null` | `NOT NULL after_summary` |
| `moderation_actions` | `moderation_actions_before_summary_not_null` | `NOT NULL before_summary` |
| `moderation_actions` | `moderation_actions_created_at_not_null` | `NOT NULL created_at` |
| `moderation_actions` | `moderation_actions_id_not_null` | `NOT NULL id` |
| `moderation_actions` | `moderation_actions_idempotency_key_not_null` | `NOT NULL idempotency_key` |
| `moderation_actions` | `moderation_actions_pkey` | `PRIMARY KEY (id)` |
| `moderation_actions` | `moderation_actions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `moderation_actions` | `moderation_actions_public_id_key` | `UNIQUE (public_id)` |
| `moderation_actions` | `moderation_actions_public_id_not_null` | `NOT NULL public_id` |
| `moderation_actions` | `moderation_actions_reason_not_null` | `NOT NULL reason` |
| `moderation_actions` | `moderation_actions_report_id_fkey` | `FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE SET NULL` |
| `moderation_actions` | `moderation_actions_target_public_id_not_null` | `NOT NULL target_public_id` |
| `moderation_actions` | `moderation_actions_target_type_not_null` | `NOT NULL target_type` |
| `moderation_actions` | `moderation_actions_target_user_id_fkey` | `FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `modpack_gallery_images` | `modpack_gallery_images_created_at_not_null` | `NOT NULL created_at` |
| `modpack_gallery_images` | `modpack_gallery_images_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `modpack_gallery_images` | `modpack_gallery_images_display_order_not_null` | `NOT NULL display_order` |
| `modpack_gallery_images` | `modpack_gallery_images_id_not_null` | `NOT NULL id` |
| `modpack_gallery_images` | `modpack_gallery_images_modpack_id_fkey` | `FOREIGN KEY (modpack_id) REFERENCES modpacks(id) ON DELETE CASCADE` |
| `modpack_gallery_images` | `modpack_gallery_images_modpack_id_not_null` | `NOT NULL modpack_id` |
| `modpack_gallery_images` | `modpack_gallery_images_modpack_id_oss_file_id_key` | `UNIQUE (modpack_id, oss_file_id)` |
| `modpack_gallery_images` | `modpack_gallery_images_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `modpack_gallery_images` | `modpack_gallery_images_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `modpack_gallery_images` | `modpack_gallery_images_pkey` | `PRIMARY KEY (id)` |
| `modpack_gallery_images` | `modpack_gallery_images_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `modpack_gallery_images` | `modpack_gallery_images_public_id_key` | `UNIQUE (public_id)` |
| `modpack_gallery_images` | `modpack_gallery_images_public_id_not_null` | `NOT NULL public_id` |
| `modpack_gallery_images` | `modpack_gallery_images_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `modpack_links` | `modpack_links_display_order_not_null` | `NOT NULL display_order` |
| `modpack_links` | `modpack_links_id_not_null` | `NOT NULL id` |
| `modpack_links` | `modpack_links_link_type_not_null` | `NOT NULL link_type` |
| `modpack_links` | `modpack_links_modpack_id_fkey` | `FOREIGN KEY (modpack_id) REFERENCES modpacks(id) ON DELETE CASCADE` |
| `modpack_links` | `modpack_links_modpack_id_link_type_url_key` | `UNIQUE (modpack_id, link_type, url)` |
| `modpack_links` | `modpack_links_modpack_id_not_null` | `NOT NULL modpack_id` |
| `modpack_links` | `modpack_links_note_not_null` | `NOT NULL note` |
| `modpack_links` | `modpack_links_pkey` | `PRIMARY KEY (id)` |
| `modpack_links` | `modpack_links_url_not_null` | `NOT NULL url` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_loader_not_null` | `NOT NULL loader` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_minecraft_version_not_null` | `NOT NULL minecraft_version` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_modpack_id_fkey` | `FOREIGN KEY (modpack_id) REFERENCES modpacks(id) ON DELETE CASCADE` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_modpack_id_not_null` | `NOT NULL modpack_id` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_pkey` | `PRIMARY KEY (modpack_id, loader, minecraft_version)` |
| `modpack_mods` | `modpack_mods_check` | `CHECK (mod_id IS NOT NULL OR provider_project_id <> ''::text OR identifier <> ''::text)` |
| `modpack_mods` | `modpack_mods_client_required_not_null` | `NOT NULL client_required` |
| `modpack_mods` | `modpack_mods_created_at_not_null` | `NOT NULL created_at` |
| `modpack_mods` | `modpack_mods_display_order_not_null` | `NOT NULL display_order` |
| `modpack_mods` | `modpack_mods_file_name_not_null` | `NOT NULL file_name` |
| `modpack_mods` | `modpack_mods_id_not_null` | `NOT NULL id` |
| `modpack_mods` | `modpack_mods_identifier_not_null` | `NOT NULL identifier` |
| `modpack_mods` | `modpack_mods_mod_id_fkey` | `FOREIGN KEY (mod_id) REFERENCES mods(id) ON DELETE RESTRICT` |
| `modpack_mods` | `modpack_mods_mod_name_not_null` | `NOT NULL mod_name` |
| `modpack_mods` | `modpack_mods_modpack_id_fkey` | `FOREIGN KEY (modpack_id) REFERENCES modpacks(id) ON DELETE CASCADE` |
| `modpack_mods` | `modpack_mods_modpack_id_not_null` | `NOT NULL modpack_id` |
| `modpack_mods` | `modpack_mods_pkey` | `PRIMARY KEY (id)` |
| `modpack_mods` | `modpack_mods_provider_check` | `CHECK (provider = ANY (ARRAY['manual'::text, 'modrinth'::text, 'curseforge'::text, 'index'::text]))` |
| `modpack_mods` | `modpack_mods_provider_not_null` | `NOT NULL provider` |
| `modpack_mods` | `modpack_mods_provider_project_id_not_null` | `NOT NULL provider_project_id` |
| `modpack_mods` | `modpack_mods_provider_version_id_not_null` | `NOT NULL provider_version_id` |
| `modpack_mods` | `modpack_mods_server_required_not_null` | `NOT NULL server_required` |
| `modpack_tags` | `modpack_tags_modpack_id_fkey` | `FOREIGN KEY (modpack_id) REFERENCES modpacks(id) ON DELETE CASCADE` |
| `modpack_tags` | `modpack_tags_modpack_id_not_null` | `NOT NULL modpack_id` |
| `modpack_tags` | `modpack_tags_pkey` | `PRIMARY KEY (modpack_id, tag)` |
| `modpack_tags` | `modpack_tags_tag_not_null` | `NOT NULL tag` |
| `modpacks` | `modpacks_abbreviation_not_null` | `NOT NULL abbreviation` |
| `modpacks` | `modpacks_body_markdown_not_null` | `NOT NULL body_markdown` |
| `modpacks` | `modpacks_created_at_not_null` | `NOT NULL created_at` |
| `modpacks` | `modpacks_curseforge_project_id_not_null` | `NOT NULL curseforge_project_id` |
| `modpacks` | `modpacks_default_locale_not_null` | `NOT NULL default_locale` |
| `modpacks` | `modpacks_environment_check` | `CHECK (environment = ANY (ARRAY['clientOnly'::text, 'serverOnly'::text, 'bothRequired'::text]))` |
| `modpacks` | `modpacks_environment_not_null` | `NOT NULL environment` |
| `modpacks` | `modpacks_icon_url_not_null` | `NOT NULL icon_url` |
| `modpacks` | `modpacks_id_not_null` | `NOT NULL id` |
| `modpacks` | `modpacks_license_not_null` | `NOT NULL license` |
| `modpacks` | `modpacks_modrinth_project_id_not_null` | `NOT NULL modrinth_project_id` |
| `modpacks` | `modpacks_official_status_check` | `CHECK (official_status = ANY (ARRAY['active'::text, 'lowFrequency'::text, 'discontinued'::text, 'archived'::text, 'development'::text]))` |
| `modpacks` | `modpacks_official_status_not_null` | `NOT NULL official_status` |
| `modpacks` | `modpacks_pack_type_check` | `CHECK (pack_type = ANY (ARRAY['native'::text, 'customized'::text]))` |
| `modpacks` | `modpacks_pack_type_not_null` | `NOT NULL pack_type` |
| `modpacks` | `modpacks_packaging_method_check` | `CHECK (packaging_method = ANY (ARRAY['curseforge'::text, 'ftb'::text, 'other_launcher'::text, 'manual'::text, 'atlauncher'::text, 'modrinth'::text, 'mcbbs'::text, 'other'::text]))` |
| `modpacks` | `modpacks_packaging_method_not_null` | `NOT NULL packaging_method` |
| `modpacks` | `modpacks_pkey` | `PRIMARY KEY (id)` |
| `modpacks` | `modpacks_primary_category_not_null` | `NOT NULL primary_category` |
| `modpacks` | `modpacks_primary_name_not_null` | `NOT NULL primary_name` |
| `modpacks` | `modpacks_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `modpacks` | `modpacks_public_id_key` | `UNIQUE (public_id)` |
| `modpacks` | `modpacks_public_id_not_null` | `NOT NULL public_id` |
| `modpacks` | `modpacks_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `modpacks` | `modpacks_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `modpacks` | `modpacks_review_status_not_null` | `NOT NULL review_status` |
| `modpacks` | `modpacks_search_keywords_not_null` | `NOT NULL search_keywords` |
| `modpacks` | `modpacks_secondary_name_not_null` | `NOT NULL secondary_name` |
| `modpacks` | `modpacks_slug_key` | `UNIQUE (slug)` |
| `modpacks` | `modpacks_slug_not_null` | `NOT NULL slug` |
| `modpacks` | `modpacks_source_status_check` | `CHECK (source_status = ANY (ARRAY['open'::text, 'partial'::text, 'closed'::text, 'unknown'::text]))` |
| `modpacks` | `modpacks_source_status_not_null` | `NOT NULL source_status` |
| `modpacks` | `modpacks_submission_method_check` | `CHECK (submission_method = ANY (ARRAY['manual'::text, 'modrinth'::text, 'curseforge'::text]))` |
| `modpacks` | `modpacks_submission_method_not_null` | `NOT NULL submission_method` |
| `modpacks` | `modpacks_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `modpacks` | `modpacks_summary_not_null` | `NOT NULL summary` |
| `modpacks` | `modpacks_updated_at_not_null` | `NOT NULL updated_at` |
| `mods` | `fk_mods_published_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `mods` | `mods_abbreviation_not_null` | `NOT NULL abbreviation` |
| `mods` | `mods_body_markdown_not_null` | `NOT NULL body_markdown` |
| `mods` | `mods_created_at_not_null` | `NOT NULL created_at` |
| `mods` | `mods_curseforge_project_id_not_null` | `NOT NULL curseforge_project_id` |
| `mods` | `mods_environment_check` | `CHECK (environment = ANY (ARRAY['clientOnly'::text, 'serverOnly'::text, 'bothRequired'::text, 'clientOptional'::text, 'serverOptional'::text]))` |
| `mods` | `mods_environment_not_null` | `NOT NULL environment` |
| `mods` | `mods_github_project_path_not_null` | `NOT NULL github_project_path` |
| `mods` | `mods_icon_url_not_null` | `NOT NULL icon_url` |
| `mods` | `mods_id_not_null` | `NOT NULL id` |
| `mods` | `mods_license_not_null` | `NOT NULL license` |
| `mods` | `mods_modrinth_project_id_not_null` | `NOT NULL modrinth_project_id` |
| `mods` | `mods_official_status_check` | `CHECK (official_status = ANY (ARRAY['active'::text, 'lowFrequency'::text, 'discontinued'::text, 'archived'::text, 'development'::text]))` |
| `mods` | `mods_official_status_not_null` | `NOT NULL official_status` |
| `mods` | `mods_pkey` | `PRIMARY KEY (id)` |
| `mods` | `mods_primary_category_not_null` | `NOT NULL primary_category` |
| `mods` | `mods_primary_name_not_null` | `NOT NULL primary_name` |
| `mods` | `mods_project_code_check` | `CHECK (project_code ~ '^[a-z0-9]{9}$'::text)` |
| `mods` | `mods_project_code_key` | `UNIQUE (project_code)` |
| `mods` | `mods_project_code_not_null` | `NOT NULL project_code` |
| `mods` | `mods_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `mods` | `mods_review_status_not_null` | `NOT NULL review_status` |
| `mods` | `mods_search_keywords_not_null` | `NOT NULL search_keywords` |
| `mods` | `mods_secondary_name_not_null` | `NOT NULL secondary_name` |
| `mods` | `mods_slug_key` | `UNIQUE (slug)` |
| `mods` | `mods_slug_not_null` | `NOT NULL slug` |
| `mods` | `mods_source_status_check` | `CHECK (source_status = ANY (ARRAY['open'::text, 'partial'::text, 'closed'::text, 'unknown'::text]))` |
| `mods` | `mods_source_status_not_null` | `NOT NULL source_status` |
| `mods` | `mods_submission_method_check` | `CHECK (submission_method = ANY (ARRAY['manual'::text, 'modrinth'::text, 'curseforge'::text, 'github'::text]))` |
| `mods` | `mods_submission_method_not_null` | `NOT NULL submission_method` |
| `mods` | `mods_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `mods` | `mods_summary_not_null` | `NOT NULL summary` |
| `mods` | `mods_updated_at_not_null` | `NOT NULL updated_at` |
| `nats_outbox` | `nats_outbox_aggregate_id_not_null` | `NOT NULL aggregate_id` |
| `nats_outbox` | `nats_outbox_aggregate_type_not_null` | `NOT NULL aggregate_type` |
| `nats_outbox` | `nats_outbox_attempts_not_null` | `NOT NULL attempts` |
| `nats_outbox` | `nats_outbox_available_at_not_null` | `NOT NULL available_at` |
| `nats_outbox` | `nats_outbox_created_at_not_null` | `NOT NULL created_at` |
| `nats_outbox` | `nats_outbox_event_id_key` | `UNIQUE (event_id)` |
| `nats_outbox` | `nats_outbox_event_id_not_null` | `NOT NULL event_id` |
| `nats_outbox` | `nats_outbox_event_type_not_null` | `NOT NULL event_type` |
| `nats_outbox` | `nats_outbox_id_not_null` | `NOT NULL id` |
| `nats_outbox` | `nats_outbox_last_error_not_null` | `NOT NULL last_error` |
| `nats_outbox` | `nats_outbox_locked_by_not_null` | `NOT NULL locked_by` |
| `nats_outbox` | `nats_outbox_max_attempts_check` | `CHECK (max_attempts > 0)` |
| `nats_outbox` | `nats_outbox_max_attempts_not_null` | `NOT NULL max_attempts` |
| `nats_outbox` | `nats_outbox_occurred_at_not_null` | `NOT NULL occurred_at` |
| `nats_outbox` | `nats_outbox_payload_not_null` | `NOT NULL payload` |
| `nats_outbox` | `nats_outbox_pkey` | `PRIMARY KEY (id)` |
| `nats_outbox` | `nats_outbox_schema_version_check` | `CHECK (schema_version > 0)` |
| `nats_outbox` | `nats_outbox_schema_version_not_null` | `NOT NULL schema_version` |
| `nats_outbox` | `nats_outbox_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'publishing'::text, 'published'::text, 'failed'::text, 'dead'::text]))` |
| `nats_outbox` | `nats_outbox_status_not_null` | `NOT NULL status` |
| `nats_outbox` | `nats_outbox_subject_not_null` | `NOT NULL subject` |
| `nats_outbox` | `nats_outbox_trace_id_not_null` | `NOT NULL trace_id` |
| `nats_outbox` | `nats_outbox_updated_at_not_null` | `NOT NULL updated_at` |
| `notification_actors` | `notification_actors_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE CASCADE` |
| `notification_actors` | `notification_actors_actor_id_not_null` | `NOT NULL actor_id` |
| `notification_actors` | `notification_actors_created_at_not_null` | `NOT NULL created_at` |
| `notification_actors` | `notification_actors_notification_id_fkey` | `FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE` |
| `notification_actors` | `notification_actors_notification_id_not_null` | `NOT NULL notification_id` |
| `notification_actors` | `notification_actors_pkey` | `PRIMARY KEY (notification_id, actor_id)` |
| `notification_receipts` | `notification_receipts_created_at_not_null` | `NOT NULL created_at` |
| `notification_receipts` | `notification_receipts_notification_id_fkey` | `FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE` |
| `notification_receipts` | `notification_receipts_notification_id_not_null` | `NOT NULL notification_id` |
| `notification_receipts` | `notification_receipts_pkey` | `PRIMARY KEY (notification_id, user_id)` |
| `notification_receipts` | `notification_receipts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `notification_receipts` | `notification_receipts_user_id_not_null` | `NOT NULL user_id` |
| `notification_translations` | `notification_translations_body_not_null` | `NOT NULL body` |
| `notification_translations` | `notification_translations_created_at_not_null` | `NOT NULL created_at` |
| `notification_translations` | `notification_translations_locale_not_null` | `NOT NULL locale` |
| `notification_translations` | `notification_translations_notification_id_fkey` | `FOREIGN KEY (notification_id) REFERENCES notifications(id) ON DELETE CASCADE` |
| `notification_translations` | `notification_translations_notification_id_not_null` | `NOT NULL notification_id` |
| `notification_translations` | `notification_translations_pkey` | `PRIMARY KEY (notification_id, user_id, locale)` |
| `notification_translations` | `notification_translations_title_not_null` | `NOT NULL title` |
| `notification_translations` | `notification_translations_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `notification_translations` | `notification_translations_user_id_not_null` | `NOT NULL user_id` |
| `notifications` | `notifications_body_not_null` | `NOT NULL body` |
| `notifications` | `notifications_created_at_not_null` | `NOT NULL created_at` |
| `notifications` | `notifications_data_not_null` | `NOT NULL data` |
| `notifications` | `notifications_id_not_null` | `NOT NULL id` |
| `notifications` | `notifications_kind_not_null` | `NOT NULL kind` |
| `notifications` | `notifications_pkey` | `PRIMARY KEY (id)` |
| `notifications` | `notifications_project_update_event_id_fkey` | `FOREIGN KEY (project_update_event_id) REFERENCES project_update_events(id) ON DELETE SET NULL` |
| `notifications` | `notifications_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `notifications` | `notifications_public_id_key` | `UNIQUE (public_id)` |
| `notifications` | `notifications_public_id_not_null` | `NOT NULL public_id` |
| `notifications` | `notifications_recipient_id_fkey` | `FOREIGN KEY (recipient_id) REFERENCES users(id) ON DELETE CASCADE` |
| `notifications` | `notifications_source_locale_not_null` | `NOT NULL source_locale` |
| `notifications` | `notifications_template_params_not_null` | `NOT NULL template_params` |
| `notifications` | `notifications_template_version_not_null` | `NOT NULL template_version` |
| `notifications` | `notifications_title_not_null` | `NOT NULL title` |
| `notifications` | `notifications_updated_at_not_null` | `NOT NULL updated_at` |
| `oauth_accounts` | `oauth_accounts_avatar_url_not_null` | `NOT NULL avatar_url` |
| `oauth_accounts` | `oauth_accounts_created_at_not_null` | `NOT NULL created_at` |
| `oauth_accounts` | `oauth_accounts_email_not_null` | `NOT NULL email` |
| `oauth_accounts` | `oauth_accounts_id_not_null` | `NOT NULL id` |
| `oauth_accounts` | `oauth_accounts_pkey` | `PRIMARY KEY (id)` |
| `oauth_accounts` | `oauth_accounts_provider_not_null` | `NOT NULL provider` |
| `oauth_accounts` | `oauth_accounts_provider_provider_user_id_key` | `UNIQUE (provider, provider_user_id)` |
| `oauth_accounts` | `oauth_accounts_provider_user_id_not_null` | `NOT NULL provider_user_id` |
| `oauth_accounts` | `oauth_accounts_updated_at_not_null` | `NOT NULL updated_at` |
| `oauth_accounts` | `oauth_accounts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `oauth_accounts` | `oauth_accounts_user_id_not_null` | `NOT NULL user_id` |
| `oauth_accounts` | `oauth_accounts_username_not_null` | `NOT NULL username` |
| `oss_download_stats` | `oss_download_stats_downloads_not_null` | `NOT NULL downloads` |
| `oss_download_stats` | `oss_download_stats_file_id_fkey` | `FOREIGN KEY (file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `oss_download_stats` | `oss_download_stats_object_key_not_null` | `NOT NULL object_key` |
| `oss_download_stats` | `oss_download_stats_pkey` | `PRIMARY KEY (object_key)` |
| `oss_download_stats` | `oss_download_stats_total_bytes_not_null` | `NOT NULL total_bytes` |
| `oss_files` | `oss_files_bucket_not_null` | `NOT NULL bucket` |
| `oss_files` | `oss_files_category_not_null` | `NOT NULL category` |
| `oss_files` | `oss_files_content_type_not_null` | `NOT NULL content_type` |
| `oss_files` | `oss_files_created_at_not_null` | `NOT NULL created_at` |
| `oss_files` | `oss_files_endpoint_not_null` | `NOT NULL endpoint` |
| `oss_files` | `oss_files_id_not_null` | `NOT NULL id` |
| `oss_files` | `oss_files_object_key_key` | `UNIQUE (object_key)` |
| `oss_files` | `oss_files_object_key_not_null` | `NOT NULL object_key` |
| `oss_files` | `oss_files_original_name_not_null` | `NOT NULL original_name` |
| `oss_files` | `oss_files_pkey` | `PRIMARY KEY (id)` |
| `oss_files` | `oss_files_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `oss_files` | `oss_files_public_id_key` | `UNIQUE (public_id)` |
| `oss_files` | `oss_files_public_id_not_null` | `NOT NULL public_id` |
| `oss_files` | `oss_files_region_not_null` | `NOT NULL region` |
| `oss_files` | `oss_files_scan_status_check` | `CHECK (scan_status = ANY (ARRAY['pending'::text, 'clean'::text, 'rejected'::text, 'trusted_generated'::text]))` |
| `oss_files` | `oss_files_scan_status_not_null` | `NOT NULL scan_status` |
| `oss_files` | `oss_files_sha256_not_null` | `NOT NULL sha256` |
| `oss_files` | `oss_files_size_bytes_not_null` | `NOT NULL size_bytes` |
| `oss_files` | `oss_files_source_not_null` | `NOT NULL source` |
| `oss_files` | `oss_files_source_original_name_not_null` | `NOT NULL source_original_name` |
| `oss_files` | `oss_files_source_size_bytes_not_null` | `NOT NULL source_size_bytes` |
| `oss_files` | `oss_files_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text, 'quarantined'::text]))` |
| `oss_files` | `oss_files_status_not_null` | `NOT NULL status` |
| `oss_files` | `oss_files_updated_at_not_null` | `NOT NULL updated_at` |
| `oss_files` | `oss_files_uploader_id_fkey` | `FOREIGN KEY (uploader_id) REFERENCES users(id) ON DELETE SET NULL` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_attempts_not_null` | `NOT NULL attempts` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_bucket_endpoint_object_key_key` | `UNIQUE (bucket, endpoint, object_key)` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_bucket_not_null` | `NOT NULL bucket` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_created_at_not_null` | `NOT NULL created_at` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_endpoint_not_null` | `NOT NULL endpoint` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_id_not_null` | `NOT NULL id` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_last_error_not_null` | `NOT NULL last_error` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_next_attempt_at_not_null` | `NOT NULL next_attempt_at` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_object_key_not_null` | `NOT NULL object_key` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_pkey` | `PRIMARY KEY (id)` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_reason_not_null` | `NOT NULL reason` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_region_not_null` | `NOT NULL region` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text]))` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_status_not_null` | `NOT NULL status` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_updated_at_not_null` | `NOT NULL updated_at` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_use_cname_not_null` | `NOT NULL use_cname` |
| `oss_scan_logs` | `oss_scan_logs_created_at_not_null` | `NOT NULL created_at` |
| `oss_scan_logs` | `oss_scan_logs_engine_not_null` | `NOT NULL engine` |
| `oss_scan_logs` | `oss_scan_logs_file_id_fkey` | `FOREIGN KEY (file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `oss_scan_logs` | `oss_scan_logs_id_not_null` | `NOT NULL id` |
| `oss_scan_logs` | `oss_scan_logs_message_not_null` | `NOT NULL message` |
| `oss_scan_logs` | `oss_scan_logs_object_key_not_null` | `NOT NULL object_key` |
| `oss_scan_logs` | `oss_scan_logs_payload_not_null` | `NOT NULL payload` |
| `oss_scan_logs` | `oss_scan_logs_pkey` | `PRIMARY KEY (id)` |
| `oss_scan_logs` | `oss_scan_logs_result_not_null` | `NOT NULL result` |
| `oss_upload_logs` | `oss_upload_logs_created_at_not_null` | `NOT NULL created_at` |
| `oss_upload_logs` | `oss_upload_logs_file_id_fkey` | `FOREIGN KEY (file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `oss_upload_logs` | `oss_upload_logs_id_not_null` | `NOT NULL id` |
| `oss_upload_logs` | `oss_upload_logs_ip_not_null` | `NOT NULL ip` |
| `oss_upload_logs` | `oss_upload_logs_message_not_null` | `NOT NULL message` |
| `oss_upload_logs` | `oss_upload_logs_object_key_not_null` | `NOT NULL object_key` |
| `oss_upload_logs` | `oss_upload_logs_original_name_not_null` | `NOT NULL original_name` |
| `oss_upload_logs` | `oss_upload_logs_pkey` | `PRIMARY KEY (id)` |
| `oss_upload_logs` | `oss_upload_logs_result_not_null` | `NOT NULL result` |
| `oss_upload_logs` | `oss_upload_logs_size_bytes_not_null` | `NOT NULL size_bytes` |
| `oss_upload_logs` | `oss_upload_logs_uploader_id_fkey` | `FOREIGN KEY (uploader_id) REFERENCES users(id) ON DELETE SET NULL` |
| `oss_upload_logs` | `oss_upload_logs_user_agent_not_null` | `NOT NULL user_agent` |
| `permission_audit_logs` | `permission_audit_logs_action_not_null` | `NOT NULL action` |
| `permission_audit_logs` | `permission_audit_logs_created_at_not_null` | `NOT NULL created_at` |
| `permission_audit_logs` | `permission_audit_logs_id_not_null` | `NOT NULL id` |
| `permission_audit_logs` | `permission_audit_logs_operator_id_fkey` | `FOREIGN KEY (operator_id) REFERENCES users(id) ON DELETE SET NULL` |
| `permission_audit_logs` | `permission_audit_logs_payload_not_null` | `NOT NULL payload` |
| `permission_audit_logs` | `permission_audit_logs_pkey` | `PRIMARY KEY (id)` |
| `permission_audit_logs` | `permission_audit_logs_target_user_id_fkey` | `FOREIGN KEY (target_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `permission_role_track_roles` | `permission_role_track_roles_pkey` | `PRIMARY KEY (track_code, "position")` |
| `permission_role_track_roles` | `permission_role_track_roles_position_not_null` | `NOT NULL "position"` |
| `permission_role_track_roles` | `permission_role_track_roles_role_id_fkey` | `FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE` |
| `permission_role_track_roles` | `permission_role_track_roles_role_id_not_null` | `NOT NULL role_id` |
| `permission_role_track_roles` | `permission_role_track_roles_track_code_fkey` | `FOREIGN KEY (track_code) REFERENCES permission_role_tracks(code) ON DELETE CASCADE` |
| `permission_role_track_roles` | `permission_role_track_roles_track_code_not_null` | `NOT NULL track_code` |
| `permission_role_track_roles` | `permission_role_track_roles_track_code_role_id_key` | `UNIQUE (track_code, role_id)` |
| `permission_role_tracks` | `permission_role_tracks_code_not_null` | `NOT NULL code` |
| `permission_role_tracks` | `permission_role_tracks_created_at_not_null` | `NOT NULL created_at` |
| `permission_role_tracks` | `permission_role_tracks_description_not_null` | `NOT NULL description` |
| `permission_role_tracks` | `permission_role_tracks_name_not_null` | `NOT NULL name` |
| `permission_role_tracks` | `permission_role_tracks_pkey` | `PRIMARY KEY (code)` |
| `permission_role_tracks` | `permission_role_tracks_updated_at_not_null` | `NOT NULL updated_at` |
| `permissions` | `permissions_access_type_check` | `CHECK (access_type = ANY (ARRAY['read'::text, 'write'::text, 'moderation'::text, 'administration'::text, 'security'::text]))` |
| `permissions` | `permissions_access_type_not_null` | `NOT NULL access_type` |
| `permissions` | `permissions_code_key` | `UNIQUE (code)` |
| `permissions` | `permissions_code_not_null` | `NOT NULL code` |
| `permissions` | `permissions_description_not_null` | `NOT NULL description` |
| `permissions` | `permissions_id_not_null` | `NOT NULL id` |
| `permissions` | `permissions_module_not_null` | `NOT NULL module` |
| `permissions` | `permissions_name_not_null` | `NOT NULL name` |
| `permissions` | `permissions_pkey` | `PRIMARY KEY (id)` |
| `permissions` | `permissions_translations_not_null` | `NOT NULL translations` |
| `player_profile_name_history` | `player_profile_name_history_changed_at_not_null` | `NOT NULL changed_at` |
| `player_profile_name_history` | `player_profile_name_history_id_not_null` | `NOT NULL id` |
| `player_profile_name_history` | `player_profile_name_history_new_name_not_null` | `NOT NULL new_name` |
| `player_profile_name_history` | `player_profile_name_history_old_name_not_null` | `NOT NULL old_name` |
| `player_profile_name_history` | `player_profile_name_history_pkey` | `PRIMARY KEY (id)` |
| `player_profile_name_history` | `player_profile_name_history_profile_id_fkey` | `FOREIGN KEY (profile_id) REFERENCES player_profiles(id) ON DELETE CASCADE` |
| `player_profile_name_history` | `player_profile_name_history_profile_id_not_null` | `NOT NULL profile_id` |
| `player_profile_textures` | `player_profile_textures_asset_id_fkey` | `FOREIGN KEY (asset_id) REFERENCES skin_assets(id) ON DELETE RESTRICT` |
| `player_profile_textures` | `player_profile_textures_asset_id_not_null` | `NOT NULL asset_id` |
| `player_profile_textures` | `player_profile_textures_equipped_at_not_null` | `NOT NULL equipped_at` |
| `player_profile_textures` | `player_profile_textures_kind_check` | `CHECK (kind = ANY (ARRAY['skin'::text, 'cape'::text]))` |
| `player_profile_textures` | `player_profile_textures_kind_not_null` | `NOT NULL kind` |
| `player_profile_textures` | `player_profile_textures_model_check` | `CHECK (model = ANY (ARRAY['default'::text, 'slim'::text]))` |
| `player_profile_textures` | `player_profile_textures_model_not_null` | `NOT NULL model` |
| `player_profile_textures` | `player_profile_textures_pkey` | `PRIMARY KEY (profile_id, kind)` |
| `player_profile_textures` | `player_profile_textures_profile_id_fkey` | `FOREIGN KEY (profile_id) REFERENCES player_profiles(id) ON DELETE CASCADE` |
| `player_profile_textures` | `player_profile_textures_profile_id_not_null` | `NOT NULL profile_id` |
| `player_profiles` | `player_profiles_bio_not_null` | `NOT NULL bio` |
| `player_profiles` | `player_profiles_created_at_not_null` | `NOT NULL created_at` |
| `player_profiles` | `player_profiles_id_not_null` | `NOT NULL id` |
| `player_profiles` | `player_profiles_is_default_not_null` | `NOT NULL is_default` |
| `player_profiles` | `player_profiles_name_check` | `CHECK (name ~ '^[A-Za-z0-9_]{3,16}$'::text)` |
| `player_profiles` | `player_profiles_name_not_null` | `NOT NULL name` |
| `player_profiles` | `player_profiles_pkey` | `PRIMARY KEY (id)` |
| `player_profiles` | `player_profiles_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `player_profiles` | `player_profiles_public_id_key` | `UNIQUE (public_id)` |
| `player_profiles` | `player_profiles_public_id_not_null` | `NOT NULL public_id` |
| `player_profiles` | `player_profiles_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text]))` |
| `player_profiles` | `player_profiles_status_not_null` | `NOT NULL status` |
| `player_profiles` | `player_profiles_updated_at_not_null` | `NOT NULL updated_at` |
| `player_profiles` | `player_profiles_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `player_profiles` | `player_profiles_user_id_not_null` | `NOT NULL user_id` |
| `player_profiles` | `player_profiles_uuid_key` | `UNIQUE (uuid)` |
| `player_profiles` | `player_profiles_uuid_not_null` | `NOT NULL uuid` |
| `player_profiles` | `player_profiles_visibility_check` | `CHECK (visibility = ANY (ARRAY['public'::text, 'unlisted'::text, 'private'::text]))` |
| `player_profiles` | `player_profiles_visibility_not_null` | `NOT NULL visibility` |
| `processed_events` | `processed_events_consumer_not_null` | `NOT NULL consumer` |
| `processed_events` | `processed_events_event_id_not_null` | `NOT NULL event_id` |
| `processed_events` | `processed_events_event_type_not_null` | `NOT NULL event_type` |
| `processed_events` | `processed_events_pkey` | `PRIMARY KEY (consumer, event_id)` |
| `processed_events` | `processed_events_processed_at_not_null` | `NOT NULL processed_at` |
| `project_auto_update_runs` | `project_auto_update_runs_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL` |
| `project_auto_update_runs` | `project_auto_update_runs_attempts_not_null` | `NOT NULL attempts` |
| `project_auto_update_runs` | `project_auto_update_runs_created_at_not_null` | `NOT NULL created_at` |
| `project_auto_update_runs` | `project_auto_update_runs_id_not_null` | `NOT NULL id` |
| `project_auto_update_runs` | `project_auto_update_runs_last_error_code_not_null` | `NOT NULL last_error_code` |
| `project_auto_update_runs` | `project_auto_update_runs_last_error_not_null` | `NOT NULL last_error` |
| `project_auto_update_runs` | `project_auto_update_runs_lease_owner_not_null` | `NOT NULL lease_owner` |
| `project_auto_update_runs` | `project_auto_update_runs_next_attempt_at_not_null` | `NOT NULL next_attempt_at` |
| `project_auto_update_runs` | `project_auto_update_runs_pkey` | `PRIMARY KEY (id)` |
| `project_auto_update_runs` | `project_auto_update_runs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_auto_update_runs` | `project_auto_update_runs_public_id_key` | `UNIQUE (public_id)` |
| `project_auto_update_runs` | `project_auto_update_runs_public_id_not_null` | `NOT NULL public_id` |
| `project_auto_update_runs` | `project_auto_update_runs_result_not_null` | `NOT NULL result` |
| `project_auto_update_runs` | `project_auto_update_runs_setting_id_fkey` | `FOREIGN KEY (setting_id) REFERENCES project_auto_update_settings(id) ON DELETE CASCADE` |
| `project_auto_update_runs` | `project_auto_update_runs_setting_id_not_null` | `NOT NULL setting_id` |
| `project_auto_update_runs` | `project_auto_update_runs_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'failed'::text, 'dead_letter'::text]))` |
| `project_auto_update_runs` | `project_auto_update_runs_status_not_null` | `NOT NULL status` |
| `project_auto_update_settings` | `project_auto_update_settings_check` | `CHECK (update_kind <> 'minecraft_versions'::text OR source_type IS DISTINCT FROM 'github'::text)` |
| `project_auto_update_settings` | `project_auto_update_settings_configured_by_fkey` | `FOREIGN KEY (configured_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_auto_update_settings` | `project_auto_update_settings_created_at_not_null` | `NOT NULL created_at` |
| `project_auto_update_settings` | `project_auto_update_settings_enabled_not_null` | `NOT NULL enabled` |
| `project_auto_update_settings` | `project_auto_update_settings_id_not_null` | `NOT NULL id` |
| `project_auto_update_settings` | `project_auto_update_settings_interval_code_check` | `CHECK (interval_code = ANY (ARRAY['week'::text, 'month'::text, 'quarter'::text, 'half_year'::text, 'year'::text, 'never'::text]))` |
| `project_auto_update_settings` | `project_auto_update_settings_interval_code_not_null` | `NOT NULL interval_code` |
| `project_auto_update_settings` | `project_auto_update_settings_last_error_code_not_null` | `NOT NULL last_error_code` |
| `project_auto_update_settings` | `project_auto_update_settings_last_error_not_null` | `NOT NULL last_error` |
| `project_auto_update_settings` | `project_auto_update_settings_last_status_not_null` | `NOT NULL last_status` |
| `project_auto_update_settings` | `project_auto_update_settings_license_override_not_null` | `NOT NULL license_override` |
| `project_auto_update_settings` | `project_auto_update_settings_license_override_reason_not_null` | `NOT NULL license_override_reason` |
| `project_auto_update_settings` | `project_auto_update_settings_license_override_source_not_null` | `NOT NULL license_override_source` |
| `project_auto_update_settings` | `project_auto_update_settings_pkey` | `PRIMARY KEY (id)` |
| `project_auto_update_settings` | `project_auto_update_settings_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_auto_update_settings` | `project_auto_update_settings_project_route_id_not_null` | `NOT NULL project_route_id` |
| `project_auto_update_settings` | `project_auto_update_settings_project_route_id_update_kind_key` | `UNIQUE (project_route_id, update_kind)` |
| `project_auto_update_settings` | `project_auto_update_settings_source_type_check` | `CHECK (source_type IS NULL OR (source_type = ANY (ARRAY['modrinth'::text, 'curseforge'::text, 'github'::text])))` |
| `project_auto_update_settings` | `project_auto_update_settings_update_kind_check` | `CHECK (update_kind = ANY (ARRAY['minecraft_versions'::text, 'changelog'::text, 'site_downloads'::text]))` |
| `project_auto_update_settings` | `project_auto_update_settings_update_kind_not_null` | `NOT NULL update_kind` |
| `project_auto_update_settings` | `project_auto_update_settings_updated_at_not_null` | `NOT NULL updated_at` |
| `project_automation_activity` | `project_automation_activity_automated_status_check` | `CHECK (automated_status IS NULL OR (automated_status = ANY (ARRAY['lowFrequency'::text, 'discontinued'::text])))` |
| `project_automation_activity` | `project_automation_activity_changed_by_fkey` | `FOREIGN KEY (changed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_automation_activity` | `project_automation_activity_has_external_activity_not_null` | `NOT NULL has_external_activity` |
| `project_automation_activity` | `project_automation_activity_last_checked_at_not_null` | `NOT NULL last_checked_at` |
| `project_automation_activity` | `project_automation_activity_last_project_change_at_not_null` | `NOT NULL last_project_change_at` |
| `project_automation_activity` | `project_automation_activity_last_source_type_not_null` | `NOT NULL last_source_type` |
| `project_automation_activity` | `project_automation_activity_last_update_kind_not_null` | `NOT NULL last_update_kind` |
| `project_automation_activity` | `project_automation_activity_manual_status_override_not_null` | `NOT NULL manual_status_override` |
| `project_automation_activity` | `project_automation_activity_pkey` | `PRIMARY KEY (project_route_id)` |
| `project_automation_activity` | `project_automation_activity_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_automation_activity` | `project_automation_activity_project_route_id_not_null` | `NOT NULL project_route_id` |
| `project_automation_activity` | `project_automation_activity_status_before_automation_check` | `CHECK (status_before_automation = ''::text OR (status_before_automation = ANY (ARRAY['active'::text, 'lowFrequency'::text, 'discontinued'::text, 'archived'::text, 'development'::text])))` |
| `project_automation_activity` | `project_automation_activity_status_before_automation_not_null` | `NOT NULL status_before_automation` |
| `project_automation_activity` | `project_automation_activity_updated_at_not_null` | `NOT NULL updated_at` |
| `project_changelog_categories` | `project_changelog_categories_created_at_not_null` | `NOT NULL created_at` |
| `project_changelog_categories` | `project_changelog_categories_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_changelog_categories` | `project_changelog_categories_default_locale_not_null` | `NOT NULL default_locale` |
| `project_changelog_categories` | `project_changelog_categories_id_not_null` | `NOT NULL id` |
| `project_changelog_categories` | `project_changelog_categories_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_changelog_categories` | `project_changelog_categories_object_route_id_not_null` | `NOT NULL object_route_id` |
| `project_changelog_categories` | `project_changelog_categories_pkey` | `PRIMARY KEY (id)` |
| `project_changelog_categories` | `project_changelog_categories_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_changelog_categories` | `project_changelog_categories_public_id_key` | `UNIQUE (public_id)` |
| `project_changelog_categories` | `project_changelog_categories_public_id_not_null` | `NOT NULL public_id` |
| `project_changelog_categories` | `project_changelog_categories_updated_at_not_null` | `NOT NULL updated_at` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_category_id_fkey` | `FOREIGN KEY (category_id) REFERENCES project_changelog_categories(id) ON DELETE CASCADE` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_category_id_not_null` | `NOT NULL category_id` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_locale_not_null` | `NOT NULL locale` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_name_check` | `CHECK (char_length(name) >= 1 AND char_length(name) <= 80)` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_name_not_null` | `NOT NULL name` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_pkey` | `PRIMARY KEY (category_id, locale)` |
| `project_changelog_localizations` | `project_changelog_localizations_body_markdown_not_null` | `NOT NULL body_markdown` |
| `project_changelog_localizations` | `project_changelog_localizations_changelog_id_fkey` | `FOREIGN KEY (changelog_id) REFERENCES project_changelogs(id) ON DELETE CASCADE` |
| `project_changelog_localizations` | `project_changelog_localizations_changelog_id_not_null` | `NOT NULL changelog_id` |
| `project_changelog_localizations` | `project_changelog_localizations_locale_not_null` | `NOT NULL locale` |
| `project_changelog_localizations` | `project_changelog_localizations_pkey` | `PRIMARY KEY (changelog_id, locale)` |
| `project_changelog_localizations` | `project_changelog_localizations_source_revision_id_fkey` | `FOREIGN KEY (source_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `project_changelog_localizations` | `project_changelog_localizations_source_revision_id_not_null` | `NOT NULL source_revision_id` |
| `project_changelog_localizations` | `project_changelog_localizations_updated_at_not_null` | `NOT NULL updated_at` |
| `project_changelog_localizations` | `project_changelog_localizations_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_changelogs` | `project_changelogs_category_id_fkey` | `FOREIGN KEY (category_id) REFERENCES project_changelog_categories(id) ON DELETE SET NULL` |
| `project_changelogs` | `project_changelogs_created_at_not_null` | `NOT NULL created_at` |
| `project_changelogs` | `project_changelogs_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT` |
| `project_changelogs` | `project_changelogs_created_by_not_null` | `NOT NULL created_by` |
| `project_changelogs` | `project_changelogs_default_locale_not_null` | `NOT NULL default_locale` |
| `project_changelogs` | `project_changelogs_event_at_not_null` | `NOT NULL event_at` |
| `project_changelogs` | `project_changelogs_id_not_null` | `NOT NULL id` |
| `project_changelogs` | `project_changelogs_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `project_changelogs` | `project_changelogs_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_changelogs` | `project_changelogs_object_route_id_not_null` | `NOT NULL object_route_id` |
| `project_changelogs` | `project_changelogs_pkey` | `PRIMARY KEY (id)` |
| `project_changelogs` | `project_changelogs_project_version_check` | `CHECK (char_length(project_version) >= 1 AND char_length(project_version) <= 120)` |
| `project_changelogs` | `project_changelogs_project_version_not_null` | `NOT NULL project_version` |
| `project_changelogs` | `project_changelogs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_changelogs` | `project_changelogs_public_id_key` | `UNIQUE (public_id)` |
| `project_changelogs` | `project_changelogs_public_id_not_null` | `NOT NULL public_id` |
| `project_changelogs` | `project_changelogs_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `project_changelogs` | `project_changelogs_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `project_changelogs` | `project_changelogs_review_status_not_null` | `NOT NULL review_status` |
| `project_changelogs` | `project_changelogs_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text]))` |
| `project_changelogs` | `project_changelogs_status_not_null` | `NOT NULL status` |
| `project_changelogs` | `project_changelogs_updated_at_not_null` | `NOT NULL updated_at` |
| `project_editor_application_attachments` | `project_editor_application_attachments_application_id_fkey` | `FOREIGN KEY (application_id) REFERENCES project_editor_applications(id) ON DELETE CASCADE` |
| `project_editor_application_attachments` | `project_editor_application_attachments_application_id_not_null` | `NOT NULL application_id` |
| `project_editor_application_attachments` | `project_editor_application_attachments_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `project_editor_application_attachments` | `project_editor_application_attachments_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `project_editor_application_attachments` | `project_editor_application_attachments_pkey` | `PRIMARY KEY (application_id, oss_file_id)` |
| `project_editor_applications` | `project_editor_applications_created_at_not_null` | `NOT NULL created_at` |
| `project_editor_applications` | `project_editor_applications_id_not_null` | `NOT NULL id` |
| `project_editor_applications` | `project_editor_applications_pkey` | `PRIMARY KEY (id)` |
| `project_editor_applications` | `project_editor_applications_proof_markdown_not_null` | `NOT NULL proof_markdown` |
| `project_editor_applications` | `project_editor_applications_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_editor_applications` | `project_editor_applications_public_id_key` | `UNIQUE (public_id)` |
| `project_editor_applications` | `project_editor_applications_public_id_not_null` | `NOT NULL public_id` |
| `project_editor_applications` | `project_editor_applications_review_note_not_null` | `NOT NULL review_note` |
| `project_editor_applications` | `project_editor_applications_reviewed_by_fkey` | `FOREIGN KEY (reviewed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_editor_applications` | `project_editor_applications_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text, 'withdrawn'::text]))` |
| `project_editor_applications` | `project_editor_applications_status_not_null` | `NOT NULL status` |
| `project_editor_applications` | `project_editor_applications_target_route_id_fkey` | `FOREIGN KEY (target_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_editor_applications` | `project_editor_applications_target_route_id_not_null` | `NOT NULL target_route_id` |
| `project_editor_applications` | `project_editor_applications_updated_at_not_null` | `NOT NULL updated_at` |
| `project_editor_applications` | `project_editor_applications_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `project_editor_applications` | `project_editor_applications_user_id_not_null` | `NOT NULL user_id` |
| `project_editor_assignments` | `project_editor_assignments_application_id_fkey` | `FOREIGN KEY (application_id) REFERENCES project_editor_applications(id) ON DELETE SET NULL` |
| `project_editor_assignments` | `project_editor_assignments_created_at_not_null` | `NOT NULL created_at` |
| `project_editor_assignments` | `project_editor_assignments_granted_by_fkey` | `FOREIGN KEY (granted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_editor_assignments` | `project_editor_assignments_pkey` | `PRIMARY KEY (target_route_id, user_id)` |
| `project_editor_assignments` | `project_editor_assignments_revoke_reason_not_null` | `NOT NULL revoke_reason` |
| `project_editor_assignments` | `project_editor_assignments_revoked_by_fkey` | `FOREIGN KEY (revoked_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_editor_assignments` | `project_editor_assignments_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'revoked'::text]))` |
| `project_editor_assignments` | `project_editor_assignments_status_not_null` | `NOT NULL status` |
| `project_editor_assignments` | `project_editor_assignments_target_route_id_fkey` | `FOREIGN KEY (target_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_editor_assignments` | `project_editor_assignments_target_route_id_not_null` | `NOT NULL target_route_id` |
| `project_editor_assignments` | `project_editor_assignments_updated_at_not_null` | `NOT NULL updated_at` |
| `project_editor_assignments` | `project_editor_assignments_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `project_editor_assignments` | `project_editor_assignments_user_id_not_null` | `NOT NULL user_id` |
| `project_external_sources` | `project_external_sources_created_at_not_null` | `NOT NULL created_at` |
| `project_external_sources` | `project_external_sources_external_project_id_not_null` | `NOT NULL external_project_id` |
| `project_external_sources` | `project_external_sources_external_project_url_not_null` | `NOT NULL external_project_url` |
| `project_external_sources` | `project_external_sources_id_not_null` | `NOT NULL id` |
| `project_external_sources` | `project_external_sources_pkey` | `PRIMARY KEY (id)` |
| `project_external_sources` | `project_external_sources_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_external_sources` | `project_external_sources_project_route_id_not_null` | `NOT NULL project_route_id` |
| `project_external_sources` | `project_external_sources_project_route_id_source_type_key` | `UNIQUE (project_route_id, source_type)` |
| `project_external_sources` | `project_external_sources_source_type_check` | `CHECK (source_type = ANY (ARRAY['modrinth'::text, 'curseforge'::text, 'github'::text]))` |
| `project_external_sources` | `project_external_sources_source_type_external_project_id_key` | `UNIQUE (source_type, external_project_id)` |
| `project_external_sources` | `project_external_sources_source_type_not_null` | `NOT NULL source_type` |
| `project_external_sources` | `project_external_sources_verified_at_not_null` | `NOT NULL verified_at` |
| `project_external_sources` | `project_external_sources_verified_by_fkey` | `FOREIGN KEY (verified_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_files` | `project_files_content_type_not_null` | `NOT NULL content_type` |
| `project_files` | `project_files_created_at_not_null` | `NOT NULL created_at` |
| `project_files` | `project_files_display_name_not_null` | `NOT NULL display_name` |
| `project_files` | `project_files_download_count_not_null` | `NOT NULL download_count` |
| `project_files` | `project_files_file_name_not_null` | `NOT NULL file_name` |
| `project_files` | `project_files_game_versions_not_null` | `NOT NULL game_versions` |
| `project_files` | `project_files_id_not_null` | `NOT NULL id` |
| `project_files` | `project_files_loaders_not_null` | `NOT NULL loaders` |
| `project_files` | `project_files_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `project_files` | `project_files_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `project_files` | `project_files_pkey` | `PRIMARY KEY (id)` |
| `project_files` | `project_files_project_internal_id_not_null` | `NOT NULL project_internal_id` |
| `project_files` | `project_files_project_type_check` | `CHECK (project_type = ANY (ARRAY['mod'::text, 'modpack'::text, 'plugin'::text, 'map'::text, 'resource_pack'::text, 'shader_pack'::text, 'datapack'::text, 'addon'::text]))` |
| `project_files` | `project_files_project_type_not_null` | `NOT NULL project_type` |
| `project_files` | `project_files_project_type_project_internal_id_fkey` | `FOREIGN KEY (project_type, project_internal_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE CASCADE` |
| `project_files` | `project_files_project_type_project_internal_id_oss_file_id_key` | `UNIQUE (project_type, project_internal_id, oss_file_id)` |
| `project_files` | `project_files_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_files` | `project_files_public_id_key` | `UNIQUE (public_id)` |
| `project_files` | `project_files_public_id_not_null` | `NOT NULL public_id` |
| `project_files` | `project_files_release_channel_check` | `CHECK (release_channel = ANY (ARRAY['release'::text, 'beta'::text, 'alpha'::text]))` |
| `project_files` | `project_files_release_channel_not_null` | `NOT NULL release_channel` |
| `project_files` | `project_files_sha256_not_null` | `NOT NULL sha256` |
| `project_files` | `project_files_size_bytes_not_null` | `NOT NULL size_bytes` |
| `project_files` | `project_files_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text]))` |
| `project_files` | `project_files_status_not_null` | `NOT NULL status` |
| `project_files` | `project_files_updated_at_not_null` | `NOT NULL updated_at` |
| `project_files` | `project_files_uploaded_by_fkey` | `FOREIGN KEY (uploaded_by) REFERENCES users(id) ON DELETE SET NULL` |
| `project_files` | `project_files_version_name_not_null` | `NOT NULL version_name` |
| `project_follows` | `project_follows_created_at_not_null` | `NOT NULL created_at` |
| `project_follows` | `project_follows_notifications_enabled_not_null` | `NOT NULL notifications_enabled` |
| `project_follows` | `project_follows_pkey` | `PRIMARY KEY (user_id, project_route_id)` |
| `project_follows` | `project_follows_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_follows` | `project_follows_project_route_id_not_null` | `NOT NULL project_route_id` |
| `project_follows` | `project_follows_updated_at_not_null` | `NOT NULL updated_at` |
| `project_follows` | `project_follows_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `project_follows` | `project_follows_user_id_not_null` | `NOT NULL user_id` |
| `project_update_events` | `project_update_events_actor_user_id_fkey` | `FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `project_update_events` | `project_update_events_changed_sections_not_null` | `NOT NULL changed_sections` |
| `project_update_events` | `project_update_events_created_at_not_null` | `NOT NULL created_at` |
| `project_update_events` | `project_update_events_id_not_null` | `NOT NULL id` |
| `project_update_events` | `project_update_events_pkey` | `PRIMARY KEY (id)` |
| `project_update_events` | `project_update_events_project_route_id_fkey` | `FOREIGN KEY (project_route_id) REFERENCES public_routes(id) ON DELETE CASCADE` |
| `project_update_events` | `project_update_events_project_route_id_not_null` | `NOT NULL project_route_id` |
| `project_update_events` | `project_update_events_project_route_id_publication_batch_id_key` | `UNIQUE (project_route_id, publication_batch_id)` |
| `project_update_events` | `project_update_events_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `project_update_events` | `project_update_events_public_id_key` | `UNIQUE (public_id)` |
| `project_update_events` | `project_update_events_public_id_not_null` | `NOT NULL public_id` |
| `project_update_events` | `project_update_events_publication_batch_id_not_null` | `NOT NULL publication_batch_id` |
| `project_update_events` | `project_update_events_published_at_not_null` | `NOT NULL published_at` |
| `project_update_events` | `project_update_events_update_kind_not_null` | `NOT NULL update_kind` |
| `project_update_notification_tasks` | `project_update_notification_tasks_attempt_count_not_null` | `NOT NULL attempt_count` |
| `project_update_notification_tasks` | `project_update_notification_tasks_created_at_not_null` | `NOT NULL created_at` |
| `project_update_notification_tasks` | `project_update_notification_tasks_event_id_fkey` | `FOREIGN KEY (event_id) REFERENCES project_update_events(id) ON DELETE CASCADE` |
| `project_update_notification_tasks` | `project_update_notification_tasks_event_id_not_null` | `NOT NULL event_id` |
| `project_update_notification_tasks` | `project_update_notification_tasks_last_error_not_null` | `NOT NULL last_error` |
| `project_update_notification_tasks` | `project_update_notification_tasks_next_attempt_at_not_null` | `NOT NULL next_attempt_at` |
| `project_update_notification_tasks` | `project_update_notification_tasks_next_user_id_not_null` | `NOT NULL next_user_id` |
| `project_update_notification_tasks` | `project_update_notification_tasks_notified_count_not_null` | `NOT NULL notified_count` |
| `project_update_notification_tasks` | `project_update_notification_tasks_pkey` | `PRIMARY KEY (event_id)` |
| `project_update_notification_tasks` | `project_update_notification_tasks_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'processing'::text, 'completed'::text, 'failed'::text]))` |
| `project_update_notification_tasks` | `project_update_notification_tasks_status_not_null` | `NOT NULL status` |
| `project_update_notification_tasks` | `project_update_notification_tasks_updated_at_not_null` | `NOT NULL updated_at` |
| `public_id_registry` | `public_id_registry_created_at_not_null` | `NOT NULL created_at` |
| `public_id_registry` | `public_id_registry_pkey` | `PRIMARY KEY (public_id)` |
| `public_id_registry` | `public_id_registry_public_id_check` | `CHECK (public_id::text ~ '^[a-z0-9]{9}$'::text)` |
| `public_id_registry` | `public_id_registry_public_id_not_null` | `NOT NULL public_id` |
| `public_routes` | `public_routes_canonical_path_not_null` | `NOT NULL canonical_path` |
| `public_routes` | `public_routes_created_at_not_null` | `NOT NULL created_at` |
| `public_routes` | `public_routes_entity_type_internal_id_key` | `UNIQUE (entity_type, internal_id)` |
| `public_routes` | `public_routes_entity_type_not_null` | `NOT NULL entity_type` |
| `public_routes` | `public_routes_id_not_null` | `NOT NULL id` |
| `public_routes` | `public_routes_internal_id_not_null` | `NOT NULL internal_id` |
| `public_routes` | `public_routes_pkey` | `PRIMARY KEY (id)` |
| `public_routes` | `public_routes_public_id_check` | `CHECK (public_id::text ~ '^[a-z0-9]{9}$'::text)` |
| `public_routes` | `public_routes_public_id_key` | `UNIQUE (public_id)` |
| `public_routes` | `public_routes_public_id_not_null` | `NOT NULL public_id` |
| `public_routes` | `public_routes_updated_at_not_null` | `NOT NULL updated_at` |
| `recipe_binding_candidates` | `recipe_binding_candidates_amount_check` | `CHECK (amount > 0::numeric)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_amount_not_null` | `NOT NULL amount` |
| `recipe_binding_candidates` | `recipe_binding_candidates_binding_id_candidate_index_key` | `UNIQUE (binding_id, candidate_index)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_binding_id_fkey` | `FOREIGN KEY (binding_id) REFERENCES recipe_bindings(id) ON DELETE CASCADE` |
| `recipe_binding_candidates` | `recipe_binding_candidates_binding_id_not_null` | `NOT NULL binding_id` |
| `recipe_binding_candidates` | `recipe_binding_candidates_byproduct_not_null` | `NOT NULL byproduct` |
| `recipe_binding_candidates` | `recipe_binding_candidates_candidate_index_check` | `CHECK (candidate_index >= 0)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_candidate_index_not_null` | `NOT NULL candidate_index` |
| `recipe_binding_candidates` | `recipe_binding_candidates_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_definition_not_null` | `NOT NULL definition` |
| `recipe_binding_candidates` | `recipe_binding_candidates_id_not_null` | `NOT NULL id` |
| `recipe_binding_candidates` | `recipe_binding_candidates_identity_key_key` | `UNIQUE (identity_key)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_identity_key_not_null` | `NOT NULL identity_key` |
| `recipe_binding_candidates` | `recipe_binding_candidates_pkey` | `PRIMARY KEY (id)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_probability_check` | `CHECK (probability IS NULL OR probability >= 0::numeric AND probability <= 1::numeric)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE RESTRICT` |
| `recipe_binding_candidates` | `recipe_binding_candidates_resource_id_not_null` | `NOT NULL resource_id` |
| `recipe_bindings` | `recipe_bindings_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_bindings` | `recipe_bindings_definition_not_null` | `NOT NULL definition` |
| `recipe_bindings` | `recipe_bindings_id_not_null` | `NOT NULL id` |
| `recipe_bindings` | `recipe_bindings_identity_key_key` | `UNIQUE (identity_key)` |
| `recipe_bindings` | `recipe_bindings_identity_key_not_null` | `NOT NULL identity_key` |
| `recipe_bindings` | `recipe_bindings_ordinal_check` | `CHECK (ordinal >= 0)` |
| `recipe_bindings` | `recipe_bindings_ordinal_not_null` | `NOT NULL ordinal` |
| `recipe_bindings` | `recipe_bindings_pkey` | `PRIMARY KEY (id)` |
| `recipe_bindings` | `recipe_bindings_recipe_id_fkey` | `FOREIGN KEY (recipe_id) REFERENCES recipes(entity_id) ON DELETE CASCADE` |
| `recipe_bindings` | `recipe_bindings_recipe_id_not_null` | `NOT NULL recipe_id` |
| `recipe_bindings` | `recipe_bindings_recipe_id_template_slot_id_key` | `UNIQUE (recipe_id, template_slot_id)` |
| `recipe_bindings` | `recipe_bindings_template_slot_id_fkey` | `FOREIGN KEY (template_slot_id) REFERENCES recipe_template_slots(id) ON DELETE RESTRICT` |
| `recipe_bindings` | `recipe_bindings_template_slot_id_not_null` | `NOT NULL template_slot_id` |
| `recipe_content_overrides` | `fk_recipe_content_overrides_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `recipe_content_overrides` | `recipe_content_overrides_note_not_null` | `NOT NULL note` |
| `recipe_content_overrides` | `recipe_content_overrides_pkey` | `PRIMARY KEY (recipe_id)` |
| `recipe_content_overrides` | `recipe_content_overrides_recipe_id_fkey` | `FOREIGN KEY (recipe_id) REFERENCES recipes(entity_id) ON DELETE CASCADE` |
| `recipe_content_overrides` | `recipe_content_overrides_recipe_id_not_null` | `NOT NULL recipe_id` |
| `recipe_content_overrides` | `recipe_content_overrides_updated_at_not_null` | `NOT NULL updated_at` |
| `recipe_content_overrides` | `recipe_content_overrides_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `recipe_definitions` | `fk_recipe_definitions_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `recipe_definitions` | `recipe_definitions_created_at_not_null` | `NOT NULL created_at` |
| `recipe_definitions` | `recipe_definitions_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_definitions` | `recipe_definitions_definition_not_null` | `NOT NULL definition` |
| `recipe_definitions` | `recipe_definitions_definition_schema_version_check` | `CHECK (definition_schema_version >= 1)` |
| `recipe_definitions` | `recipe_definitions_definition_schema_version_not_null` | `NOT NULL definition_schema_version` |
| `recipe_definitions` | `recipe_definitions_pkey` | `PRIMARY KEY (recipe_id)` |
| `recipe_definitions` | `recipe_definitions_recipe_id_fkey` | `FOREIGN KEY (recipe_id) REFERENCES recipes(entity_id) ON DELETE CASCADE` |
| `recipe_definitions` | `recipe_definitions_recipe_id_not_null` | `NOT NULL recipe_id` |
| `recipe_definitions` | `recipe_definitions_source_mod_content_version_id_fkey` | `FOREIGN KEY (source_mod_content_version_id) REFERENCES mod_content_versions(id) ON DELETE SET NULL` |
| `recipe_definitions` | `recipe_definitions_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES recipe_layout_templates(entity_id) ON DELETE RESTRICT` |
| `recipe_definitions` | `recipe_definitions_template_id_not_null` | `NOT NULL template_id` |
| `recipe_definitions` | `recipe_definitions_updated_at_not_null` | `NOT NULL updated_at` |
| `recipe_definitions` | `recipe_definitions_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candida_binding_id_alternative_index__key` | `UNIQUE (binding_id, alternative_index, raw_resource_id)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidate_chance_translation_key_not_null` | `NOT NULL chance_translation_key` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_alternative_index_not_null` | `NOT NULL alternative_index` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_amount_not_null` | `NOT NULL amount` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_binding_id_fkey` | `FOREIGN KEY (binding_id) REFERENCES recipe_import_bindings(id) ON DELETE CASCADE` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_binding_id_not_null` | `NOT NULL binding_id` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_byproduct_not_null` | `NOT NULL byproduct` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_available_not_null` | `NOT NULL chance_available` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_check` | `CHECK (chance IS NULL OR chance >= 0::double precision AND chance <= 1::double precision)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_comparator_not_null` | `NOT NULL chance_comparator` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_percent_check` | `CHECK (chance_percent IS NULL OR chance_percent >= 0::double precision AND chance_percent <= 100::double precision)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_source_not_null` | `NOT NULL chance_source` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_text_not_null` | `NOT NULL chance_text` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_chance_texts_not_null` | `NOT NULL chance_texts` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_check` | `CHECK (chance_available OR chance IS NULL AND chance_percent IS NULL)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_id_not_null` | `NOT NULL id` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_ingredient_kind_not_null` | `NOT NULL ingredient_kind` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_ingredient_type_not_null` | `NOT NULL ingredient_type` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_nbt_snbt_not_null` | `NOT NULL nbt_snbt` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_pkey` | `PRIMARY KEY (id)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_raw_resource_id_not_null` | `NOT NULL raw_resource_id` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE SET NULL` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_unique_id_not_null` | `NOT NULL unique_id` |
| `recipe_import_bindings` | `recipe_import_bindings_clickable_not_null` | `NOT NULL clickable` |
| `recipe_import_bindings` | `recipe_import_bindings_id_not_null` | `NOT NULL id` |
| `recipe_import_bindings` | `recipe_import_bindings_ingredient_present_not_null` | `NOT NULL ingredient_present` |
| `recipe_import_bindings` | `recipe_import_bindings_item_tag_equivalent_not_null` | `NOT NULL item_tag_equivalent` |
| `recipe_import_bindings` | `recipe_import_bindings_ordinal_not_null` | `NOT NULL ordinal` |
| `recipe_import_bindings` | `recipe_import_bindings_pkey` | `PRIMARY KEY (id)` |
| `recipe_import_bindings` | `recipe_import_bindings_placeholder_item_not_null` | `NOT NULL placeholder_item` |
| `recipe_import_bindings` | `recipe_import_bindings_recipe_snapshot_id_fkey` | `FOREIGN KEY (recipe_snapshot_id) REFERENCES recipe_import_snapshots(id) ON DELETE CASCADE` |
| `recipe_import_bindings` | `recipe_import_bindings_recipe_snapshot_id_not_null` | `NOT NULL recipe_snapshot_id` |
| `recipe_import_bindings` | `recipe_import_bindings_recipe_snapshot_id_source_slot_id_key` | `UNIQUE (recipe_snapshot_id, source_slot_id)` |
| `recipe_import_bindings` | `recipe_import_bindings_role_source_not_null` | `NOT NULL role_source` |
| `recipe_import_bindings` | `recipe_import_bindings_semantic_role_not_null` | `NOT NULL semantic_role` |
| `recipe_import_bindings` | `recipe_import_bindings_source_slot_id_not_null` | `NOT NULL source_slot_id` |
| `recipe_import_bindings` | `recipe_import_bindings_tag_id_fkey` | `FOREIGN KEY (tag_id) REFERENCES catalog_tags(entity_id) ON DELETE SET NULL` |
| `recipe_import_bindings` | `recipe_import_bindings_template_slot_id_fkey` | `FOREIGN KEY (template_slot_id) REFERENCES recipe_template_import_slots(id) ON DELETE RESTRICT` |
| `recipe_import_bindings` | `recipe_import_bindings_template_slot_id_not_null` | `NOT NULL template_slot_id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_binding_count_not_null` | `NOT NULL binding_count` |
| `recipe_import_snapshots` | `recipe_import_snapshots_check` | `CHECK (layout_kind = 'shaped'::text AND ordered IS TRUE OR layout_kind = 'shapeless'::text AND ordered IS FALSE OR (layout_kind = ANY (ARRAY['not_applicable'::text, 'unknown'::text])) AND ordered IS NULL)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_definition_schema_version_check` | `CHECK (definition_schema_version >= 1)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_definition_schema_version_not_null` | `NOT NULL definition_schema_version` |
| `recipe_import_snapshots` | `recipe_import_snapshots_id_not_null` | `NOT NULL id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_layout_available_not_null` | `NOT NULL layout_available` |
| `recipe_import_snapshots` | `recipe_import_snapshots_layout_classification_source_not_null` | `NOT NULL layout_classification_source` |
| `recipe_import_snapshots` | `recipe_import_snapshots_layout_kind_check` | `CHECK (layout_kind = ANY (ARRAY['shaped'::text, 'shapeless'::text, 'not_applicable'::text, 'unknown'::text]))` |
| `recipe_import_snapshots` | `recipe_import_snapshots_layout_kind_not_null` | `NOT NULL layout_kind` |
| `recipe_import_snapshots` | `recipe_import_snapshots_origin_kind_not_null` | `NOT NULL origin_kind` |
| `recipe_import_snapshots` | `recipe_import_snapshots_parameters_not_null` | `NOT NULL parameters` |
| `recipe_import_snapshots` | `recipe_import_snapshots_pkey` | `PRIMARY KEY (id)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_recipe_collection_path_not_null` | `NOT NULL recipe_collection_path` |
| `recipe_import_snapshots` | `recipe_import_snapshots_recipe_id_fkey` | `FOREIGN KEY (recipe_id) REFERENCES recipes(entity_id) ON DELETE CASCADE` |
| `recipe_import_snapshots` | `recipe_import_snapshots_recipe_id_not_null` | `NOT NULL recipe_id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_recipe_id_revision_id_source_recipe_key` | `UNIQUE (recipe_id, revision_id, source_recipe_key)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_render_locale_not_null` | `NOT NULL render_locale` |
| `recipe_import_snapshots` | `recipe_import_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `recipe_import_snapshots` | `recipe_import_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_id_kind_not_null` | `NOT NULL source_id_kind` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_mod_id_not_null` | `NOT NULL source_mod_id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_mod_id_source_not_null` | `NOT NULL source_mod_id_source` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_mod_version_not_null` | `NOT NULL source_mod_version` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_recipe_id_not_null` | `NOT NULL source_recipe_id` |
| `recipe_import_snapshots` | `recipe_import_snapshots_source_recipe_key_not_null` | `NOT NULL source_recipe_key` |
| `recipe_import_snapshots` | `recipe_import_snapshots_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES recipe_template_import_snapshots(id) ON DELETE RESTRICT` |
| `recipe_import_snapshots` | `recipe_import_snapshots_underlying_recipe_type_id_not_null` | `NOT NULL underlying_recipe_type_id` |
| `recipe_layout_templates` | `fk_recipe_layout_templates_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `recipe_layout_templates` | `recipe_layout_templates_background_file_id_fkey` | `FOREIGN KEY (background_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `recipe_layout_templates` | `recipe_layout_templates_canvas_height_check` | `CHECK (canvas_height > 0 AND canvas_height <= 8192)` |
| `recipe_layout_templates` | `recipe_layout_templates_canvas_height_not_null` | `NOT NULL canvas_height` |
| `recipe_layout_templates` | `recipe_layout_templates_canvas_width_check` | `CHECK (canvas_width > 0 AND canvas_width <= 8192)` |
| `recipe_layout_templates` | `recipe_layout_templates_canvas_width_not_null` | `NOT NULL canvas_width` |
| `recipe_layout_templates` | `recipe_layout_templates_created_at_not_null` | `NOT NULL created_at` |
| `recipe_layout_templates` | `recipe_layout_templates_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_layout_templates` | `recipe_layout_templates_definition_not_null` | `NOT NULL definition` |
| `recipe_layout_templates` | `recipe_layout_templates_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `recipe_layout_templates` | `recipe_layout_templates_entity_id_not_null` | `NOT NULL entity_id` |
| `recipe_layout_templates` | `recipe_layout_templates_image_scale_check` | `CHECK (image_scale >= 1 AND image_scale <= 32)` |
| `recipe_layout_templates` | `recipe_layout_templates_image_scale_not_null` | `NOT NULL image_scale` |
| `recipe_layout_templates` | `recipe_layout_templates_import_snapshot_id_fkey` | `FOREIGN KEY (import_snapshot_id) REFERENCES recipe_template_import_snapshots(id) ON DELETE SET NULL` |
| `recipe_layout_templates` | `recipe_layout_templates_pkey` | `PRIMARY KEY (entity_id)` |
| `recipe_layout_templates` | `recipe_layout_templates_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE RESTRICT` |
| `recipe_layout_templates` | `recipe_layout_templates_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipe_layout_templates` | `recipe_layout_templates_recipe_type_id_template_key_key` | `UNIQUE (recipe_type_id, template_key)` |
| `recipe_layout_templates` | `recipe_layout_templates_template_key_not_null` | `NOT NULL template_key` |
| `recipe_layout_templates` | `recipe_layout_templates_updated_at_not_null` | `NOT NULL updated_at` |
| `recipe_layout_templates` | `recipe_layout_templates_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `recipe_template_import_slots` | `recipe_template_import_slots_coordinates_available_not_null` | `NOT NULL coordinates_available` |
| `recipe_template_import_slots` | `recipe_template_import_slots_data_not_null` | `NOT NULL data` |
| `recipe_template_import_slots` | `recipe_template_import_slots_id_not_null` | `NOT NULL id` |
| `recipe_template_import_slots` | `recipe_template_import_slots_jei_role_not_null` | `NOT NULL jei_role` |
| `recipe_template_import_slots` | `recipe_template_import_slots_ordinal_not_null` | `NOT NULL ordinal` |
| `recipe_template_import_slots` | `recipe_template_import_slots_pkey` | `PRIMARY KEY (id)` |
| `recipe_template_import_slots` | `recipe_template_import_slots_rect_not_null` | `NOT NULL rect` |
| `recipe_template_import_slots` | `recipe_template_import_slots_role_not_null` | `NOT NULL role` |
| `recipe_template_import_slots` | `recipe_template_import_slots_source_slot_id_not_null` | `NOT NULL source_slot_id` |
| `recipe_template_import_slots` | `recipe_template_import_slots_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES recipe_template_import_snapshots(id) ON DELETE CASCADE` |
| `recipe_template_import_slots` | `recipe_template_import_slots_template_id_not_null` | `NOT NULL template_id` |
| `recipe_template_import_slots` | `recipe_template_import_slots_template_id_source_slot_id_key` | `UNIQUE (template_id, source_slot_id)` |
| `recipe_template_import_slots` | `recipe_template_import_slots_visual_rect_not_null` | `NOT NULL visual_rect` |
| `recipe_template_import_snapshots` | `recipe_template_import_snap_background_contains_ingred_not_null` | `NOT NULL background_contains_ingredients` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapsh_recipe_type_snapshot_id_sourc_key` | `UNIQUE (recipe_type_snapshot_id, source_template_id)` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapsh_template_collection_path_not_null` | `NOT NULL template_collection_path` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapsho_recipe_type_snapshot_id_not_null` | `NOT NULL recipe_type_snapshot_id` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_background_path_not_null` | `NOT NULL background_path` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_canonical_template_id_fkey` | `FOREIGN KEY (canonical_template_id) REFERENCES catalog_entities(id) ON DELETE SET NULL` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_canvas_not_null` | `NOT NULL canvas` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_content_rect_not_null` | `NOT NULL content_rect` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_coordinate_space_not_null` | `NOT NULL coordinate_space` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_id_not_null` | `NOT NULL id` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_image_pixels_not_null` | `NOT NULL image_pixels` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_image_scale_not_null` | `NOT NULL image_scale` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_pkey` | `PRIMARY KEY (id)` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE CASCADE` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_recipe_type_snapshot_id_fkey` | `FOREIGN KEY (recipe_type_snapshot_id) REFERENCES recipe_type_import_snapshots(id) ON DELETE CASCADE` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_schema_version_not_null` | `NOT NULL schema_version` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_slot_count_not_null` | `NOT NULL slot_count` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_source_template_id_not_null` | `NOT NULL source_template_id` |
| `recipe_template_slots` | `recipe_template_slots_check` | `CHECK (role = 'output'::text OR output_index IS NULL)` |
| `recipe_template_slots` | `recipe_template_slots_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_template_slots` | `recipe_template_slots_definition_not_null` | `NOT NULL definition` |
| `recipe_template_slots` | `recipe_template_slots_height_check` | `CHECK (height > 0::numeric)` |
| `recipe_template_slots` | `recipe_template_slots_height_not_null` | `NOT NULL height` |
| `recipe_template_slots` | `recipe_template_slots_id_not_null` | `NOT NULL id` |
| `recipe_template_slots` | `recipe_template_slots_identity_key_key` | `UNIQUE (identity_key)` |
| `recipe_template_slots` | `recipe_template_slots_identity_key_not_null` | `NOT NULL identity_key` |
| `recipe_template_slots` | `recipe_template_slots_ordinal_check` | `CHECK (ordinal >= 0)` |
| `recipe_template_slots` | `recipe_template_slots_ordinal_not_null` | `NOT NULL ordinal` |
| `recipe_template_slots` | `recipe_template_slots_pkey` | `PRIMARY KEY (id)` |
| `recipe_template_slots` | `recipe_template_slots_role_check` | `CHECK (role = ANY (ARRAY['input'::text, 'output'::text, 'catalyst'::text]))` |
| `recipe_template_slots` | `recipe_template_slots_role_not_null` | `NOT NULL role` |
| `recipe_template_slots` | `recipe_template_slots_slot_key_not_null` | `NOT NULL slot_key` |
| `recipe_template_slots` | `recipe_template_slots_template_id_fkey` | `FOREIGN KEY (template_id) REFERENCES recipe_layout_templates(entity_id) ON DELETE CASCADE` |
| `recipe_template_slots` | `recipe_template_slots_template_id_not_null` | `NOT NULL template_id` |
| `recipe_template_slots` | `recipe_template_slots_template_id_ordinal_key` | `UNIQUE (template_id, ordinal)` |
| `recipe_template_slots` | `recipe_template_slots_template_id_slot_key_key` | `UNIQUE (template_id, slot_key)` |
| `recipe_template_slots` | `recipe_template_slots_width_check` | `CHECK (width > 0::numeric)` |
| `recipe_template_slots` | `recipe_template_slots_width_not_null` | `NOT NULL width` |
| `recipe_template_slots` | `recipe_template_slots_x_not_null` | `NOT NULL x` |
| `recipe_template_slots` | `recipe_template_slots_y_not_null` | `NOT NULL y` |
| `recipe_type_catalysts` | `fk_recipe_type_catalysts_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `recipe_type_catalysts` | `recipe_type_catalysts_ordinal_check` | `CHECK (ordinal >= 0)` |
| `recipe_type_catalysts` | `recipe_type_catalysts_ordinal_not_null` | `NOT NULL ordinal` |
| `recipe_type_catalysts` | `recipe_type_catalysts_pkey` | `PRIMARY KEY (recipe_type_id, resource_id)` |
| `recipe_type_catalysts` | `recipe_type_catalysts_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE CASCADE` |
| `recipe_type_catalysts` | `recipe_type_catalysts_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipe_type_catalysts` | `recipe_type_catalysts_recipe_type_id_ordinal_key` | `UNIQUE (recipe_type_id, ordinal)` |
| `recipe_type_catalysts` | `recipe_type_catalysts_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE RESTRICT` |
| `recipe_type_catalysts` | `recipe_type_catalysts_resource_id_not_null` | `NOT NULL resource_id` |
| `recipe_type_definitions` | `fk_recipe_type_definitions_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `recipe_type_definitions` | `recipe_type_definitions_created_at_not_null` | `NOT NULL created_at` |
| `recipe_type_definitions` | `recipe_type_definitions_definition_check` | `CHECK (jsonb_typeof(definition) = 'object'::text)` |
| `recipe_type_definitions` | `recipe_type_definitions_definition_not_null` | `NOT NULL definition` |
| `recipe_type_definitions` | `recipe_type_definitions_pkey` | `PRIMARY KEY (recipe_type_id)` |
| `recipe_type_definitions` | `recipe_type_definitions_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE CASCADE` |
| `recipe_type_definitions` | `recipe_type_definitions_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipe_type_definitions` | `recipe_type_definitions_updated_at_not_null` | `NOT NULL updated_at` |
| `recipe_type_definitions` | `recipe_type_definitions_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_background_count_not_null` | `NOT NULL background_count` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_canvas_not_null` | `NOT NULL canvas` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_catalysts_not_null` | `NOT NULL catalysts` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_exported_recipe_count_not_null` | `NOT NULL exported_recipe_count` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_height_not_null` | `NOT NULL height` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_id_not_null` | `NOT NULL id` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_image_scale_not_null` | `NOT NULL image_scale` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_pkey` | `PRIMARY KEY (id)` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_collection_path_not_null` | `NOT NULL recipe_collection_path` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_count_not_null` | `NOT NULL recipe_count` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE CASCADE` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_type_id_revision_id_key` | `UNIQUE (recipe_type_id, revision_id)` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_template_collection_path_not_null` | `NOT NULL template_collection_path` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_template_count_not_null` | `NOT NULL template_count` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_title_names_not_null` | `NOT NULL title_names` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_title_translation_key_not_null` | `NOT NULL title_translation_key` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_width_not_null` | `NOT NULL width` |
| `recipe_types` | `recipe_types_canonical_id_key` | `UNIQUE (canonical_id)` |
| `recipe_types` | `recipe_types_canonical_id_not_null` | `NOT NULL canonical_id` |
| `recipe_types` | `recipe_types_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `recipe_types` | `recipe_types_entity_id_not_null` | `NOT NULL entity_id` |
| `recipe_types` | `recipe_types_pkey` | `PRIMARY KEY (entity_id)` |
| `recipe_version_bindings` | `recipe_version_bindings_created_at_not_null` | `NOT NULL created_at` |
| `recipe_version_bindings` | `recipe_version_bindings_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `recipe_version_bindings` | `recipe_version_bindings_pkey` | `PRIMARY KEY (recipe_id, version_code)` |
| `recipe_version_bindings` | `recipe_version_bindings_recipe_id_fkey` | `FOREIGN KEY (recipe_id) REFERENCES recipes(entity_id) ON DELETE CASCADE` |
| `recipe_version_bindings` | `recipe_version_bindings_recipe_id_not_null` | `NOT NULL recipe_id` |
| `recipe_version_bindings` | `recipe_version_bindings_source_check` | `CHECK (source = ANY (ARRAY['import'::text, 'editor'::text, 'backfill'::text, 'split'::text]))` |
| `recipe_version_bindings` | `recipe_version_bindings_source_not_null` | `NOT NULL source` |
| `recipe_version_bindings` | `recipe_version_bindings_version_code_check` | `CHECK (version_code = btrim(version_code) AND version_code <> ''::text)` |
| `recipe_version_bindings` | `recipe_version_bindings_version_code_not_null` | `NOT NULL version_code` |
| `recipes` | `recipes_created_at_not_null` | `NOT NULL created_at` |
| `recipes` | `recipes_entity_id_fkey` | `FOREIGN KEY (entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `recipes` | `recipes_entity_id_not_null` | `NOT NULL entity_id` |
| `recipes` | `recipes_identity_source_check` | `CHECK (identity_source = ANY (ARRAY['manual'::text, 'import'::text, 'minecraft_recipe'::text, 'jei_category'::text, 'generated_index'::text]))` |
| `recipes` | `recipes_identity_source_not_null` | `NOT NULL identity_source` |
| `recipes` | `recipes_owner_mod_id_fkey` | `FOREIGN KEY (owner_mod_id) REFERENCES mods(id) ON DELETE SET NULL` |
| `recipes` | `recipes_pkey` | `PRIMARY KEY (entity_id)` |
| `recipes` | `recipes_recipe_type_id_fkey` | `FOREIGN KEY (recipe_type_id) REFERENCES recipe_types(entity_id) ON DELETE CASCADE` |
| `recipes` | `recipes_recipe_type_id_not_null` | `NOT NULL recipe_type_id` |
| `recipes` | `recipes_semantic_fingerprint_not_null` | `NOT NULL semantic_fingerprint` |
| `report_evidence` | `report_evidence_byte_size_check` | `CHECK (byte_size > 0)` |
| `report_evidence` | `report_evidence_byte_size_not_null` | `NOT NULL byte_size` |
| `report_evidence` | `report_evidence_cleanup_after_not_null` | `NOT NULL cleanup_after` |
| `report_evidence` | `report_evidence_content_type_not_null` | `NOT NULL content_type` |
| `report_evidence` | `report_evidence_created_at_not_null` | `NOT NULL created_at` |
| `report_evidence` | `report_evidence_delete_attempts_not_null` | `NOT NULL delete_attempts` |
| `report_evidence` | `report_evidence_id_not_null` | `NOT NULL id` |
| `report_evidence` | `report_evidence_last_error_not_null` | `NOT NULL last_error` |
| `report_evidence` | `report_evidence_object_key_key` | `UNIQUE (object_key)` |
| `report_evidence` | `report_evidence_object_key_not_null` | `NOT NULL object_key` |
| `report_evidence` | `report_evidence_original_name_not_null` | `NOT NULL original_name` |
| `report_evidence` | `report_evidence_pkey` | `PRIMARY KEY (id)` |
| `report_evidence` | `report_evidence_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `report_evidence` | `report_evidence_public_id_key` | `UNIQUE (public_id)` |
| `report_evidence` | `report_evidence_public_id_not_null` | `NOT NULL public_id` |
| `report_evidence` | `report_evidence_report_id_fkey` | `FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE SET NULL` |
| `report_evidence` | `report_evidence_scan_status_check` | `CHECK (scan_status = ANY (ARRAY['pending'::text, 'clean'::text, 'rejected'::text, 'failed'::text]))` |
| `report_evidence` | `report_evidence_scan_status_not_null` | `NOT NULL scan_status` |
| `report_evidence` | `report_evidence_sha256_check` | `CHECK (sha256 ~ '^[a-f0-9]{64}$'::text)` |
| `report_evidence` | `report_evidence_sha256_not_null` | `NOT NULL sha256` |
| `report_evidence` | `report_evidence_status_check` | `CHECK (status = ANY (ARRAY['temporary'::text, 'bound'::text, 'pending_delete'::text, 'deleted'::text]))` |
| `report_evidence` | `report_evidence_status_not_null` | `NOT NULL status` |
| `report_evidence` | `report_evidence_uploader_id_fkey` | `FOREIGN KEY (uploader_id) REFERENCES users(id) ON DELETE CASCADE` |
| `report_evidence` | `report_evidence_uploader_id_not_null` | `NOT NULL uploader_id` |
| `report_reviews` | `report_reviews_conclusion_check` | `CHECK (conclusion = ANY (ARRAY['valid'::text, 'invalid'::text, 'reopened'::text]))` |
| `report_reviews` | `report_reviews_conclusion_not_null` | `NOT NULL conclusion` |
| `report_reviews` | `report_reviews_created_at_not_null` | `NOT NULL created_at` |
| `report_reviews` | `report_reviews_id_not_null` | `NOT NULL id` |
| `report_reviews` | `report_reviews_idempotency_key_not_null` | `NOT NULL idempotency_key` |
| `report_reviews` | `report_reviews_note_not_null` | `NOT NULL note` |
| `report_reviews` | `report_reviews_pkey` | `PRIMARY KEY (id)` |
| `report_reviews` | `report_reviews_report_id_fkey` | `FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE` |
| `report_reviews` | `report_reviews_report_id_idempotency_key_key` | `UNIQUE (report_id, idempotency_key)` |
| `report_reviews` | `report_reviews_report_id_not_null` | `NOT NULL report_id` |
| `report_reviews` | `report_reviews_reviewer_id_fkey` | `FOREIGN KEY (reviewer_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `report_reviews` | `report_reviews_reviewer_id_not_null` | `NOT NULL reviewer_id` |
| `report_snapshots` | `report_snapshots_content_sha256_check` | `CHECK (content_sha256 ~ '^[a-f0-9]{64}$'::text)` |
| `report_snapshots` | `report_snapshots_content_sha256_not_null` | `NOT NULL content_sha256` |
| `report_snapshots` | `report_snapshots_created_at_not_null` | `NOT NULL created_at` |
| `report_snapshots` | `report_snapshots_payload_not_null` | `NOT NULL payload` |
| `report_snapshots` | `report_snapshots_pkey` | `PRIMARY KEY (report_id)` |
| `report_snapshots` | `report_snapshots_report_id_fkey` | `FOREIGN KEY (report_id) REFERENCES reports(id) ON DELETE CASCADE` |
| `report_snapshots` | `report_snapshots_report_id_not_null` | `NOT NULL report_id` |
| `report_snapshots` | `report_snapshots_schema_version_check` | `CHECK (schema_version > 0)` |
| `report_snapshots` | `report_snapshots_schema_version_not_null` | `NOT NULL schema_version` |
| `reports` | `reports_check` | `CHECK (reason_code <> 'other'::text OR btrim(custom_reason) <> ''::text)` |
| `reports` | `reports_claimed_by_fkey` | `FOREIGN KEY (claimed_by) REFERENCES users(id) ON DELETE SET NULL` |
| `reports` | `reports_created_at_not_null` | `NOT NULL created_at` |
| `reports` | `reports_custom_reason_not_null` | `NOT NULL custom_reason` |
| `reports` | `reports_detail_not_null` | `NOT NULL detail` |
| `reports` | `reports_id_not_null` | `NOT NULL id` |
| `reports` | `reports_lock_version_not_null` | `NOT NULL lock_version` |
| `reports` | `reports_pkey` | `PRIMARY KEY (id)` |
| `reports` | `reports_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `reports` | `reports_public_id_key` | `UNIQUE (public_id)` |
| `reports` | `reports_public_id_not_null` | `NOT NULL public_id` |
| `reports` | `reports_reason_code_not_null` | `NOT NULL reason_code` |
| `reports` | `reports_reason_text_snapshot_not_null` | `NOT NULL reason_text_snapshot` |
| `reports` | `reports_reason_version_check` | `CHECK (reason_version > 0)` |
| `reports` | `reports_reason_version_not_null` | `NOT NULL reason_version` |
| `reports` | `reports_reporter_id_fkey` | `FOREIGN KEY (reporter_id) REFERENCES users(id) ON DELETE CASCADE` |
| `reports` | `reports_reporter_id_not_null` | `NOT NULL reporter_id` |
| `reports` | `reports_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'in_review'::text, 'resolved_valid'::text, 'resolved_invalid'::text, 'cancelled'::text]))` |
| `reports` | `reports_status_not_null` | `NOT NULL status` |
| `reports` | `reports_target_author_id_fkey` | `FOREIGN KEY (target_author_id) REFERENCES users(id) ON DELETE SET NULL` |
| `reports` | `reports_target_public_id_not_null` | `NOT NULL target_public_id` |
| `reports` | `reports_target_type_check` | `CHECK (target_type = ANY (ARRAY['mod'::text, 'plugin'::text, 'map'::text, 'shader'::text, 'resource_pack'::text, 'datapack'::text, 'addon'::text, 'discussion'::text, 'bug'::text, 'news'::text, 'tutorial'::text, 'skin'::text, 'blueprint'::text, 'server'::text, 'comment'::text, 'user'::text]))` |
| `reports` | `reports_target_type_not_null` | `NOT NULL target_type` |
| `reports` | `reports_updated_at_not_null` | `NOT NULL updated_at` |
| `resource_import_snapshots` | `resource_import_snapshots_created_at_not_null` | `NOT NULL created_at` |
| `resource_import_snapshots` | `resource_import_snapshots_data_not_null` | `NOT NULL data` |
| `resource_import_snapshots` | `resource_import_snapshots_definition_schema_version_check` | `CHECK (definition_schema_version >= 1)` |
| `resource_import_snapshots` | `resource_import_snapshots_definition_schema_version_not_null` | `NOT NULL definition_schema_version` |
| `resource_import_snapshots` | `resource_import_snapshots_entry_type_code_not_null` | `NOT NULL entry_type_code` |
| `resource_import_snapshots` | `resource_import_snapshots_icon_path_not_null` | `NOT NULL icon_path` |
| `resource_import_snapshots` | `resource_import_snapshots_id_not_null` | `NOT NULL id` |
| `resource_import_snapshots` | `resource_import_snapshots_names_not_null` | `NOT NULL names` |
| `resource_import_snapshots` | `resource_import_snapshots_pkey` | `PRIMARY KEY (id)` |
| `resource_import_snapshots` | `resource_import_snapshots_preview_path_not_null` | `NOT NULL preview_path` |
| `resource_import_snapshots` | `resource_import_snapshots_registry_not_null` | `NOT NULL registry` |
| `resource_import_snapshots` | `resource_import_snapshots_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE CASCADE` |
| `resource_import_snapshots` | `resource_import_snapshots_resource_id_not_null` | `NOT NULL resource_id` |
| `resource_import_snapshots` | `resource_import_snapshots_resource_id_revision_id_key` | `UNIQUE (resource_id, revision_id)` |
| `resource_import_snapshots` | `resource_import_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `resource_import_snapshots` | `resource_import_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `resource_import_snapshots` | `resource_import_snapshots_revision_id_registry_resource_id_key` | `UNIQUE (revision_id, registry, resource_id)` |
| `resource_import_snapshots` | `resource_import_snapshots_translation_key_not_null` | `NOT NULL translation_key` |
| `resource_kinds` | `resource_kinds_code_not_null` | `NOT NULL code` |
| `resource_kinds` | `resource_kinds_created_at_not_null` | `NOT NULL created_at` |
| `resource_kinds` | `resource_kinds_display_order_not_null` | `NOT NULL display_order` |
| `resource_kinds` | `resource_kinds_family_not_null` | `NOT NULL family` |
| `resource_kinds` | `resource_kinds_pkey` | `PRIMARY KEY (code)` |
| `resource_kinds` | `resource_kinds_user_visible_not_null` | `NOT NULL user_visible` |
| `review_completion_subscriptions` | `review_completion_subscriptions_change_request_id_fkey` | `FOREIGN KEY (change_request_id) REFERENCES change_requests(id) ON DELETE CASCADE` |
| `review_completion_subscriptions` | `review_completion_subscriptions_change_request_id_not_null` | `NOT NULL change_request_id` |
| `review_completion_subscriptions` | `review_completion_subscriptions_created_at_not_null` | `NOT NULL created_at` |
| `review_completion_subscriptions` | `review_completion_subscriptions_pkey` | `PRIMARY KEY (change_request_id, user_id)` |
| `review_completion_subscriptions` | `review_completion_subscriptions_target_label_not_null` | `NOT NULL target_label` |
| `review_completion_subscriptions` | `review_completion_subscriptions_target_url_not_null` | `NOT NULL target_url` |
| `review_completion_subscriptions` | `review_completion_subscriptions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `review_completion_subscriptions` | `review_completion_subscriptions_user_id_not_null` | `NOT NULL user_id` |
| `review_events` | `review_events_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL` |
| `review_events` | `review_events_actor_snapshot_not_null` | `NOT NULL actor_snapshot` |
| `review_events` | `review_events_change_request_id_fkey` | `FOREIGN KEY (change_request_id) REFERENCES change_requests(id) ON DELETE RESTRICT` |
| `review_events` | `review_events_change_request_id_not_null` | `NOT NULL change_request_id` |
| `review_events` | `review_events_created_at_not_null` | `NOT NULL created_at` |
| `review_events` | `review_events_event_type_check` | `CHECK (event_type = ANY (ARRAY['submitted'::text, 'approved'::text, 'rejected'::text, 'conflicted'::text, 'withdrawn'::text, 'published'::text]))` |
| `review_events` | `review_events_event_type_not_null` | `NOT NULL event_type` |
| `review_events` | `review_events_id_not_null` | `NOT NULL id` |
| `review_events` | `review_events_ip_not_null` | `NOT NULL ip` |
| `review_events` | `review_events_metadata_not_null` | `NOT NULL metadata` |
| `review_events` | `review_events_note_not_null` | `NOT NULL note` |
| `review_events` | `review_events_pkey` | `PRIMARY KEY (id)` |
| `review_events` | `review_events_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `review_events` | `review_events_public_id_key` | `UNIQUE (public_id)` |
| `review_events` | `review_events_public_id_not_null` | `NOT NULL public_id` |
| `review_events` | `review_events_user_agent_not_null` | `NOT NULL user_agent` |
| `role_permissions` | `role_permissions_allow_not_null` | `NOT NULL allow` |
| `role_permissions` | `role_permissions_permission_id_fkey` | `FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE` |
| `role_permissions` | `role_permissions_permission_id_not_null` | `NOT NULL permission_id` |
| `role_permissions` | `role_permissions_pkey` | `PRIMARY KEY (role_id, permission_id)` |
| `role_permissions` | `role_permissions_role_id_fkey` | `FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE` |
| `role_permissions` | `role_permissions_role_id_not_null` | `NOT NULL role_id` |
| `role_permissions` | `role_permissions_updated_at_not_null` | `NOT NULL updated_at` |
| `roles` | `roles_code_key` | `UNIQUE (code)` |
| `roles` | `roles_code_not_null` | `NOT NULL code` |
| `roles` | `roles_created_at_not_null` | `NOT NULL created_at` |
| `roles` | `roles_description_not_null` | `NOT NULL description` |
| `roles` | `roles_id_not_null` | `NOT NULL id` |
| `roles` | `roles_name_not_null` | `NOT NULL name` |
| `roles` | `roles_parents_not_null` | `NOT NULL parents` |
| `roles` | `roles_pkey` | `PRIMARY KEY (id)` |
| `roles` | `roles_status_not_null` | `NOT NULL status` |
| `roles` | `roles_translations_not_null` | `NOT NULL translations` |
| `roles` | `roles_updated_at_not_null` | `NOT NULL updated_at` |
| `roles` | `roles_weight_not_null` | `NOT NULL weight` |
| `runtime_versions` | `runtime_versions_name_not_null` | `NOT NULL name` |
| `runtime_versions` | `runtime_versions_pkey` | `PRIMARY KEY (name)` |
| `runtime_versions` | `runtime_versions_updated_at_not_null` | `NOT NULL updated_at` |
| `runtime_versions` | `runtime_versions_version_check` | `CHECK (version > 0)` |
| `runtime_versions` | `runtime_versions_version_not_null` | `NOT NULL version` |
| `schema_metadata` | `schema_metadata_generation_not_null` | `NOT NULL generation` |
| `schema_metadata` | `schema_metadata_installed_at_not_null` | `NOT NULL installed_at` |
| `schema_metadata` | `schema_metadata_pkey` | `PRIMARY KEY (singleton)` |
| `schema_metadata` | `schema_metadata_singleton_check` | `CHECK (singleton)` |
| `schema_metadata` | `schema_metadata_singleton_not_null` | `NOT NULL singleton` |
| `search_index_queue` | `search_index_queue_attempts_check` | `CHECK (attempts >= 0)` |
| `search_index_queue` | `search_index_queue_attempts_not_null` | `NOT NULL attempts` |
| `search_index_queue` | `search_index_queue_available_at_not_null` | `NOT NULL available_at` |
| `search_index_queue` | `search_index_queue_created_at_not_null` | `NOT NULL created_at` |
| `search_index_queue` | `search_index_queue_document_id_check` | `CHECK (document_id > 0)` |
| `search_index_queue` | `search_index_queue_document_id_not_null` | `NOT NULL document_id` |
| `search_index_queue` | `search_index_queue_document_type_check` | `CHECK (document_type = ANY (ARRAY['mod'::text, 'modpack'::text, 'simple_project'::text, 'creator'::text, 'community_post'::text, 'resource'::text, 'server'::text]))` |
| `search_index_queue` | `search_index_queue_document_type_not_null` | `NOT NULL document_type` |
| `search_index_queue` | `search_index_queue_last_error_not_null` | `NOT NULL last_error` |
| `search_index_queue` | `search_index_queue_operation_check` | `CHECK (operation = ANY (ARRAY['upsert'::text, 'delete'::text]))` |
| `search_index_queue` | `search_index_queue_operation_not_null` | `NOT NULL operation` |
| `search_index_queue` | `search_index_queue_pkey` | `PRIMARY KEY (document_type, document_id)` |
| `search_index_queue` | `search_index_queue_updated_at_not_null` | `NOT NULL updated_at` |
| `search_index_state` | `search_index_state_collection_kind_not_null` | `NOT NULL collection_kind` |
| `search_index_state` | `search_index_state_collection_name_not_null` | `NOT NULL collection_name` |
| `search_index_state` | `search_index_state_pkey` | `PRIMARY KEY (collection_kind)` |
| `search_index_state` | `search_index_state_rebuilt_at_not_null` | `NOT NULL rebuilt_at` |
| `search_index_state` | `search_index_state_schema_version_check` | `CHECK (schema_version > 0)` |
| `search_index_state` | `search_index_state_schema_version_not_null` | `NOT NULL schema_version` |
| `seed_crawler_candidates` | `seed_crawler_candidates_created_at_not_null` | `NOT NULL created_at` |
| `seed_crawler_candidates` | `seed_crawler_candidates_downloads_not_null` | `NOT NULL downloads` |
| `seed_crawler_candidates` | `seed_crawler_candidates_external_project_id_key` | `UNIQUE (external_project_id)` |
| `seed_crawler_candidates` | `seed_crawler_candidates_external_project_id_not_null` | `NOT NULL external_project_id` |
| `seed_crawler_candidates` | `seed_crawler_candidates_id_not_null` | `NOT NULL id` |
| `seed_crawler_candidates` | `seed_crawler_candidates_last_error_not_null` | `NOT NULL last_error` |
| `seed_crawler_candidates` | `seed_crawler_candidates_payload_not_null` | `NOT NULL payload` |
| `seed_crawler_candidates` | `seed_crawler_candidates_pkey` | `PRIMARY KEY (id)` |
| `seed_crawler_candidates` | `seed_crawler_candidates_project_type_check` | `CHECK (project_type = ANY (ARRAY['mod'::text, 'plugin'::text, 'shader_pack'::text, 'resource_pack'::text]))` |
| `seed_crawler_candidates` | `seed_crawler_candidates_project_type_not_null` | `NOT NULL project_type` |
| `seed_crawler_candidates` | `seed_crawler_candidates_run_id_fkey` | `FOREIGN KEY (run_id) REFERENCES seed_crawler_runs(id) ON DELETE SET NULL` |
| `seed_crawler_candidates` | `seed_crawler_candidates_status_not_null` | `NOT NULL status` |
| `seed_crawler_candidates` | `seed_crawler_candidates_updated_at_not_null` | `NOT NULL updated_at` |
| `seed_crawler_configs` | `seed_crawler_configs_ai_daily_token_budget_check` | `CHECK (ai_daily_token_budget >= 0)` |
| `seed_crawler_configs` | `seed_crawler_configs_ai_daily_token_budget_not_null` | `NOT NULL ai_daily_token_budget` |
| `seed_crawler_configs` | `seed_crawler_configs_auto_submit_review_not_null` | `NOT NULL auto_submit_review` |
| `seed_crawler_configs` | `seed_crawler_configs_batch_size_check` | `CHECK (batch_size >= 1 AND batch_size <= 50)` |
| `seed_crawler_configs` | `seed_crawler_configs_batch_size_not_null` | `NOT NULL batch_size` |
| `seed_crawler_configs` | `seed_crawler_configs_daily_limit_check` | `CHECK (daily_limit >= 1 AND daily_limit <= 1000)` |
| `seed_crawler_configs` | `seed_crawler_configs_daily_limit_not_null` | `NOT NULL daily_limit` |
| `seed_crawler_configs` | `seed_crawler_configs_enabled_not_null` | `NOT NULL enabled` |
| `seed_crawler_configs` | `seed_crawler_configs_id_check` | `CHECK (id)` |
| `seed_crawler_configs` | `seed_crawler_configs_id_not_null` | `NOT NULL id` |
| `seed_crawler_configs` | `seed_crawler_configs_interval_seconds_check` | `CHECK (interval_seconds >= 60)` |
| `seed_crawler_configs` | `seed_crawler_configs_interval_seconds_not_null` | `NOT NULL interval_seconds` |
| `seed_crawler_configs` | `seed_crawler_configs_max_concurrency_check` | `CHECK (max_concurrency >= 1 AND max_concurrency <= 16)` |
| `seed_crawler_configs` | `seed_crawler_configs_max_concurrency_not_null` | `NOT NULL max_concurrency` |
| `seed_crawler_configs` | `seed_crawler_configs_minimum_downloads_check` | `CHECK (minimum_downloads >= 0)` |
| `seed_crawler_configs` | `seed_crawler_configs_minimum_downloads_not_null` | `NOT NULL minimum_downloads` |
| `seed_crawler_configs` | `seed_crawler_configs_pkey` | `PRIMARY KEY (id)` |
| `seed_crawler_configs` | `seed_crawler_configs_project_types_not_null` | `NOT NULL project_types` |
| `seed_crawler_configs` | `seed_crawler_configs_updated_at_not_null` | `NOT NULL updated_at` |
| `seed_crawler_configs` | `seed_crawler_configs_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `seed_crawler_runs` | `seed_crawler_runs_actor_id_fkey` | `FOREIGN KEY (actor_id) REFERENCES users(id) ON DELETE SET NULL` |
| `seed_crawler_runs` | `seed_crawler_runs_attempts_not_null` | `NOT NULL attempts` |
| `seed_crawler_runs` | `seed_crawler_runs_created_at_not_null` | `NOT NULL created_at` |
| `seed_crawler_runs` | `seed_crawler_runs_dry_run_not_null` | `NOT NULL dry_run` |
| `seed_crawler_runs` | `seed_crawler_runs_id_not_null` | `NOT NULL id` |
| `seed_crawler_runs` | `seed_crawler_runs_last_error_not_null` | `NOT NULL last_error` |
| `seed_crawler_runs` | `seed_crawler_runs_lease_owner_not_null` | `NOT NULL lease_owner` |
| `seed_crawler_runs` | `seed_crawler_runs_next_attempt_at_not_null` | `NOT NULL next_attempt_at` |
| `seed_crawler_runs` | `seed_crawler_runs_pkey` | `PRIMARY KEY (id)` |
| `seed_crawler_runs` | `seed_crawler_runs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `seed_crawler_runs` | `seed_crawler_runs_public_id_key` | `UNIQUE (public_id)` |
| `seed_crawler_runs` | `seed_crawler_runs_public_id_not_null` | `NOT NULL public_id` |
| `seed_crawler_runs` | `seed_crawler_runs_requested_by_fkey` | `FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE SET NULL` |
| `seed_crawler_runs` | `seed_crawler_runs_stats_not_null` | `NOT NULL stats` |
| `seed_crawler_runs` | `seed_crawler_runs_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'failed'::text, 'paused'::text]))` |
| `seed_crawler_runs` | `seed_crawler_runs_status_not_null` | `NOT NULL status` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_attempts_not_null` | `NOT NULL attempts` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_candidate_id_fkey` | `FOREIGN KEY (candidate_id) REFERENCES seed_crawler_candidates(id) ON DELETE CASCADE` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_candidate_id_not_null` | `NOT NULL candidate_id` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_input_tokens_not_null` | `NOT NULL input_tokens` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_last_error_not_null` | `NOT NULL last_error` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_locale_not_null` | `NOT NULL locale` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_output_tokens_not_null` | `NOT NULL output_tokens` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_pkey` | `PRIMARY KEY (candidate_id, locale)` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'running'::text, 'completed'::text, 'failed'::text]))` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_status_not_null` | `NOT NULL status` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_updated_at_not_null` | `NOT NULL updated_at` |
| `shop_items` | `shop_items_code_key` | `UNIQUE (code)` |
| `shop_items` | `shop_items_code_not_null` | `NOT NULL code` |
| `shop_items` | `shop_items_config_not_null` | `NOT NULL config` |
| `shop_items` | `shop_items_created_at_not_null` | `NOT NULL created_at` |
| `shop_items` | `shop_items_description_not_null` | `NOT NULL description` |
| `shop_items` | `shop_items_icon_not_null` | `NOT NULL icon` |
| `shop_items` | `shop_items_id_not_null` | `NOT NULL id` |
| `shop_items` | `shop_items_item_type_not_null` | `NOT NULL item_type` |
| `shop_items` | `shop_items_name_not_null` | `NOT NULL name` |
| `shop_items` | `shop_items_pkey` | `PRIMARY KEY (id)` |
| `shop_items` | `shop_items_price_amount_check` | `CHECK (price_amount >= 0)` |
| `shop_items` | `shop_items_price_amount_not_null` | `NOT NULL price_amount` |
| `shop_items` | `shop_items_price_currency_id_fkey` | `FOREIGN KEY (price_currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `shop_items` | `shop_items_price_currency_id_not_null` | `NOT NULL price_currency_id` |
| `shop_items` | `shop_items_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `shop_items` | `shop_items_public_id_key` | `UNIQUE (public_id)` |
| `shop_items` | `shop_items_public_id_not_null` | `NOT NULL public_id` |
| `shop_items` | `shop_items_purchase_permission_not_null` | `NOT NULL purchase_permission` |
| `shop_items` | `shop_items_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'disabled'::text]))` |
| `shop_items` | `shop_items_status_not_null` | `NOT NULL status` |
| `shop_items` | `shop_items_translations_not_null` | `NOT NULL translations` |
| `shop_items` | `shop_items_updated_at_not_null` | `NOT NULL updated_at` |
| `shop_items` | `shop_items_use_permission_not_null` | `NOT NULL use_permission` |
| `shop_purchases` | `shop_purchases_created_at_not_null` | `NOT NULL created_at` |
| `shop_purchases` | `shop_purchases_currency_id_fkey` | `FOREIGN KEY (currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `shop_purchases` | `shop_purchases_currency_id_not_null` | `NOT NULL currency_id` |
| `shop_purchases` | `shop_purchases_id_not_null` | `NOT NULL id` |
| `shop_purchases` | `shop_purchases_pkey` | `PRIMARY KEY (id)` |
| `shop_purchases` | `shop_purchases_quantity_check` | `CHECK (quantity > 0)` |
| `shop_purchases` | `shop_purchases_quantity_not_null` | `NOT NULL quantity` |
| `shop_purchases` | `shop_purchases_shop_item_id_fkey` | `FOREIGN KEY (shop_item_id) REFERENCES shop_items(id) ON DELETE RESTRICT` |
| `shop_purchases` | `shop_purchases_shop_item_id_not_null` | `NOT NULL shop_item_id` |
| `shop_purchases` | `shop_purchases_total_price_check` | `CHECK (total_price >= 0)` |
| `shop_purchases` | `shop_purchases_total_price_not_null` | `NOT NULL total_price` |
| `shop_purchases` | `shop_purchases_unit_price_check` | `CHECK (unit_price >= 0)` |
| `shop_purchases` | `shop_purchases_unit_price_not_null` | `NOT NULL unit_price` |
| `shop_purchases` | `shop_purchases_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `shop_purchases` | `shop_purchases_user_id_not_null` | `NOT NULL user_id` |
| `simple_project_gallery_images` | `simple_project_gallery_images_created_at_not_null` | `NOT NULL created_at` |
| `simple_project_gallery_images` | `simple_project_gallery_images_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `simple_project_gallery_images` | `simple_project_gallery_images_display_order_not_null` | `NOT NULL display_order` |
| `simple_project_gallery_images` | `simple_project_gallery_images_id_not_null` | `NOT NULL id` |
| `simple_project_gallery_images` | `simple_project_gallery_images_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `simple_project_gallery_images` | `simple_project_gallery_images_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `simple_project_gallery_images` | `simple_project_gallery_images_pkey` | `PRIMARY KEY (id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_project_id_fkey` | `FOREIGN KEY (project_id) REFERENCES simple_projects(id) ON DELETE CASCADE` |
| `simple_project_gallery_images` | `simple_project_gallery_images_project_id_not_null` | `NOT NULL project_id` |
| `simple_project_gallery_images` | `simple_project_gallery_images_project_id_oss_file_id_key` | `UNIQUE (project_id, oss_file_id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_public_id_key` | `UNIQUE (public_id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_public_id_not_null` | `NOT NULL public_id` |
| `simple_project_gallery_images` | `simple_project_gallery_images_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `simple_project_links` | `simple_project_links_display_order_not_null` | `NOT NULL display_order` |
| `simple_project_links` | `simple_project_links_id_not_null` | `NOT NULL id` |
| `simple_project_links` | `simple_project_links_link_type_not_null` | `NOT NULL link_type` |
| `simple_project_links` | `simple_project_links_note_not_null` | `NOT NULL note` |
| `simple_project_links` | `simple_project_links_pkey` | `PRIMARY KEY (id)` |
| `simple_project_links` | `simple_project_links_project_id_fkey` | `FOREIGN KEY (project_id) REFERENCES simple_projects(id) ON DELETE CASCADE` |
| `simple_project_links` | `simple_project_links_project_id_link_type_url_key` | `UNIQUE (project_id, link_type, url)` |
| `simple_project_links` | `simple_project_links_project_id_not_null` | `NOT NULL project_id` |
| `simple_project_links` | `simple_project_links_url_not_null` | `NOT NULL url` |
| `simple_project_localizations` | `simple_project_localizations_body_markdown_not_null` | `NOT NULL body_markdown` |
| `simple_project_localizations` | `simple_project_localizations_locale_not_null` | `NOT NULL locale` |
| `simple_project_localizations` | `simple_project_localizations_name_not_null` | `NOT NULL name` |
| `simple_project_localizations` | `simple_project_localizations_pkey` | `PRIMARY KEY (project_id, locale)` |
| `simple_project_localizations` | `simple_project_localizations_project_id_fkey` | `FOREIGN KEY (project_id) REFERENCES simple_projects(id) ON DELETE CASCADE` |
| `simple_project_localizations` | `simple_project_localizations_project_id_not_null` | `NOT NULL project_id` |
| `simple_project_localizations` | `simple_project_localizations_summary_not_null` | `NOT NULL summary` |
| `simple_project_parent_refs` | `simple_project_parent_refs_check` | `CHECK ((target_id IS NULL) <> (raw_identifier = ''::text))` |
| `simple_project_parent_refs` | `simple_project_parent_refs_display_order_not_null` | `NOT NULL display_order` |
| `simple_project_parent_refs` | `simple_project_parent_refs_id_not_null` | `NOT NULL id` |
| `simple_project_parent_refs` | `simple_project_parent_refs_pkey` | `PRIMARY KEY (id)` |
| `simple_project_parent_refs` | `simple_project_parent_refs_project_id_fkey` | `FOREIGN KEY (project_id) REFERENCES simple_projects(id) ON DELETE CASCADE` |
| `simple_project_parent_refs` | `simple_project_parent_refs_project_id_not_null` | `NOT NULL project_id` |
| `simple_project_parent_refs` | `simple_project_parent_refs_raw_identifier_not_null` | `NOT NULL raw_identifier` |
| `simple_project_parent_refs` | `simple_project_parent_refs_target_type_not_null` | `NOT NULL target_type` |
| `simple_project_parent_refs` | `simple_project_parent_refs_target_type_target_id_fkey` | `FOREIGN KEY (target_type, target_id) REFERENCES public_routes(entity_type, internal_id) ON DELETE RESTRICT` |
| `simple_projects` | `simple_projects_abbreviation_not_null` | `NOT NULL abbreviation` |
| `simple_projects` | `simple_projects_body_markdown_not_null` | `NOT NULL body_markdown` |
| `simple_projects` | `simple_projects_categories_not_null` | `NOT NULL categories` |
| `simple_projects` | `simple_projects_created_at_not_null` | `NOT NULL created_at` |
| `simple_projects` | `simple_projects_curseforge_project_id_not_null` | `NOT NULL curseforge_project_id` |
| `simple_projects` | `simple_projects_default_locale_not_null` | `NOT NULL default_locale` |
| `simple_projects` | `simple_projects_features_not_null` | `NOT NULL features` |
| `simple_projects` | `simple_projects_icon_url_not_null` | `NOT NULL icon_url` |
| `simple_projects` | `simple_projects_id_not_null` | `NOT NULL id` |
| `simple_projects` | `simple_projects_license_not_null` | `NOT NULL license` |
| `simple_projects` | `simple_projects_loaders_not_null` | `NOT NULL loaders` |
| `simple_projects` | `simple_projects_map_size_not_null` | `NOT NULL map_size` |
| `simple_projects` | `simple_projects_minecraft_versions_not_null` | `NOT NULL minecraft_versions` |
| `simple_projects` | `simple_projects_modrinth_project_id_not_null` | `NOT NULL modrinth_project_id` |
| `simple_projects` | `simple_projects_official_status_check` | `CHECK (official_status = ANY (ARRAY['active'::text, 'lowFrequency'::text, 'discontinued'::text, 'archived'::text, 'development'::text]))` |
| `simple_projects` | `simple_projects_official_status_not_null` | `NOT NULL official_status` |
| `simple_projects` | `simple_projects_performance_not_null` | `NOT NULL performance` |
| `simple_projects` | `simple_projects_pkey` | `PRIMARY KEY (id)` |
| `simple_projects` | `simple_projects_primary_name_not_null` | `NOT NULL primary_name` |
| `simple_projects` | `simple_projects_project_type_check` | `CHECK (project_type = ANY (ARRAY['plugin'::text, 'map'::text, 'resource_pack'::text, 'shader_pack'::text, 'datapack'::text, 'addon'::text]))` |
| `simple_projects` | `simple_projects_project_type_not_null` | `NOT NULL project_type` |
| `simple_projects` | `simple_projects_project_type_slug_key` | `UNIQUE (project_type, slug)` |
| `simple_projects` | `simple_projects_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `simple_projects` | `simple_projects_public_id_key` | `UNIQUE (public_id)` |
| `simple_projects` | `simple_projects_public_id_not_null` | `NOT NULL public_id` |
| `simple_projects` | `simple_projects_published_revision_id_fkey` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `simple_projects` | `simple_projects_resolution_not_null` | `NOT NULL resolution` |
| `simple_projects` | `simple_projects_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `simple_projects` | `simple_projects_review_status_not_null` | `NOT NULL review_status` |
| `simple_projects` | `simple_projects_search_keywords_not_null` | `NOT NULL search_keywords` |
| `simple_projects` | `simple_projects_slug_not_null` | `NOT NULL slug` |
| `simple_projects` | `simple_projects_source_status_check` | `CHECK (source_status = ANY (ARRAY['open'::text, 'partial'::text, 'closed'::text, 'unknown'::text]))` |
| `simple_projects` | `simple_projects_source_status_not_null` | `NOT NULL source_status` |
| `simple_projects` | `simple_projects_submission_method_check` | `CHECK (submission_method = ANY (ARRAY['manual'::text, 'modrinth'::text, 'curseforge'::text]))` |
| `simple_projects` | `simple_projects_submission_method_not_null` | `NOT NULL submission_method` |
| `simple_projects` | `simple_projects_submitted_by_fkey` | `FOREIGN KEY (submitted_by) REFERENCES users(id) ON DELETE SET NULL` |
| `simple_projects` | `simple_projects_summary_not_null` | `NOT NULL summary` |
| `simple_projects` | `simple_projects_updated_at_not_null` | `NOT NULL updated_at` |
| `site_changelog_translations` | `site_changelog_translations_body_markdown_not_null` | `NOT NULL body_markdown` |
| `site_changelog_translations` | `site_changelog_translations_changelog_id_fkey` | `FOREIGN KEY (changelog_id) REFERENCES site_changelogs(id) ON DELETE CASCADE` |
| `site_changelog_translations` | `site_changelog_translations_changelog_id_not_null` | `NOT NULL changelog_id` |
| `site_changelog_translations` | `site_changelog_translations_locale_not_null` | `NOT NULL locale` |
| `site_changelog_translations` | `site_changelog_translations_pkey` | `PRIMARY KEY (changelog_id, locale)` |
| `site_changelog_translations` | `site_changelog_translations_title_not_null` | `NOT NULL title` |
| `site_changelog_translations` | `site_changelog_translations_updated_at_not_null` | `NOT NULL updated_at` |
| `site_changelog_translations` | `site_changelog_translations_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `site_changelogs` | `site_changelogs_change_date_not_null` | `NOT NULL change_date` |
| `site_changelogs` | `site_changelogs_created_at_not_null` | `NOT NULL created_at` |
| `site_changelogs` | `site_changelogs_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `site_changelogs` | `site_changelogs_id_not_null` | `NOT NULL id` |
| `site_changelogs` | `site_changelogs_pkey` | `PRIMARY KEY (id)` |
| `site_changelogs` | `site_changelogs_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `site_changelogs` | `site_changelogs_public_id_key` | `UNIQUE (public_id)` |
| `site_changelogs` | `site_changelogs_public_id_not_null` | `NOT NULL public_id` |
| `site_changelogs` | `site_changelogs_status_check` | `CHECK (status = ANY (ARRAY['draft'::text, 'published'::text]))` |
| `site_changelogs` | `site_changelogs_status_not_null` | `NOT NULL status` |
| `site_changelogs` | `site_changelogs_updated_at_not_null` | `NOT NULL updated_at` |
| `site_daily_active_users` | `site_daily_active_users_activity_date_not_null` | `NOT NULL activity_date` |
| `site_daily_active_users` | `site_daily_active_users_first_seen_at_not_null` | `NOT NULL first_seen_at` |
| `site_daily_active_users` | `site_daily_active_users_pkey` | `PRIMARY KEY (activity_date, user_id)` |
| `site_daily_active_users` | `site_daily_active_users_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `site_daily_active_users` | `site_daily_active_users_user_id_not_null` | `NOT NULL user_id` |
| `site_daily_metrics` | `site_daily_metrics_actions_check` | `CHECK (actions >= 0)` |
| `site_daily_metrics` | `site_daily_metrics_actions_not_null` | `NOT NULL actions` |
| `site_daily_metrics` | `site_daily_metrics_active_users_check` | `CHECK (active_users >= 0)` |
| `site_daily_metrics` | `site_daily_metrics_active_users_not_null` | `NOT NULL active_users` |
| `site_daily_metrics` | `site_daily_metrics_metric_date_not_null` | `NOT NULL metric_date` |
| `site_daily_metrics` | `site_daily_metrics_new_users_check` | `CHECK (new_users >= 0)` |
| `site_daily_metrics` | `site_daily_metrics_new_users_not_null` | `NOT NULL new_users` |
| `site_daily_metrics` | `site_daily_metrics_pkey` | `PRIMARY KEY (metric_date)` |
| `site_daily_metrics` | `site_daily_metrics_review_submissions_check` | `CHECK (review_submissions >= 0)` |
| `site_daily_metrics` | `site_daily_metrics_review_submissions_not_null` | `NOT NULL review_submissions` |
| `site_daily_metrics` | `site_daily_metrics_updated_at_not_null` | `NOT NULL updated_at` |
| `site_daily_metrics` | `site_daily_metrics_views_check` | `CHECK (views >= 0)` |
| `site_daily_metrics` | `site_daily_metrics_views_not_null` | `NOT NULL views` |
| `site_monthly_active_users` | `site_monthly_active_users_activity_month_not_null` | `NOT NULL activity_month` |
| `site_monthly_active_users` | `site_monthly_active_users_last_active_date_not_null` | `NOT NULL last_active_date` |
| `site_monthly_active_users` | `site_monthly_active_users_pkey` | `PRIMARY KEY (activity_month, user_id)` |
| `site_monthly_active_users` | `site_monthly_active_users_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `site_monthly_active_users` | `site_monthly_active_users_user_id_not_null` | `NOT NULL user_id` |
| `site_page_translations` | `site_page_translations_body_markdown_not_null` | `NOT NULL body_markdown` |
| `site_page_translations` | `site_page_translations_locale_not_null` | `NOT NULL locale` |
| `site_page_translations` | `site_page_translations_page_id_fkey` | `FOREIGN KEY (page_id) REFERENCES site_pages(id) ON DELETE CASCADE` |
| `site_page_translations` | `site_page_translations_page_id_not_null` | `NOT NULL page_id` |
| `site_page_translations` | `site_page_translations_pkey` | `PRIMARY KEY (page_id, locale)` |
| `site_page_translations` | `site_page_translations_revision_not_null` | `NOT NULL revision` |
| `site_page_translations` | `site_page_translations_status_check` | `CHECK (status = ANY (ARRAY['draft'::text, 'published'::text]))` |
| `site_page_translations` | `site_page_translations_status_not_null` | `NOT NULL status` |
| `site_page_translations` | `site_page_translations_title_not_null` | `NOT NULL title` |
| `site_page_translations` | `site_page_translations_updated_at_not_null` | `NOT NULL updated_at` |
| `site_page_translations` | `site_page_translations_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `site_pages` | `site_pages_code_key` | `UNIQUE (code)` |
| `site_pages` | `site_pages_code_not_null` | `NOT NULL code` |
| `site_pages` | `site_pages_created_at_not_null` | `NOT NULL created_at` |
| `site_pages` | `site_pages_id_not_null` | `NOT NULL id` |
| `site_pages` | `site_pages_pkey` | `PRIMARY KEY (id)` |
| `site_pages` | `site_pages_published_revision_not_null` | `NOT NULL published_revision` |
| `site_pages` | `site_pages_status_check` | `CHECK (status = ANY (ARRAY['draft'::text, 'published'::text]))` |
| `site_pages` | `site_pages_status_not_null` | `NOT NULL status` |
| `site_pages` | `site_pages_updated_at_not_null` | `NOT NULL updated_at` |
| `site_pages` | `site_pages_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `site_view_daily` | `site_view_daily_counter_shard_check` | `CHECK (counter_shard >= 0 AND counter_shard <= 31)` |
| `site_view_daily` | `site_view_daily_counter_shard_not_null` | `NOT NULL counter_shard` |
| `site_view_daily` | `site_view_daily_metric_date_not_null` | `NOT NULL metric_date` |
| `site_view_daily` | `site_view_daily_pkey` | `PRIMARY KEY (metric_date, counter_shard)` |
| `site_view_daily` | `site_view_daily_views_check` | `CHECK (views >= 0)` |
| `site_view_daily` | `site_view_daily_views_not_null` | `NOT NULL views` |
| `skin_asset_adoptions` | `skin_asset_adoptions_asset_id_fkey` | `FOREIGN KEY (asset_id) REFERENCES skin_assets(id) ON DELETE CASCADE` |
| `skin_asset_adoptions` | `skin_asset_adoptions_asset_id_not_null` | `NOT NULL asset_id` |
| `skin_asset_adoptions` | `skin_asset_adoptions_created_at_not_null` | `NOT NULL created_at` |
| `skin_asset_adoptions` | `skin_asset_adoptions_pkey` | `PRIMARY KEY (user_id, asset_id)` |
| `skin_asset_adoptions` | `skin_asset_adoptions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `skin_asset_adoptions` | `skin_asset_adoptions_user_id_not_null` | `NOT NULL user_id` |
| `skin_assets` | `fk_skin_assets_published_revision` | `FOREIGN KEY (published_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `skin_assets` | `skin_assets_blob_hash_fkey` | `FOREIGN KEY (blob_hash) REFERENCES skin_texture_blobs(hash) ON DELETE RESTRICT` |
| `skin_assets` | `skin_assets_blob_hash_not_null` | `NOT NULL blob_hash` |
| `skin_assets` | `skin_assets_created_at_not_null` | `NOT NULL created_at` |
| `skin_assets` | `skin_assets_description_not_null` | `NOT NULL description` |
| `skin_assets` | `skin_assets_display_name_not_null` | `NOT NULL display_name` |
| `skin_assets` | `skin_assets_downloads_check` | `CHECK (downloads >= 0)` |
| `skin_assets` | `skin_assets_downloads_not_null` | `NOT NULL downloads` |
| `skin_assets` | `skin_assets_id_not_null` | `NOT NULL id` |
| `skin_assets` | `skin_assets_kind_check` | `CHECK (kind = ANY (ARRAY['skin'::text, 'cape'::text]))` |
| `skin_assets` | `skin_assets_kind_not_null` | `NOT NULL kind` |
| `skin_assets` | `skin_assets_model_check` | `CHECK (model = ANY (ARRAY['default'::text, 'slim'::text]))` |
| `skin_assets` | `skin_assets_model_not_null` | `NOT NULL model` |
| `skin_assets` | `skin_assets_owner_id_fkey` | `FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE RESTRICT` |
| `skin_assets` | `skin_assets_owner_id_not_null` | `NOT NULL owner_id` |
| `skin_assets` | `skin_assets_pkey` | `PRIMARY KEY (id)` |
| `skin_assets` | `skin_assets_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `skin_assets` | `skin_assets_public_id_key` | `UNIQUE (public_id)` |
| `skin_assets` | `skin_assets_public_id_not_null` | `NOT NULL public_id` |
| `skin_assets` | `skin_assets_review_status_check` | `CHECK (review_status = ANY (ARRAY['approved'::text, 'pending'::text, 'rejected'::text]))` |
| `skin_assets` | `skin_assets_review_status_not_null` | `NOT NULL review_status` |
| `skin_assets` | `skin_assets_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'deleted'::text]))` |
| `skin_assets` | `skin_assets_status_not_null` | `NOT NULL status` |
| `skin_assets` | `skin_assets_tags_not_null` | `NOT NULL tags` |
| `skin_assets` | `skin_assets_updated_at_not_null` | `NOT NULL updated_at` |
| `skin_assets` | `skin_assets_visibility_check` | `CHECK (visibility = ANY (ARRAY['public'::text, 'unlisted'::text, 'private'::text]))` |
| `skin_assets` | `skin_assets_visibility_not_null` | `NOT NULL visibility` |
| `skin_texture_blobs` | `skin_texture_blobs_created_at_not_null` | `NOT NULL created_at` |
| `skin_texture_blobs` | `skin_texture_blobs_hash_check` | `CHECK (hash ~ '^[0-9a-f]{64}$'::text)` |
| `skin_texture_blobs` | `skin_texture_blobs_hash_not_null` | `NOT NULL hash` |
| `skin_texture_blobs` | `skin_texture_blobs_height_check` | `CHECK (height > 0)` |
| `skin_texture_blobs` | `skin_texture_blobs_height_not_null` | `NOT NULL height` |
| `skin_texture_blobs` | `skin_texture_blobs_object_key_key` | `UNIQUE (object_key)` |
| `skin_texture_blobs` | `skin_texture_blobs_object_key_not_null` | `NOT NULL object_key` |
| `skin_texture_blobs` | `skin_texture_blobs_oss_file_id_fkey` | `FOREIGN KEY (oss_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `skin_texture_blobs` | `skin_texture_blobs_oss_file_id_key` | `UNIQUE (oss_file_id)` |
| `skin_texture_blobs` | `skin_texture_blobs_oss_file_id_not_null` | `NOT NULL oss_file_id` |
| `skin_texture_blobs` | `skin_texture_blobs_pkey` | `PRIMARY KEY (hash)` |
| `skin_texture_blobs` | `skin_texture_blobs_size_bytes_check` | `CHECK (size_bytes > 0)` |
| `skin_texture_blobs` | `skin_texture_blobs_size_bytes_not_null` | `NOT NULL size_bytes` |
| `skin_texture_blobs` | `skin_texture_blobs_width_check` | `CHECK (width > 0)` |
| `skin_texture_blobs` | `skin_texture_blobs_width_not_null` | `NOT NULL width` |
| `skin_wardrobe` | `skin_wardrobe_added_at_not_null` | `NOT NULL added_at` |
| `skin_wardrobe` | `skin_wardrobe_asset_id_fkey` | `FOREIGN KEY (asset_id) REFERENCES skin_assets(id) ON DELETE CASCADE` |
| `skin_wardrobe` | `skin_wardrobe_asset_id_not_null` | `NOT NULL asset_id` |
| `skin_wardrobe` | `skin_wardrobe_pkey` | `PRIMARY KEY (user_id, asset_id)` |
| `skin_wardrobe` | `skin_wardrobe_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `skin_wardrobe` | `skin_wardrobe_user_id_not_null` | `NOT NULL user_id` |
| `sticker_catalog_state` | `sticker_catalog_state_pkey` | `PRIMARY KEY (singleton)` |
| `sticker_catalog_state` | `sticker_catalog_state_singleton_check` | `CHECK (singleton)` |
| `sticker_catalog_state` | `sticker_catalog_state_singleton_not_null` | `NOT NULL singleton` |
| `sticker_catalog_state` | `sticker_catalog_state_updated_at_not_null` | `NOT NULL updated_at` |
| `sticker_catalog_state` | `sticker_catalog_state_version_not_null` | `NOT NULL version` |
| `sticker_pack_translations` | `sticker_pack_translations_locale_not_null` | `NOT NULL locale` |
| `sticker_pack_translations` | `sticker_pack_translations_name_check` | `CHECK (length(btrim(name)) >= 1 AND length(btrim(name)) <= 80)` |
| `sticker_pack_translations` | `sticker_pack_translations_name_not_null` | `NOT NULL name` |
| `sticker_pack_translations` | `sticker_pack_translations_pack_id_fkey` | `FOREIGN KEY (pack_id) REFERENCES sticker_packs(id) ON DELETE CASCADE` |
| `sticker_pack_translations` | `sticker_pack_translations_pack_id_not_null` | `NOT NULL pack_id` |
| `sticker_pack_translations` | `sticker_pack_translations_pkey` | `PRIMARY KEY (pack_id, locale)` |
| `sticker_packs` | `sticker_packs_code_check` | `CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,47}$'::text)` |
| `sticker_packs` | `sticker_packs_code_key` | `UNIQUE (code)` |
| `sticker_packs` | `sticker_packs_code_not_null` | `NOT NULL code` |
| `sticker_packs` | `sticker_packs_created_at_not_null` | `NOT NULL created_at` |
| `sticker_packs` | `sticker_packs_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `sticker_packs` | `sticker_packs_id_not_null` | `NOT NULL id` |
| `sticker_packs` | `sticker_packs_pkey` | `PRIMARY KEY (id)` |
| `sticker_packs` | `sticker_packs_sort_order_not_null` | `NOT NULL sort_order` |
| `sticker_packs` | `sticker_packs_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'disabled'::text]))` |
| `sticker_packs` | `sticker_packs_status_not_null` | `NOT NULL status` |
| `sticker_packs` | `sticker_packs_updated_at_not_null` | `NOT NULL updated_at` |
| `sticker_translations` | `sticker_translations_locale_not_null` | `NOT NULL locale` |
| `sticker_translations` | `sticker_translations_name_check` | `CHECK (length(btrim(name)) >= 1 AND length(btrim(name)) <= 80)` |
| `sticker_translations` | `sticker_translations_name_not_null` | `NOT NULL name` |
| `sticker_translations` | `sticker_translations_pkey` | `PRIMARY KEY (sticker_id, locale)` |
| `sticker_translations` | `sticker_translations_sticker_id_fkey` | `FOREIGN KEY (sticker_id) REFERENCES stickers(id) ON DELETE CASCADE` |
| `sticker_translations` | `sticker_translations_sticker_id_not_null` | `NOT NULL sticker_id` |
| `stickers` | `stickers_check` | `CHECK (width > 0 AND height > 0 AND file_size > 0)` |
| `stickers` | `stickers_checksum_not_null` | `NOT NULL checksum` |
| `stickers` | `stickers_code_check` | `CHECK (code ~ '^[a-z0-9][a-z0-9_-]{0,47}$'::text)` |
| `stickers` | `stickers_code_not_null` | `NOT NULL code` |
| `stickers` | `stickers_created_at_not_null` | `NOT NULL created_at` |
| `stickers` | `stickers_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `stickers` | `stickers_file_size_not_null` | `NOT NULL file_size` |
| `stickers` | `stickers_height_not_null` | `NOT NULL height` |
| `stickers` | `stickers_id_not_null` | `NOT NULL id` |
| `stickers` | `stickers_image_file_id_fkey` | `FOREIGN KEY (image_file_id) REFERENCES oss_files(id) ON DELETE RESTRICT` |
| `stickers` | `stickers_image_file_id_not_null` | `NOT NULL image_file_id` |
| `stickers` | `stickers_mime_type_check` | `CHECK (mime_type = ANY (ARRAY['image/png'::text, 'image/gif'::text]))` |
| `stickers` | `stickers_mime_type_not_null` | `NOT NULL mime_type` |
| `stickers` | `stickers_pack_id_code_key` | `UNIQUE (pack_id, code)` |
| `stickers` | `stickers_pack_id_fkey` | `FOREIGN KEY (pack_id) REFERENCES sticker_packs(id) ON DELETE CASCADE` |
| `stickers` | `stickers_pack_id_not_null` | `NOT NULL pack_id` |
| `stickers` | `stickers_pkey` | `PRIMARY KEY (id)` |
| `stickers` | `stickers_sort_order_not_null` | `NOT NULL sort_order` |
| `stickers` | `stickers_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'disabled'::text]))` |
| `stickers` | `stickers_status_not_null` | `NOT NULL status` |
| `stickers` | `stickers_updated_at_not_null` | `NOT NULL updated_at` |
| `stickers` | `stickers_width_not_null` | `NOT NULL width` |
| `system_settings` | `system_settings_key_not_null` | `NOT NULL key` |
| `system_settings` | `system_settings_pkey` | `PRIMARY KEY (key)` |
| `system_settings` | `system_settings_updated_at_not_null` | `NOT NULL updated_at` |
| `system_settings` | `system_settings_updated_by_fkey` | `FOREIGN KEY (updated_by) REFERENCES users(id) ON DELETE SET NULL` |
| `system_settings` | `system_settings_value_not_null` | `NOT NULL value` |
| `tag_import_members` | `tag_import_members_ordinal_not_null` | `NOT NULL ordinal` |
| `tag_import_members` | `tag_import_members_pkey` | `PRIMARY KEY (tag_snapshot_id, raw_member_id)` |
| `tag_import_members` | `tag_import_members_raw_member_id_not_null` | `NOT NULL raw_member_id` |
| `tag_import_members` | `tag_import_members_resource_id_fkey` | `FOREIGN KEY (resource_id) REFERENCES game_resources(entity_id) ON DELETE SET NULL` |
| `tag_import_members` | `tag_import_members_tag_snapshot_id_fkey` | `FOREIGN KEY (tag_snapshot_id) REFERENCES tag_import_snapshots(id) ON DELETE CASCADE` |
| `tag_import_members` | `tag_import_members_tag_snapshot_id_not_null` | `NOT NULL tag_snapshot_id` |
| `tag_import_snapshots` | `tag_import_snapshots_id_not_null` | `NOT NULL id` |
| `tag_import_snapshots` | `tag_import_snapshots_member_count_not_null` | `NOT NULL member_count` |
| `tag_import_snapshots` | `tag_import_snapshots_pkey` | `PRIMARY KEY (id)` |
| `tag_import_snapshots` | `tag_import_snapshots_revision_id_fkey` | `FOREIGN KEY (revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `tag_import_snapshots` | `tag_import_snapshots_revision_id_not_null` | `NOT NULL revision_id` |
| `tag_import_snapshots` | `tag_import_snapshots_tag_id_fkey` | `FOREIGN KEY (tag_id) REFERENCES catalog_tags(entity_id) ON DELETE CASCADE` |
| `tag_import_snapshots` | `tag_import_snapshots_tag_id_not_null` | `NOT NULL tag_id` |
| `tag_import_snapshots` | `tag_import_snapshots_tag_id_revision_id_key` | `UNIQUE (tag_id, revision_id)` |
| `task_definitions` | `task_definitions_code_key` | `UNIQUE (code)` |
| `task_definitions` | `task_definitions_code_not_null` | `NOT NULL code` |
| `task_definitions` | `task_definitions_condition_not_null` | `NOT NULL condition` |
| `task_definitions` | `task_definitions_created_at_not_null` | `NOT NULL created_at` |
| `task_definitions` | `task_definitions_created_by_fkey` | `FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE SET NULL` |
| `task_definitions` | `task_definitions_description_not_null` | `NOT NULL description` |
| `task_definitions` | `task_definitions_icon_not_null` | `NOT NULL icon` |
| `task_definitions` | `task_definitions_id_not_null` | `NOT NULL id` |
| `task_definitions` | `task_definitions_name_not_null` | `NOT NULL name` |
| `task_definitions` | `task_definitions_pkey` | `PRIMARY KEY (id)` |
| `task_definitions` | `task_definitions_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `task_definitions` | `task_definitions_public_id_key` | `UNIQUE (public_id)` |
| `task_definitions` | `task_definitions_public_id_not_null` | `NOT NULL public_id` |
| `task_definitions` | `task_definitions_refresh_period_check` | `CHECK (refresh_period = ANY (ARRAY['never'::text, 'daily'::text, 'weekly'::text, 'monthly'::text]))` |
| `task_definitions` | `task_definitions_refresh_period_not_null` | `NOT NULL refresh_period` |
| `task_definitions` | `task_definitions_rewards_not_null` | `NOT NULL rewards` |
| `task_definitions` | `task_definitions_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'disabled'::text]))` |
| `task_definitions` | `task_definitions_status_not_null` | `NOT NULL status` |
| `task_definitions` | `task_definitions_translations_not_null` | `NOT NULL translations` |
| `task_definitions` | `task_definitions_updated_at_not_null` | `NOT NULL updated_at` |
| `unresolved_references` | `unresolved_references_check` | `CHECK (raw_identifier <> ''::text AND normalized_identifier <> ''::text)` |
| `unresolved_references` | `unresolved_references_created_at_not_null` | `NOT NULL created_at` |
| `unresolved_references` | `unresolved_references_field_path_not_null` | `NOT NULL field_path` |
| `unresolved_references` | `unresolved_references_id_not_null` | `NOT NULL id` |
| `unresolved_references` | `unresolved_references_metadata_not_null` | `NOT NULL metadata` |
| `unresolved_references` | `unresolved_references_normalized_identifier_not_null` | `NOT NULL normalized_identifier` |
| `unresolved_references` | `unresolved_references_pkey` | `PRIMARY KEY (id)` |
| `unresolved_references` | `unresolved_references_raw_identifier_not_null` | `NOT NULL raw_identifier` |
| `unresolved_references` | `unresolved_references_reference_type_not_null` | `NOT NULL reference_type` |
| `unresolved_references` | `unresolved_references_resolved_type_not_null` | `NOT NULL resolved_type` |
| `unresolved_references` | `unresolved_references_source_id_not_null` | `NOT NULL source_id` |
| `unresolved_references` | `unresolved_references_source_type_not_null` | `NOT NULL source_type` |
| `unresolved_references` | `unresolved_references_source_type_source_id_field_path_refe_key` | `UNIQUE (source_type, source_id, field_path, reference_type, normalized_identifier)` |
| `unresolved_references` | `unresolved_references_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'resolved'::text, 'ignored'::text]))` |
| `unresolved_references` | `unresolved_references_status_not_null` | `NOT NULL status` |
| `unresolved_references` | `unresolved_references_updated_at_not_null` | `NOT NULL updated_at` |
| `unresolved_resource_references` | `unresolved_resource_reference_source_entity_id_source_revis_key` | `UNIQUE (source_entity_id, source_revision_id, field_path, kind_code, raw_resource_id)` |
| `unresolved_resource_references` | `unresolved_resource_references_created_at_not_null` | `NOT NULL created_at` |
| `unresolved_resource_references` | `unresolved_resource_references_field_path_not_null` | `NOT NULL field_path` |
| `unresolved_resource_references` | `unresolved_resource_references_id_not_null` | `NOT NULL id` |
| `unresolved_resource_references` | `unresolved_resource_references_kind_code_not_null` | `NOT NULL kind_code` |
| `unresolved_resource_references` | `unresolved_resource_references_pkey` | `PRIMARY KEY (id)` |
| `unresolved_resource_references` | `unresolved_resource_references_raw_resource_id_not_null` | `NOT NULL raw_resource_id` |
| `unresolved_resource_references` | `unresolved_resource_references_resolved_resource_id_fkey` | `FOREIGN KEY (resolved_resource_id) REFERENCES game_resources(entity_id) ON DELETE SET NULL` |
| `unresolved_resource_references` | `unresolved_resource_references_source_entity_id_fkey` | `FOREIGN KEY (source_entity_id) REFERENCES catalog_entities(id) ON DELETE CASCADE` |
| `unresolved_resource_references` | `unresolved_resource_references_source_entity_id_not_null` | `NOT NULL source_entity_id` |
| `unresolved_resource_references` | `unresolved_resource_references_source_revision_id_fkey` | `FOREIGN KEY (source_revision_id) REFERENCES catalog_import_revisions(id) ON DELETE CASCADE` |
| `unresolved_resource_references` | `unresolved_resource_references_status_check` | `CHECK (status = ANY (ARRAY['pending'::text, 'resolved'::text, 'ignored'::text]))` |
| `unresolved_resource_references` | `unresolved_resource_references_status_not_null` | `NOT NULL status` |
| `user_activity_events` | `user_activity_events_action_id_fkey` | `FOREIGN KEY (action_id) REFERENCES activity_actions(id) ON DELETE RESTRICT` |
| `user_activity_events` | `user_activity_events_action_id_not_null` | `NOT NULL action_id` |
| `user_activity_events` | `user_activity_events_id_not_null` | `NOT NULL id` |
| `user_activity_events` | `user_activity_events_markdown_added_bytes_check` | `CHECK (markdown_added_bytes >= 0)` |
| `user_activity_events` | `user_activity_events_markdown_added_bytes_not_null` | `NOT NULL markdown_added_bytes` |
| `user_activity_events` | `user_activity_events_markdown_deleted_bytes_check` | `CHECK (markdown_deleted_bytes >= 0)` |
| `user_activity_events` | `user_activity_events_markdown_deleted_bytes_not_null` | `NOT NULL markdown_deleted_bytes` |
| `user_activity_events` | `user_activity_events_object_route_id_fkey` | `FOREIGN KEY (object_route_id) REFERENCES public_routes(id) ON DELETE SET NULL` |
| `user_activity_events` | `user_activity_events_object_type_id_fkey` | `FOREIGN KEY (object_type_id) REFERENCES activity_object_types(id) ON DELETE RESTRICT` |
| `user_activity_events` | `user_activity_events_object_type_id_not_null` | `NOT NULL object_type_id` |
| `user_activity_events` | `user_activity_events_occurred_at_not_null` | `NOT NULL occurred_at` |
| `user_activity_events` | `user_activity_events_pkey` | `PRIMARY KEY (id)` |
| `user_activity_events` | `user_activity_events_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `user_blocks` | `user_blocks_blocked_id_fkey` | `FOREIGN KEY (blocked_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_blocks` | `user_blocks_blocked_id_not_null` | `NOT NULL blocked_id` |
| `user_blocks` | `user_blocks_blocker_id_fkey` | `FOREIGN KEY (blocker_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_blocks` | `user_blocks_blocker_id_not_null` | `NOT NULL blocker_id` |
| `user_blocks` | `user_blocks_check` | `CHECK (blocker_id <> blocked_id)` |
| `user_blocks` | `user_blocks_created_at_not_null` | `NOT NULL created_at` |
| `user_blocks` | `user_blocks_pkey` | `PRIMARY KEY (blocker_id, blocked_id)` |
| `user_chat_presence` | `user_chat_presence_conversation_id_fkey` | `FOREIGN KEY (conversation_id) REFERENCES direct_conversations(id) ON DELETE CASCADE` |
| `user_chat_presence` | `user_chat_presence_conversation_id_not_null` | `NOT NULL conversation_id` |
| `user_chat_presence` | `user_chat_presence_expires_at_not_null` | `NOT NULL expires_at` |
| `user_chat_presence` | `user_chat_presence_pkey` | `PRIMARY KEY (user_id)` |
| `user_chat_presence` | `user_chat_presence_updated_at_not_null` | `NOT NULL updated_at` |
| `user_chat_presence` | `user_chat_presence_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_chat_presence` | `user_chat_presence_user_id_not_null` | `NOT NULL user_id` |
| `user_checkins` | `user_checkins_claimed_at_not_null` | `NOT NULL claimed_at` |
| `user_checkins` | `user_checkins_local_date_not_null` | `NOT NULL local_date` |
| `user_checkins` | `user_checkins_pkey` | `PRIMARY KEY (user_id, local_date)` |
| `user_checkins` | `user_checkins_timezone_not_null` | `NOT NULL timezone` |
| `user_checkins` | `user_checkins_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_checkins` | `user_checkins_user_id_not_null` | `NOT NULL user_id` |
| `user_content_creation_facts` | `user_content_creation_facts_content_type_not_null` | `NOT NULL content_type` |
| `user_content_creation_facts` | `user_content_creation_facts_created_at_not_null` | `NOT NULL created_at` |
| `user_content_creation_facts` | `user_content_creation_facts_current_exists_not_null` | `NOT NULL current_exists` |
| `user_content_creation_facts` | `user_content_creation_facts_object_key_not_null` | `NOT NULL object_key` |
| `user_content_creation_facts` | `user_content_creation_facts_pkey` | `PRIMARY KEY (content_type, object_key)` |
| `user_content_creation_facts` | `user_content_creation_facts_review_status_check` | `CHECK (review_status = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))` |
| `user_content_creation_facts` | `user_content_creation_facts_review_status_not_null` | `NOT NULL review_status` |
| `user_content_creation_facts` | `user_content_creation_facts_updated_at_not_null` | `NOT NULL updated_at` |
| `user_content_creation_facts` | `user_content_creation_facts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_content_creation_facts` | `user_content_creation_facts_user_id_not_null` | `NOT NULL user_id` |
| `user_currency_balances` | `user_currency_balances_balance_check` | `CHECK (balance >= 0)` |
| `user_currency_balances` | `user_currency_balances_balance_not_null` | `NOT NULL balance` |
| `user_currency_balances` | `user_currency_balances_currency_id_fkey` | `FOREIGN KEY (currency_id) REFERENCES currencies(id) ON DELETE RESTRICT` |
| `user_currency_balances` | `user_currency_balances_currency_id_not_null` | `NOT NULL currency_id` |
| `user_currency_balances` | `user_currency_balances_pkey` | `PRIMARY KEY (user_id, currency_id)` |
| `user_currency_balances` | `user_currency_balances_updated_at_not_null` | `NOT NULL updated_at` |
| `user_currency_balances` | `user_currency_balances_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_currency_balances` | `user_currency_balances_user_id_not_null` | `NOT NULL user_id` |
| `user_daily_contributions` | `user_daily_contributions_contribution_count_check` | `CHECK (contribution_count > 0)` |
| `user_daily_contributions` | `user_daily_contributions_contribution_count_not_null` | `NOT NULL contribution_count` |
| `user_daily_contributions` | `user_daily_contributions_contribution_date_not_null` | `NOT NULL contribution_date` |
| `user_daily_contributions` | `user_daily_contributions_pkey` | `PRIMARY KEY (user_id, contribution_date)` |
| `user_daily_contributions` | `user_daily_contributions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_daily_contributions` | `user_daily_contributions_user_id_not_null` | `NOT NULL user_id` |
| `user_drafts` | `user_drafts_change_request_id_fkey` | `FOREIGN KEY (change_request_id) REFERENCES change_requests(id) ON DELETE SET NULL` |
| `user_drafts` | `user_drafts_check` | `CHECK (submitted_at IS NULL AND submitted_status = ''::text OR submitted_at IS NOT NULL AND submitted_status <> ''::text)` |
| `user_drafts` | `user_drafts_created_at_not_null` | `NOT NULL created_at` |
| `user_drafts` | `user_drafts_draft_key_check` | `CHECK (length(draft_key) >= 1 AND length(draft_key) <= 255)` |
| `user_drafts` | `user_drafts_draft_key_not_null` | `NOT NULL draft_key` |
| `user_drafts` | `user_drafts_edit_url_check` | `CHECK (length(edit_url) >= 1 AND length(edit_url) <= 1000)` |
| `user_drafts` | `user_drafts_edit_url_not_null` | `NOT NULL edit_url` |
| `user_drafts` | `user_drafts_expires_at_not_null` | `NOT NULL expires_at` |
| `user_drafts` | `user_drafts_id_not_null` | `NOT NULL id` |
| `user_drafts` | `user_drafts_kind_check` | `CHECK (length(kind) >= 1 AND length(kind) <= 64)` |
| `user_drafts` | `user_drafts_kind_not_null` | `NOT NULL kind` |
| `user_drafts` | `user_drafts_payload_check` | `CHECK (jsonb_typeof(payload) = 'object'::text)` |
| `user_drafts` | `user_drafts_payload_not_null` | `NOT NULL payload` |
| `user_drafts` | `user_drafts_pkey` | `PRIMARY KEY (id)` |
| `user_drafts` | `user_drafts_project_key_check` | `CHECK (length(project_key) >= 1 AND length(project_key) <= 255)` |
| `user_drafts` | `user_drafts_project_key_not_null` | `NOT NULL project_key` |
| `user_drafts` | `user_drafts_project_title_check` | `CHECK (length(project_title) <= 200)` |
| `user_drafts` | `user_drafts_project_title_not_null` | `NOT NULL project_title` |
| `user_drafts` | `user_drafts_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `user_drafts` | `user_drafts_public_id_key` | `UNIQUE (public_id)` |
| `user_drafts` | `user_drafts_public_id_not_null` | `NOT NULL public_id` |
| `user_drafts` | `user_drafts_review_target_id_fkey` | `FOREIGN KEY (review_target_id) REFERENCES minecraft_servers(id) ON DELETE SET NULL` |
| `user_drafts` | `user_drafts_review_target_type_check` | `CHECK (review_target_type = ANY (ARRAY[''::text, 'server'::text]))` |
| `user_drafts` | `user_drafts_review_target_type_not_null` | `NOT NULL review_target_type` |
| `user_drafts` | `user_drafts_submitted_status_check` | `CHECK (submitted_status = ANY (ARRAY[''::text, 'pending'::text, 'approved'::text]))` |
| `user_drafts` | `user_drafts_submitted_status_not_null` | `NOT NULL submitted_status` |
| `user_drafts` | `user_drafts_target_url_check` | `CHECK (length(target_url) <= 1000)` |
| `user_drafts` | `user_drafts_target_url_not_null` | `NOT NULL target_url` |
| `user_drafts` | `user_drafts_title_check` | `CHECK (length(title) <= 200)` |
| `user_drafts` | `user_drafts_title_not_null` | `NOT NULL title` |
| `user_drafts` | `user_drafts_updated_at_not_null` | `NOT NULL updated_at` |
| `user_drafts` | `user_drafts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_drafts` | `user_drafts_user_id_not_null` | `NOT NULL user_id` |
| `user_experience` | `user_experience_experience_check` | `CHECK (experience >= 0)` |
| `user_experience` | `user_experience_experience_not_null` | `NOT NULL experience` |
| `user_experience` | `user_experience_level_check` | `CHECK (level >= 0)` |
| `user_experience` | `user_experience_level_not_null` | `NOT NULL level` |
| `user_experience` | `user_experience_pkey` | `PRIMARY KEY (user_id)` |
| `user_experience` | `user_experience_updated_at_not_null` | `NOT NULL updated_at` |
| `user_experience` | `user_experience_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_experience` | `user_experience_user_id_not_null` | `NOT NULL user_id` |
| `user_follows` | `user_follows_check` | `CHECK (follower_id <> followed_id)` |
| `user_follows` | `user_follows_created_at_not_null` | `NOT NULL created_at` |
| `user_follows` | `user_follows_followed_id_fkey` | `FOREIGN KEY (followed_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_follows` | `user_follows_followed_id_not_null` | `NOT NULL followed_id` |
| `user_follows` | `user_follows_follower_id_fkey` | `FOREIGN KEY (follower_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_follows` | `user_follows_follower_id_not_null` | `NOT NULL follower_id` |
| `user_follows` | `user_follows_pkey` | `PRIMARY KEY (follower_id, followed_id)` |
| `user_inventory` | `user_inventory_pkey` | `PRIMARY KEY (user_id, shop_item_id)` |
| `user_inventory` | `user_inventory_quantity_check` | `CHECK (quantity >= 0)` |
| `user_inventory` | `user_inventory_quantity_not_null` | `NOT NULL quantity` |
| `user_inventory` | `user_inventory_shop_item_id_fkey` | `FOREIGN KEY (shop_item_id) REFERENCES shop_items(id) ON DELETE RESTRICT` |
| `user_inventory` | `user_inventory_shop_item_id_not_null` | `NOT NULL shop_item_id` |
| `user_inventory` | `user_inventory_updated_at_not_null` | `NOT NULL updated_at` |
| `user_inventory` | `user_inventory_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_inventory` | `user_inventory_user_id_not_null` | `NOT NULL user_id` |
| `user_login_logs` | `user_login_logs_account_not_null` | `NOT NULL account` |
| `user_login_logs` | `user_login_logs_city_not_null` | `NOT NULL city` |
| `user_login_logs` | `user_login_logs_country_code_not_null` | `NOT NULL country_code` |
| `user_login_logs` | `user_login_logs_created_at_not_null` | `NOT NULL created_at` |
| `user_login_logs` | `user_login_logs_id_not_null` | `NOT NULL id` |
| `user_login_logs` | `user_login_logs_ip_not_null` | `NOT NULL ip` |
| `user_login_logs` | `user_login_logs_pkey` | `PRIMARY KEY (id)` |
| `user_login_logs` | `user_login_logs_reason_not_null` | `NOT NULL reason` |
| `user_login_logs` | `user_login_logs_success_not_null` | `NOT NULL success` |
| `user_login_logs` | `user_login_logs_user_agent_not_null` | `NOT NULL user_agent` |
| `user_login_logs` | `user_login_logs_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL` |
| `user_notification_settings` | `user_notification_settings_email_enabled_not_null` | `NOT NULL email_enabled` |
| `user_notification_settings` | `user_notification_settings_pkey` | `PRIMARY KEY (user_id)` |
| `user_notification_settings` | `user_notification_settings_project_updates_enabled_not_null` | `NOT NULL project_updates_enabled` |
| `user_notification_settings` | `user_notification_settings_updated_at_not_null` | `NOT NULL updated_at` |
| `user_notification_settings` | `user_notification_settings_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_notification_settings` | `user_notification_settings_user_id_not_null` | `NOT NULL user_id` |
| `user_permissions` | `user_permissions_allow_not_null` | `NOT NULL allow` |
| `user_permissions` | `user_permissions_context_not_null` | `NOT NULL context` |
| `user_permissions` | `user_permissions_created_at_not_null` | `NOT NULL created_at` |
| `user_permissions` | `user_permissions_permission_id_fkey` | `FOREIGN KEY (permission_id) REFERENCES permissions(id) ON DELETE CASCADE` |
| `user_permissions` | `user_permissions_permission_id_not_null` | `NOT NULL permission_id` |
| `user_permissions` | `user_permissions_pkey` | `PRIMARY KEY (user_id, permission_id)` |
| `user_permissions` | `user_permissions_updated_at_not_null` | `NOT NULL updated_at` |
| `user_permissions` | `user_permissions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_permissions` | `user_permissions_user_id_not_null` | `NOT NULL user_id` |
| `user_presence_sessions` | `user_presence_sessions_last_active_at_not_null` | `NOT NULL last_active_at` |
| `user_presence_sessions` | `user_presence_sessions_pkey` | `PRIMARY KEY (session_hash)` |
| `user_presence_sessions` | `user_presence_sessions_session_hash_fkey` | `FOREIGN KEY (session_hash) REFERENCES auth_sessions(session_hash) ON DELETE CASCADE` |
| `user_presence_sessions` | `user_presence_sessions_session_hash_not_null` | `NOT NULL session_hash` |
| `user_presence_sessions` | `user_presence_sessions_updated_at_not_null` | `NOT NULL updated_at` |
| `user_presence_sessions` | `user_presence_sessions_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_presence_sessions` | `user_presence_sessions_user_id_not_null` | `NOT NULL user_id` |
| `user_registration_attempts` | `user_registration_attempts_created_at_not_null` | `NOT NULL created_at` |
| `user_registration_attempts` | `user_registration_attempts_id_not_null` | `NOT NULL id` |
| `user_registration_attempts` | `user_registration_attempts_ip_not_null` | `NOT NULL ip` |
| `user_registration_attempts` | `user_registration_attempts_pkey` | `PRIMARY KEY (id)` |
| `user_role_bindings` | `user_role_bindings_context_not_null` | `NOT NULL context` |
| `user_role_bindings` | `user_role_bindings_created_at_not_null` | `NOT NULL created_at` |
| `user_role_bindings` | `user_role_bindings_pkey` | `PRIMARY KEY (user_id, role_id)` |
| `user_role_bindings` | `user_role_bindings_role_id_fkey` | `FOREIGN KEY (role_id) REFERENCES roles(id) ON DELETE CASCADE` |
| `user_role_bindings` | `user_role_bindings_role_id_not_null` | `NOT NULL role_id` |
| `user_role_bindings` | `user_role_bindings_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_role_bindings` | `user_role_bindings_user_id_not_null` | `NOT NULL user_id` |
| `user_statistics_daily` | `user_statistics_daily_action_count_check` | `CHECK (action_count >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_action_count_not_null` | `NOT NULL action_count` |
| `user_statistics_daily` | `user_statistics_daily_action_counts_not_null` | `NOT NULL action_counts` |
| `user_statistics_daily` | `user_statistics_daily_create_count_check` | `CHECK (create_count >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_create_count_not_null` | `NOT NULL create_count` |
| `user_statistics_daily` | `user_statistics_daily_delete_count_check` | `CHECK (delete_count >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_delete_count_not_null` | `NOT NULL delete_count` |
| `user_statistics_daily` | `user_statistics_daily_edit_count_check` | `CHECK (edit_count >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_edit_count_not_null` | `NOT NULL edit_count` |
| `user_statistics_daily` | `user_statistics_daily_markdown_added_bytes_check` | `CHECK (markdown_added_bytes >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_markdown_added_bytes_not_null` | `NOT NULL markdown_added_bytes` |
| `user_statistics_daily` | `user_statistics_daily_markdown_deleted_bytes_check` | `CHECK (markdown_deleted_bytes >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_markdown_deleted_bytes_not_null` | `NOT NULL markdown_deleted_bytes` |
| `user_statistics_daily` | `user_statistics_daily_pkey` | `PRIMARY KEY (user_id, stat_date)` |
| `user_statistics_daily` | `user_statistics_daily_stat_date_not_null` | `NOT NULL stat_date` |
| `user_statistics_daily` | `user_statistics_daily_updated_at_not_null` | `NOT NULL updated_at` |
| `user_statistics_daily` | `user_statistics_daily_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_statistics_daily` | `user_statistics_daily_user_id_not_null` | `NOT NULL user_id` |
| `user_statistics_daily` | `user_statistics_daily_view_count_check` | `CHECK (view_count >= 0)` |
| `user_statistics_daily` | `user_statistics_daily_view_count_not_null` | `NOT NULL view_count` |
| `user_statistics_totals` | `user_statistics_totals_action_count_check` | `CHECK (action_count >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_action_count_not_null` | `NOT NULL action_count` |
| `user_statistics_totals` | `user_statistics_totals_action_counts_not_null` | `NOT NULL action_counts` |
| `user_statistics_totals` | `user_statistics_totals_create_count_check` | `CHECK (create_count >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_create_count_not_null` | `NOT NULL create_count` |
| `user_statistics_totals` | `user_statistics_totals_delete_count_check` | `CHECK (delete_count >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_delete_count_not_null` | `NOT NULL delete_count` |
| `user_statistics_totals` | `user_statistics_totals_edit_count_check` | `CHECK (edit_count >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_edit_count_not_null` | `NOT NULL edit_count` |
| `user_statistics_totals` | `user_statistics_totals_markdown_added_bytes_check` | `CHECK (markdown_added_bytes >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_markdown_added_bytes_not_null` | `NOT NULL markdown_added_bytes` |
| `user_statistics_totals` | `user_statistics_totals_markdown_deleted_bytes_check` | `CHECK (markdown_deleted_bytes >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_markdown_deleted_bytes_not_null` | `NOT NULL markdown_deleted_bytes` |
| `user_statistics_totals` | `user_statistics_totals_pkey` | `PRIMARY KEY (user_id)` |
| `user_statistics_totals` | `user_statistics_totals_updated_at_not_null` | `NOT NULL updated_at` |
| `user_statistics_totals` | `user_statistics_totals_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_statistics_totals` | `user_statistics_totals_user_id_not_null` | `NOT NULL user_id` |
| `user_statistics_totals` | `user_statistics_totals_view_count_check` | `CHECK (view_count >= 0)` |
| `user_statistics_totals` | `user_statistics_totals_view_count_not_null` | `NOT NULL view_count` |
| `user_task_progress` | `user_task_progress_period_key_not_null` | `NOT NULL period_key` |
| `user_task_progress` | `user_task_progress_pkey` | `PRIMARY KEY (user_id, task_id, period_key)` |
| `user_task_progress` | `user_task_progress_progress_check` | `CHECK (progress >= 0)` |
| `user_task_progress` | `user_task_progress_progress_not_null` | `NOT NULL progress` |
| `user_task_progress` | `user_task_progress_task_id_fkey` | `FOREIGN KEY (task_id) REFERENCES task_definitions(id) ON DELETE CASCADE` |
| `user_task_progress` | `user_task_progress_task_id_not_null` | `NOT NULL task_id` |
| `user_task_progress` | `user_task_progress_updated_at_not_null` | `NOT NULL updated_at` |
| `user_task_progress` | `user_task_progress_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_task_progress` | `user_task_progress_user_id_not_null` | `NOT NULL user_id` |
| `user_timezone_changes` | `user_timezone_changes_changed_at_not_null` | `NOT NULL changed_at` |
| `user_timezone_changes` | `user_timezone_changes_id_not_null` | `NOT NULL id` |
| `user_timezone_changes` | `user_timezone_changes_new_timezone_not_null` | `NOT NULL new_timezone` |
| `user_timezone_changes` | `user_timezone_changes_old_timezone_not_null` | `NOT NULL old_timezone` |
| `user_timezone_changes` | `user_timezone_changes_pkey` | `PRIMARY KEY (id)` |
| `user_timezone_changes` | `user_timezone_changes_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `user_timezone_changes` | `user_timezone_changes_user_id_not_null` | `NOT NULL user_id` |
| `users` | `fk_users_avatar_file` | `FOREIGN KEY (avatar_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `users` | `fk_users_profile_revision` | `FOREIGN KEY (profile_revision_id) REFERENCES content_revisions(id) ON DELETE RESTRICT` |
| `users` | `users_auth_version_check` | `CHECK (auth_version > 0)` |
| `users` | `users_auth_version_not_null` | `NOT NULL auth_version` |
| `users` | `users_avatar_url_not_null` | `NOT NULL avatar_url` |
| `users` | `users_country_not_null` | `NOT NULL country` |
| `users` | `users_created_at_not_null` | `NOT NULL created_at` |
| `users` | `users_email_key` | `UNIQUE (email)` |
| `users` | `users_email_not_null` | `NOT NULL email` |
| `users` | `users_email_verified_not_null` | `NOT NULL email_verified` |
| `users` | `users_id_not_null` | `NOT NULL id` |
| `users` | `users_password_hash_not_null` | `NOT NULL password_hash` |
| `users` | `users_permission_version_check` | `CHECK (permission_version > 0)` |
| `users` | `users_permission_version_not_null` | `NOT NULL permission_version` |
| `users` | `users_pkey` | `PRIMARY KEY (id)` |
| `users` | `users_preferred_content_language_not_null` | `NOT NULL preferred_content_language` |
| `users` | `users_preferred_ui_language_not_null` | `NOT NULL preferred_ui_language` |
| `users` | `users_profile_background_file_id_fkey` | `FOREIGN KEY (profile_background_file_id) REFERENCES oss_files(id) ON DELETE SET NULL` |
| `users` | `users_profile_background_url_not_null` | `NOT NULL profile_background_url` |
| `users` | `users_public_card_stat_slots_check` | `CHECK (cardinality(public_card_stat_slots) = 6)` |
| `users` | `users_public_card_stat_slots_not_null` | `NOT NULL public_card_stat_slots` |
| `users` | `users_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `users` | `users_public_id_key` | `UNIQUE (public_id)` |
| `users` | `users_public_id_not_null` | `NOT NULL public_id` |
| `users` | `users_registration_city_not_null` | `NOT NULL registration_city` |
| `users` | `users_registration_country_code_not_null` | `NOT NULL registration_country_code` |
| `users` | `users_registration_ip_not_null` | `NOT NULL registration_ip` |
| `users` | `users_secondary_content_language_not_null` | `NOT NULL secondary_content_language` |
| `users` | `users_security_score_not_null` | `NOT NULL security_score` |
| `users` | `users_show_online_status_not_null` | `NOT NULL show_online_status` |
| `users` | `users_signature_not_null` | `NOT NULL signature` |
| `users` | `users_status_not_null` | `NOT NULL status` |
| `users` | `users_timezone_not_null` | `NOT NULL timezone` |
| `users` | `users_updated_at_not_null` | `NOT NULL updated_at` |
| `users` | `users_username_key` | `UNIQUE (username)` |
| `users` | `users_username_not_null` | `NOT NULL username` |
| `yggdrasil_accounts` | `yggdrasil_accounts_account_uuid_key` | `UNIQUE (account_uuid)` |
| `yggdrasil_accounts` | `yggdrasil_accounts_account_uuid_not_null` | `NOT NULL account_uuid` |
| `yggdrasil_accounts` | `yggdrasil_accounts_created_at_not_null` | `NOT NULL created_at` |
| `yggdrasil_accounts` | `yggdrasil_accounts_enabled_not_null` | `NOT NULL enabled` |
| `yggdrasil_accounts` | `yggdrasil_accounts_launcher_password_hash_not_null` | `NOT NULL launcher_password_hash` |
| `yggdrasil_accounts` | `yggdrasil_accounts_pkey` | `PRIMARY KEY (user_id)` |
| `yggdrasil_accounts` | `yggdrasil_accounts_updated_at_not_null` | `NOT NULL updated_at` |
| `yggdrasil_accounts` | `yggdrasil_accounts_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `yggdrasil_accounts` | `yggdrasil_accounts_user_id_not_null` | `NOT NULL user_id` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_client_ip_not_null` | `NOT NULL client_ip` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_created_at_not_null` | `NOT NULL created_at` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_expires_at_not_null` | `NOT NULL expires_at` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_pkey` | `PRIMARY KEY (server_id)` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_player_profile_id_fkey` | `FOREIGN KEY (player_profile_id) REFERENCES player_profiles(id) ON DELETE CASCADE` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_player_profile_id_not_null` | `NOT NULL player_profile_id` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_server_id_check` | `CHECK (char_length(server_id) >= 1 AND char_length(server_id) <= 255)` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_server_id_not_null` | `NOT NULL server_id` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_token_id_fkey` | `FOREIGN KEY (token_id) REFERENCES yggdrasil_tokens(id) ON DELETE CASCADE` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_token_id_not_null` | `NOT NULL token_id` |
| `yggdrasil_tokens` | `yggdrasil_tokens_access_token_hash_check` | `CHECK (access_token_hash ~ '^[0-9a-f]{64}$'::text)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_access_token_hash_key` | `UNIQUE (access_token_hash)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_access_token_hash_not_null` | `NOT NULL access_token_hash` |
| `yggdrasil_tokens` | `yggdrasil_tokens_client_token_check` | `CHECK (char_length(client_token) >= 1 AND char_length(client_token) <= 512)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_client_token_not_null` | `NOT NULL client_token` |
| `yggdrasil_tokens` | `yggdrasil_tokens_expires_at_not_null` | `NOT NULL expires_at` |
| `yggdrasil_tokens` | `yggdrasil_tokens_id_not_null` | `NOT NULL id` |
| `yggdrasil_tokens` | `yggdrasil_tokens_ip_not_null` | `NOT NULL ip` |
| `yggdrasil_tokens` | `yggdrasil_tokens_issued_at_not_null` | `NOT NULL issued_at` |
| `yggdrasil_tokens` | `yggdrasil_tokens_pkey` | `PRIMARY KEY (id)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_player_profile_id_fkey` | `FOREIGN KEY (player_profile_id) REFERENCES player_profiles(id) ON DELETE SET NULL` |
| `yggdrasil_tokens` | `yggdrasil_tokens_public_id_check` | `CHECK (public_id ~ '^[a-z0-9]{9}$'::text)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_public_id_key` | `UNIQUE (public_id)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_public_id_not_null` | `NOT NULL public_id` |
| `yggdrasil_tokens` | `yggdrasil_tokens_replaced_by_id_fkey` | `FOREIGN KEY (replaced_by_id) REFERENCES yggdrasil_tokens(id) ON DELETE SET NULL` |
| `yggdrasil_tokens` | `yggdrasil_tokens_status_check` | `CHECK (status = ANY (ARRAY['active'::text, 'stale'::text, 'revoked'::text]))` |
| `yggdrasil_tokens` | `yggdrasil_tokens_status_not_null` | `NOT NULL status` |
| `yggdrasil_tokens` | `yggdrasil_tokens_user_agent_not_null` | `NOT NULL user_agent` |
| `yggdrasil_tokens` | `yggdrasil_tokens_user_id_fkey` | `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE` |
| `yggdrasil_tokens` | `yggdrasil_tokens_user_id_not_null` | `NOT NULL user_id` |

## 索引

| 表 | 名称 | 定义 |
| --- | --- | --- |
| `activity_actions` | `activity_actions_code_key` | `CREATE UNIQUE INDEX activity_actions_code_key ON public.activity_actions USING btree (code)` |
| `activity_actions` | `activity_actions_pkey` | `CREATE UNIQUE INDEX activity_actions_pkey ON public.activity_actions USING btree (id)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_pkey` | `CREATE UNIQUE INDEX activity_cleanup_runs_pkey ON public.activity_cleanup_runs USING btree (id)` |
| `activity_cleanup_runs` | `activity_cleanup_runs_public_id_key` | `CREATE UNIQUE INDEX activity_cleanup_runs_public_id_key ON public.activity_cleanup_runs USING btree (public_id)` |
| `activity_cleanup_runs` | `idx_activity_cleanup_runs_schedule` | `CREATE INDEX idx_activity_cleanup_runs_schedule ON public.activity_cleanup_runs USING btree (source, started_at DESC, id DESC)` |
| `activity_cleanup_runs` | `idx_fk_activity_cleanup_runs_activity_cleanup_runs_ini_35202ffb` | `CREATE INDEX idx_fk_activity_cleanup_runs_activity_cleanup_runs_ini_35202ffb ON public.activity_cleanup_runs USING btree (initiated_by)` |
| `activity_event_outbox` | `activity_event_outbox_pkey` | `CREATE UNIQUE INDEX activity_event_outbox_pkey ON public.activity_event_outbox USING btree (id)` |
| `activity_event_outbox` | `idx_activity_outbox_available` | `CREATE INDEX idx_activity_outbox_available ON public.activity_event_outbox USING btree (available_at, id)` |
| `activity_event_outbox` | `idx_activity_outbox_created_brin` | `CREATE INDEX idx_activity_outbox_created_brin ON public.activity_event_outbox USING brin (created_at)` |
| `activity_event_outbox` | `idx_fk_activity_event_outbox_activity_event_outbox_act_24de5206` | `CREATE INDEX idx_fk_activity_event_outbox_activity_event_outbox_act_24de5206 ON public.activity_event_outbox USING btree (action_id)` |
| `activity_event_outbox` | `idx_fk_activity_event_outbox_activity_event_outbox_obj_8480dded` | `CREATE INDEX idx_fk_activity_event_outbox_activity_event_outbox_obj_8480dded ON public.activity_event_outbox USING btree (object_type_id)` |
| `activity_event_outbox` | `idx_fk_activity_event_outbox_activity_event_outbox_obj_b8ddbd72` | `CREATE INDEX idx_fk_activity_event_outbox_activity_event_outbox_obj_b8ddbd72 ON public.activity_event_outbox USING btree (object_route_id)` |
| `activity_event_outbox` | `idx_fk_activity_event_outbox_activity_event_outbox_use_5d312d83` | `CREATE INDEX idx_fk_activity_event_outbox_activity_event_outbox_use_5d312d83 ON public.activity_event_outbox USING btree (user_id)` |
| `activity_object_types` | `activity_object_types_code_key` | `CREATE UNIQUE INDEX activity_object_types_code_key ON public.activity_object_types USING btree (code)` |
| `activity_object_types` | `activity_object_types_pkey` | `CREATE UNIQUE INDEX activity_object_types_pkey ON public.activity_object_types USING btree (id)` |
| `ai_task_logs` | `ai_task_logs_pkey` | `CREATE UNIQUE INDEX ai_task_logs_pkey ON public.ai_task_logs USING btree (id)` |
| `ai_task_logs` | `idx_ai_task_logs_task_created_at` | `CREATE INDEX idx_ai_task_logs_task_created_at ON public.ai_task_logs USING btree (task_id, created_at DESC)` |
| `ai_tasks` | `ai_tasks_pkey` | `CREATE UNIQUE INDEX ai_tasks_pkey ON public.ai_tasks USING btree (id)` |
| `ai_tasks` | `ai_tasks_task_uid_key` | `CREATE UNIQUE INDEX ai_tasks_task_uid_key ON public.ai_tasks USING btree (task_uid)` |
| `ai_tasks` | `idx_ai_tasks_status_created_at` | `CREATE INDEX idx_ai_tasks_status_created_at ON public.ai_tasks USING btree (status, created_at DESC)` |
| `ai_tasks` | `idx_ai_tasks_type_created_at` | `CREATE INDEX idx_ai_tasks_type_created_at ON public.ai_tasks USING btree (task_type, created_at DESC)` |
| `ai_tasks` | `idx_fk_ai_tasks_ai_tasks_created_by_fkey_c24788f4` | `CREATE INDEX idx_fk_ai_tasks_ai_tasks_created_by_fkey_c24788f4 ON public.ai_tasks USING btree (created_by)` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_pkey` | `CREATE UNIQUE INDEX anti_abuse_bot_rules_pkey ON public.anti_abuse_bot_rules USING btree (id)` |
| `anti_abuse_bot_rules` | `anti_abuse_bot_rules_public_id_key` | `CREATE UNIQUE INDEX anti_abuse_bot_rules_public_id_key ON public.anti_abuse_bot_rules USING btree (public_id)` |
| `anti_abuse_bot_rules` | `idx_anti_abuse_bot_rules_kind_enabled` | `CREATE INDEX idx_anti_abuse_bot_rules_kind_enabled ON public.anti_abuse_bot_rules USING btree (kind, enabled, updated_at DESC)` |
| `anti_abuse_bot_rules` | `idx_fk_anti_abuse_bot_rules_anti_abuse_bot_rules_creat_3e90cb5d` | `CREATE INDEX idx_fk_anti_abuse_bot_rules_anti_abuse_bot_rules_creat_3e90cb5d ON public.anti_abuse_bot_rules USING btree (created_by)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_pkey` | `CREATE UNIQUE INDEX anti_abuse_challenges_pkey ON public.anti_abuse_challenges USING btree (id)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_public_id_key` | `CREATE UNIQUE INDEX anti_abuse_challenges_public_id_key ON public.anti_abuse_challenges USING btree (public_id)` |
| `anti_abuse_challenges` | `anti_abuse_challenges_token_hash_key` | `CREATE UNIQUE INDEX anti_abuse_challenges_token_hash_key ON public.anti_abuse_challenges USING btree (token_hash)` |
| `anti_abuse_challenges` | `idx_anti_abuse_challenges_expiry` | `CREATE INDEX idx_anti_abuse_challenges_expiry ON public.anti_abuse_challenges USING btree (expires_at, status)` |
| `anti_abuse_challenges` | `idx_anti_abuse_challenges_user_pending` | `CREATE INDEX idx_anti_abuse_challenges_user_pending ON public.anti_abuse_challenges USING btree (user_id, action, expires_at) WHERE (status = ANY (ARRAY['pending'::text, 'passed'::text]))` |
| `anti_abuse_challenges` | `idx_fk_anti_abuse_challenges_anti_abuse_challenges_use_14e7dea4` | `CREATE INDEX idx_fk_anti_abuse_challenges_anti_abuse_challenges_use_14e7dea4 ON public.anti_abuse_challenges USING btree (user_id)` |
| `anti_abuse_content_fingerprints` | `anti_abuse_content_fingerprints_pkey` | `CREATE UNIQUE INDEX anti_abuse_content_fingerprints_pkey ON public.anti_abuse_content_fingerprints USING btree (id)` |
| `anti_abuse_content_fingerprints` | `idx_anti_abuse_fingerprint_exact` | `CREATE INDEX idx_anti_abuse_fingerprint_exact ON public.anti_abuse_content_fingerprints USING btree (exact_hash, created_at DESC, id DESC)` |
| `anti_abuse_content_fingerprints` | `idx_anti_abuse_fingerprint_object` | `CREATE INDEX idx_anti_abuse_fingerprint_object ON public.anti_abuse_content_fingerprints USING btree (action, object_key, created_at DESC, id DESC) WHERE (object_key <> ''::text)` |
| `anti_abuse_content_fingerprints` | `idx_anti_abuse_fingerprint_user_action` | `CREATE INDEX idx_anti_abuse_fingerprint_user_action ON public.anti_abuse_content_fingerprints USING btree (user_id, action, created_at DESC, id DESC)` |
| `anti_abuse_content_fingerprints` | `idx_fk_anti_abuse_content_fingerprints_anti_abuse_cont_0572480d` | `CREATE INDEX idx_fk_anti_abuse_content_fingerprints_anti_abuse_cont_0572480d ON public.anti_abuse_content_fingerprints USING btree (event_id)` |
| `anti_abuse_daily_stats` | `anti_abuse_daily_stats_pkey` | `CREATE UNIQUE INDEX anti_abuse_daily_stats_pkey ON public.anti_abuse_daily_stats USING btree (stat_date, action, outcome, crawler_class)` |
| `anti_abuse_daily_stats` | `idx_anti_abuse_daily_stats_date` | `CREATE INDEX idx_anti_abuse_daily_stats_date ON public.anti_abuse_daily_stats USING btree (stat_date DESC, action)` |
| `anti_abuse_events` | `anti_abuse_events_pkey` | `CREATE UNIQUE INDEX anti_abuse_events_pkey ON public.anti_abuse_events USING btree (id)` |
| `anti_abuse_events` | `anti_abuse_events_public_id_key` | `CREATE UNIQUE INDEX anti_abuse_events_public_id_key ON public.anti_abuse_events USING btree (public_id)` |
| `anti_abuse_events` | `idx_anti_abuse_events_action_time` | `CREATE INDEX idx_anti_abuse_events_action_time ON public.anti_abuse_events USING btree (action, created_at DESC, id DESC)` |
| `anti_abuse_events` | `idx_anti_abuse_events_ip_time` | `CREATE INDEX idx_anti_abuse_events_ip_time ON public.anti_abuse_events USING btree (ip_hash, created_at DESC, id DESC) WHERE (ip_hash <> ''::text)` |
| `anti_abuse_events` | `idx_anti_abuse_events_outcome_time` | `CREATE INDEX idx_anti_abuse_events_outcome_time ON public.anti_abuse_events USING btree (outcome, created_at DESC, id DESC)` |
| `anti_abuse_events` | `idx_anti_abuse_events_time` | `CREATE INDEX idx_anti_abuse_events_time ON public.anti_abuse_events USING btree (created_at DESC, id DESC)` |
| `anti_abuse_events` | `idx_anti_abuse_events_user_time` | `CREATE INDEX idx_anti_abuse_events_user_time ON public.anti_abuse_events USING btree (user_id, created_at DESC, id DESC) WHERE (user_id IS NOT NULL)` |
| `anti_abuse_events` | `idx_fk_anti_abuse_events_anti_abuse_events_reviewed_by_ee4fa63d` | `CREATE INDEX idx_fk_anti_abuse_events_anti_abuse_events_reviewed_by_ee4fa63d ON public.anti_abuse_events USING btree (reviewed_by)` |
| `anti_abuse_events` | `idx_fk_anti_abuse_events_anti_abuse_events_similar_eve_25f8d63b` | `CREATE INDEX idx_fk_anti_abuse_events_anti_abuse_events_similar_eve_25f8d63b ON public.anti_abuse_events USING btree (similar_event_id)` |
| `anti_abuse_events` | `idx_fk_anti_abuse_events_anti_abuse_events_user_id_fke_e8057382` | `CREATE INDEX idx_fk_anti_abuse_events_anti_abuse_events_user_id_fke_e8057382 ON public.anti_abuse_events USING btree (user_id)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_pkey` | `CREATE UNIQUE INDEX anti_abuse_restrictions_pkey ON public.anti_abuse_restrictions USING btree (id)` |
| `anti_abuse_restrictions` | `anti_abuse_restrictions_public_id_key` | `CREATE UNIQUE INDEX anti_abuse_restrictions_public_id_key ON public.anti_abuse_restrictions USING btree (public_id)` |
| `anti_abuse_restrictions` | `idx_anti_abuse_restrictions_ip_active` | `CREATE INDEX idx_anti_abuse_restrictions_ip_active ON public.anti_abuse_restrictions USING btree (ip_hash, starts_at DESC) WHERE ((lifted_at IS NULL) AND (ip_hash <> ''::text))` |
| `anti_abuse_restrictions` | `idx_anti_abuse_restrictions_user_active` | `CREATE INDEX idx_anti_abuse_restrictions_user_active ON public.anti_abuse_restrictions USING btree (user_id, starts_at DESC) WHERE (lifted_at IS NULL)` |
| `anti_abuse_restrictions` | `idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_241faea9` | `CREATE INDEX idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_241faea9 ON public.anti_abuse_restrictions USING btree (lifted_by)` |
| `anti_abuse_restrictions` | `idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_60e60bbc` | `CREATE INDEX idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_60e60bbc ON public.anti_abuse_restrictions USING btree (created_by)` |
| `anti_abuse_restrictions` | `idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_e4dc1b32` | `CREATE INDEX idx_fk_anti_abuse_restrictions_anti_abuse_restrictions_e4dc1b32 ON public.anti_abuse_restrictions USING btree (user_id)` |
| `anti_abuse_user_states` | `anti_abuse_user_states_pkey` | `CREATE UNIQUE INDEX anti_abuse_user_states_pkey ON public.anti_abuse_user_states USING btree (user_id)` |
| `app_logs` | `app_logs_pkey` | `CREATE UNIQUE INDEX app_logs_pkey ON public.app_logs USING btree (id)` |
| `app_logs` | `idx_app_logs_category_created_at` | `CREATE INDEX idx_app_logs_category_created_at ON public.app_logs USING btree (category, created_at DESC)` |
| `app_logs` | `idx_fk_app_logs_app_logs_actor_id_fkey_8a9b8872` | `CREATE INDEX idx_fk_app_logs_app_logs_actor_id_fkey_8a9b8872 ON public.app_logs USING btree (actor_id)` |
| `audit_events` | `audit_events_pkey` | `CREATE UNIQUE INDEX audit_events_pkey ON public.audit_events USING btree (id)` |
| `audit_events` | `audit_events_public_id_key` | `CREATE UNIQUE INDEX audit_events_public_id_key ON public.audit_events USING btree (public_id)` |
| `audit_events` | `idx_audit_events_aggregate` | `CREATE INDEX idx_audit_events_aggregate ON public.audit_events USING btree (aggregate_type, aggregate_key, created_at DESC, id DESC)` |
| `audit_events` | `idx_audit_events_entity` | `CREATE INDEX idx_audit_events_entity ON public.audit_events USING btree (entity_type, entity_id, created_at DESC, id DESC) WHERE (entity_id IS NOT NULL)` |
| `audit_events` | `idx_fk_audit_events_audit_events_actor_id_fkey_7c7a3720` | `CREATE INDEX idx_fk_audit_events_audit_events_actor_id_fkey_7c7a3720 ON public.audit_events USING btree (actor_id)` |
| `audit_events` | `idx_fk_audit_events_audit_events_entity_type_entity_id_95fd254e` | `CREATE INDEX idx_fk_audit_events_audit_events_entity_type_entity_id_95fd254e ON public.audit_events USING btree (entity_type, entity_id)` |
| `auth_sessions` | `auth_sessions_pkey` | `CREATE UNIQUE INDEX auth_sessions_pkey ON public.auth_sessions USING btree (id)` |
| `auth_sessions` | `auth_sessions_session_hash_key` | `CREATE UNIQUE INDEX auth_sessions_session_hash_key ON public.auth_sessions USING btree (session_hash)` |
| `auth_sessions` | `idx_auth_sessions_expiry` | `CREATE INDEX idx_auth_sessions_expiry ON public.auth_sessions USING btree (expires_at)` |
| `auth_sessions` | `idx_auth_sessions_user_active` | `CREATE INDEX idx_auth_sessions_user_active ON public.auth_sessions USING btree (user_id, expires_at DESC) WHERE (revoked_at IS NULL)` |
| `auth_sessions` | `idx_fk_auth_sessions_auth_sessions_user_id_fkey_3e7d42cb` | `CREATE INDEX idx_fk_auth_sessions_auth_sessions_user_id_fkey_3e7d42cb ON public.auth_sessions USING btree (user_id)` |
| `ban_reasons` | `ban_reasons_pkey` | `CREATE UNIQUE INDEX ban_reasons_pkey ON public.ban_reasons USING btree (code)` |
| `ban_records` | `ban_records_pkey` | `CREATE UNIQUE INDEX ban_records_pkey ON public.ban_records USING btree (id)` |
| `ban_records` | `ban_records_public_id_key` | `CREATE UNIQUE INDEX ban_records_public_id_key ON public.ban_records USING btree (public_id)` |
| `ban_records` | `idx_ban_records_public` | `CREATE INDEX idx_ban_records_public ON public.ban_records USING btree (created_at DESC, id DESC)` |
| `ban_records` | `idx_fk_ban_records_ban_records_moderator_id_fkey_afa8dacd` | `CREATE INDEX idx_fk_ban_records_ban_records_moderator_id_fkey_afa8dacd ON public.ban_records USING btree (moderator_id)` |
| `ban_records` | `idx_fk_ban_records_ban_records_reason_code_fkey_2a120c5d` | `CREATE INDEX idx_fk_ban_records_ban_records_reason_code_fkey_2a120c5d ON public.ban_records USING btree (reason_code)` |
| `ban_records` | `idx_fk_ban_records_ban_records_report_id_fkey_c0acb0e1` | `CREATE INDEX idx_fk_ban_records_ban_records_report_id_fkey_c0acb0e1 ON public.ban_records USING btree (report_id)` |
| `ban_records` | `idx_fk_ban_records_ban_records_revoked_by_fkey_8cbba9b6` | `CREATE INDEX idx_fk_ban_records_ban_records_revoked_by_fkey_8cbba9b6 ON public.ban_records USING btree (revoked_by)` |
| `ban_records` | `idx_fk_ban_records_ban_records_user_id_fkey_9a27bda7` | `CREATE INDEX idx_fk_ban_records_ban_records_user_id_fkey_9a27bda7 ON public.ban_records USING btree (user_id)` |
| `ban_records` | `uq_ban_records_active_user` | `CREATE UNIQUE INDEX uq_ban_records_active_user ON public.ban_records USING btree (user_id) WHERE (status = 'active'::text)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_pkey` | `CREATE UNIQUE INDEX block_entity_model_snapshots_pkey ON public.block_entity_model_snapshots USING btree (id)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_resource_snapshot_id_key` | `CREATE UNIQUE INDEX block_entity_model_snapshots_resource_snapshot_id_key ON public.block_entity_model_snapshots USING btree (resource_snapshot_id)` |
| `block_entity_model_snapshots` | `block_entity_model_snapshots_revision_id_block_id_key` | `CREATE UNIQUE INDEX block_entity_model_snapshots_revision_id_block_id_key ON public.block_entity_model_snapshots USING btree (revision_id, block_id)` |
| `block_entity_model_snapshots` | `idx_block_entity_model_snapshots_revision` | `CREATE INDEX idx_block_entity_model_snapshots_revision ON public.block_entity_model_snapshots USING btree (revision_id, block_resource_id)` |
| `block_entity_model_snapshots` | `idx_fk_block_entity_model_snapshots_block_entity_model_ec16bcde` | `CREATE INDEX idx_fk_block_entity_model_snapshots_block_entity_model_ec16bcde ON public.block_entity_model_snapshots USING btree (block_resource_id)` |
| `block_entity_model_variants` | `block_entity_model_variants_model_snapshot_id_variant_id_key` | `CREATE UNIQUE INDEX block_entity_model_variants_model_snapshot_id_variant_id_key ON public.block_entity_model_variants USING btree (model_snapshot_id, variant_id)` |
| `block_entity_model_variants` | `block_entity_model_variants_pkey` | `CREATE UNIQUE INDEX block_entity_model_variants_pkey ON public.block_entity_model_variants USING btree (id)` |
| `block_entity_model_variants` | `idx_block_entity_model_variants_snapshot` | `CREATE INDEX idx_block_entity_model_variants_snapshot ON public.block_entity_model_variants USING btree (model_snapshot_id, variant_id)` |
| `blueprint_jobs` | `blueprint_jobs_pkey` | `CREATE UNIQUE INDEX blueprint_jobs_pkey ON public.blueprint_jobs USING btree (id)` |
| `blueprint_jobs` | `blueprint_jobs_public_id_key` | `CREATE UNIQUE INDEX blueprint_jobs_public_id_key ON public.blueprint_jobs USING btree (public_id)` |
| `blueprint_jobs` | `idx_blueprint_jobs_queue` | `CREATE INDEX idx_blueprint_jobs_queue ON public.blueprint_jobs USING btree (status, created_at)` |
| `blueprint_jobs` | `idx_fk_blueprint_jobs_blueprint_jobs_blueprint_id_fkey_19ea9954` | `CREATE INDEX idx_fk_blueprint_jobs_blueprint_jobs_blueprint_id_fkey_19ea9954 ON public.blueprint_jobs USING btree (blueprint_id)` |
| `blueprint_jobs` | `idx_fk_blueprint_jobs_blueprint_jobs_created_by_fkey_2f2026e5` | `CREATE INDEX idx_fk_blueprint_jobs_blueprint_jobs_created_by_fkey_2f2026e5 ON public.blueprint_jobs USING btree (created_by)` |
| `blueprint_materials` | `blueprint_materials_pkey` | `CREATE UNIQUE INDEX blueprint_materials_pkey ON public.blueprint_materials USING btree (blueprint_id, block_state)` |
| `blueprint_materials` | `idx_blueprint_materials_count` | `CREATE INDEX idx_blueprint_materials_count ON public.blueprint_materials USING btree (blueprint_id, block_count DESC)` |
| `blueprint_mods` | `blueprint_mods_pkey` | `CREATE UNIQUE INDEX blueprint_mods_pkey ON public.blueprint_mods USING btree (blueprint_id, source_namespace)` |
| `blueprint_mods` | `idx_blueprint_mods_mod_blueprint` | `CREATE INDEX idx_blueprint_mods_mod_blueprint ON public.blueprint_mods USING btree (mod_id, blueprint_id)` |
| `blueprint_variants` | `blueprint_variants_blueprint_id_object_key_key` | `CREATE UNIQUE INDEX blueprint_variants_blueprint_id_object_key_key ON public.blueprint_variants USING btree (blueprint_id, object_key)` |
| `blueprint_variants` | `blueprint_variants_pkey` | `CREATE UNIQUE INDEX blueprint_variants_pkey ON public.blueprint_variants USING btree (id)` |
| `blueprint_variants` | `blueprint_variants_public_id_key` | `CREATE UNIQUE INDEX blueprint_variants_public_id_key ON public.blueprint_variants USING btree (public_id)` |
| `blueprint_variants` | `idx_blueprint_variants_blueprint` | `CREATE INDEX idx_blueprint_variants_blueprint ON public.blueprint_variants USING btree (blueprint_id, original DESC, created_at)` |
| `blueprint_variants` | `idx_fk_blueprint_variants_blueprint_variants_created_b_b2184075` | `CREATE INDEX idx_fk_blueprint_variants_blueprint_variants_created_b_b2184075 ON public.blueprint_variants USING btree (created_by)` |
| `blueprint_variants` | `idx_fk_blueprint_variants_blueprint_variants_file_id_f_81d4217b` | `CREATE INDEX idx_fk_blueprint_variants_blueprint_variants_file_id_f_81d4217b ON public.blueprint_variants USING btree (file_id)` |
| `blueprints` | `blueprints_pkey` | `CREATE UNIQUE INDEX blueprints_pkey ON public.blueprints USING btree (id)` |
| `blueprints` | `blueprints_public_id_key` | `CREATE UNIQUE INDEX blueprints_public_id_key ON public.blueprints USING btree (public_id)` |
| `blueprints` | `idx_blueprints_owner_created` | `CREATE INDEX idx_blueprints_owner_created ON public.blueprints USING btree (owner_id, created_at DESC)` |
| `blueprints` | `idx_blueprints_owner_original_file` | `CREATE UNIQUE INDEX idx_blueprints_owner_original_file ON public.blueprints USING btree (owner_id, original_file_id) WHERE ((original_file_id IS NOT NULL) AND (status <> 'deleted'::text))` |
| `blueprints` | `idx_blueprints_status_updated` | `CREATE INDEX idx_blueprints_status_updated ON public.blueprints USING btree (status, updated_at DESC)` |
| `blueprints` | `idx_fk_blueprints_blueprints_cover_file_id_fkey_06a49266` | `CREATE INDEX idx_fk_blueprints_blueprints_cover_file_id_fkey_06a49266 ON public.blueprints USING btree (cover_file_id)` |
| `blueprints` | `idx_fk_blueprints_blueprints_original_file_id_fkey_9d6826d4` | `CREATE INDEX idx_fk_blueprints_blueprints_original_file_id_fkey_9d6826d4 ON public.blueprints USING btree (original_file_id)` |
| `blueprints` | `idx_fk_blueprints_fk_blueprints_published_revision_a2d5739e` | `CREATE INDEX idx_fk_blueprints_fk_blueprints_published_revision_a2d5739e ON public.blueprints USING btree (published_revision_id)` |
| `catalog_entities` | `catalog_entities_id_entity_type_key` | `CREATE UNIQUE INDEX catalog_entities_id_entity_type_key ON public.catalog_entities USING btree (id, entity_type)` |
| `catalog_entities` | `catalog_entities_identity_key_key` | `CREATE UNIQUE INDEX catalog_entities_identity_key_key ON public.catalog_entities USING btree (identity_key)` |
| `catalog_entities` | `catalog_entities_pkey` | `CREATE UNIQUE INDEX catalog_entities_pkey ON public.catalog_entities USING btree (id)` |
| `catalog_entities` | `catalog_entities_public_id_key` | `CREATE UNIQUE INDEX catalog_entities_public_id_key ON public.catalog_entities USING btree (public_id)` |
| `catalog_entities` | `idx_catalog_entities_identity_key` | `CREATE INDEX idx_catalog_entities_identity_key ON public.catalog_entities USING btree (identity_key)` |
| `catalog_entities` | `idx_catalog_entities_type_status` | `CREATE INDEX idx_catalog_entities_type_status ON public.catalog_entities USING btree (entity_type, status, id)` |
| `catalog_entities` | `idx_fk_catalog_entities_fk_catalog_entities_published__89acc832` | `CREATE INDEX idx_fk_catalog_entities_fk_catalog_entities_published__89acc832 ON public.catalog_entities USING btree (published_revision_id)` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_pkey` | `CREATE UNIQUE INDEX catalog_import_binary_assets_pkey ON public.catalog_import_binary_assets USING btree (id)` |
| `catalog_import_binary_assets` | `catalog_import_binary_assets_revision_id_asset_path_key` | `CREATE UNIQUE INDEX catalog_import_binary_assets_revision_id_asset_path_key ON public.catalog_import_binary_assets USING btree (revision_id, asset_path)` |
| `catalog_import_capabilities` | `catalog_import_capabilities_pkey` | `CREATE UNIQUE INDEX catalog_import_capabilities_pkey ON public.catalog_import_capabilities USING btree (revision_id, capability_id)` |
| `catalog_import_job_logs` | `catalog_import_job_logs_pkey` | `CREATE UNIQUE INDEX catalog_import_job_logs_pkey ON public.catalog_import_job_logs USING btree (id)` |
| `catalog_import_job_logs` | `idx_fk_catalog_import_job_logs_catalog_import_job_logs_3dd62162` | `CREATE INDEX idx_fk_catalog_import_job_logs_catalog_import_job_logs_3dd62162 ON public.catalog_import_job_logs USING btree (job_id)` |
| `catalog_import_jobs` | `catalog_import_jobs_mod_id_package_id_importer_version_targ_key` | `CREATE UNIQUE INDEX catalog_import_jobs_mod_id_package_id_importer_version_targ_key ON public.catalog_import_jobs USING btree (mod_id, package_id, importer_version, target_version_id, overwrite_existing)` |
| `catalog_import_jobs` | `catalog_import_jobs_pkey` | `CREATE UNIQUE INDEX catalog_import_jobs_pkey ON public.catalog_import_jobs USING btree (id)` |
| `catalog_import_jobs` | `idx_catalog_import_jobs_confirmation` | `CREATE INDEX idx_catalog_import_jobs_confirmation ON public.catalog_import_jobs USING btree (created_by, updated_at DESC) WHERE (status = 'confirmation_required'::text)` |
| `catalog_import_jobs` | `idx_catalog_import_jobs_running_heartbeat` | `CREATE INDEX idx_catalog_import_jobs_running_heartbeat ON public.catalog_import_jobs USING btree (heartbeat_at) WHERE (status = ANY (ARRAY['validating'::text, 'importing'::text]))` |
| `catalog_import_jobs` | `idx_catalog_import_jobs_status_created` | `CREATE INDEX idx_catalog_import_jobs_status_created ON public.catalog_import_jobs USING btree (status, created_at)` |
| `catalog_import_jobs` | `idx_fk_catalog_import_jobs_catalog_import_jobs_created_ab902537` | `CREATE INDEX idx_fk_catalog_import_jobs_catalog_import_jobs_created_ab902537 ON public.catalog_import_jobs USING btree (created_by)` |
| `catalog_import_jobs` | `idx_fk_catalog_import_jobs_catalog_import_jobs_modid_c_98f46aa4` | `CREATE INDEX idx_fk_catalog_import_jobs_catalog_import_jobs_modid_c_98f46aa4 ON public.catalog_import_jobs USING btree (modid_confirmed_by)` |
| `catalog_import_jobs` | `idx_fk_catalog_import_jobs_catalog_import_jobs_package_cd2f6c9f` | `CREATE INDEX idx_fk_catalog_import_jobs_catalog_import_jobs_package_cd2f6c9f ON public.catalog_import_jobs USING btree (package_id)` |
| `catalog_import_jobs` | `idx_fk_catalog_import_jobs_fk_catalog_import_jobs_targ_07dde8fa` | `CREATE INDEX idx_fk_catalog_import_jobs_fk_catalog_import_jobs_targ_07dde8fa ON public.catalog_import_jobs USING btree (target_version_id)` |
| `catalog_import_locales` | `catalog_import_locales_pkey` | `CREATE UNIQUE INDEX catalog_import_locales_pkey ON public.catalog_import_locales USING btree (revision_id, locale)` |
| `catalog_import_media` | `catalog_import_media_pkey` | `CREATE UNIQUE INDEX catalog_import_media_pkey ON public.catalog_import_media USING btree (revision_id, asset_path)` |
| `catalog_import_media` | `idx_fk_catalog_import_media_catalog_import_media_oss_f_73d2c06d` | `CREATE INDEX idx_fk_catalog_import_media_catalog_import_media_oss_f_73d2c06d ON public.catalog_import_media USING btree (oss_file_id)` |
| `catalog_import_packages` | `catalog_import_packages_pkey` | `CREATE UNIQUE INDEX catalog_import_packages_pkey ON public.catalog_import_packages USING btree (id)` |
| `catalog_import_packages` | `catalog_import_packages_sha256_key` | `CREATE UNIQUE INDEX catalog_import_packages_sha256_key ON public.catalog_import_packages USING btree (sha256)` |
| `catalog_import_packages` | `idx_fk_catalog_import_packages_catalog_import_packages_41766a1b` | `CREATE INDEX idx_fk_catalog_import_packages_catalog_import_packages_41766a1b ON public.catalog_import_packages USING btree (uploaded_by)` |
| `catalog_import_packages` | `idx_fk_catalog_import_packages_catalog_import_packages_6e8e4c97` | `CREATE INDEX idx_fk_catalog_import_packages_catalog_import_packages_6e8e4c97 ON public.catalog_import_packages USING btree (archive_file_id)` |
| `catalog_import_revision_stats` | `catalog_import_revision_stats_pkey` | `CREATE UNIQUE INDEX catalog_import_revision_stats_pkey ON public.catalog_import_revision_stats USING btree (revision_id)` |
| `catalog_import_revisions` | `catalog_import_revisions_mod_id_target_version_id_source_ki_key` | `CREATE UNIQUE INDEX catalog_import_revisions_mod_id_target_version_id_source_ki_key ON public.catalog_import_revisions USING btree (mod_id, target_version_id, source_kind, source_namespace, revision_no)` |
| `catalog_import_revisions` | `catalog_import_revisions_pkey` | `CREATE UNIQUE INDEX catalog_import_revisions_pkey ON public.catalog_import_revisions USING btree (id)` |
| `catalog_import_revisions` | `idx_catalog_import_revisions_active` | `CREATE UNIQUE INDEX idx_catalog_import_revisions_active ON public.catalog_import_revisions USING btree (mod_id, target_version_id, source_kind, source_namespace) WHERE is_active` |
| `catalog_import_revisions` | `idx_catalog_import_revisions_import_run` | `CREATE INDEX idx_catalog_import_revisions_import_run ON public.catalog_import_revisions USING btree (import_run_token) WHERE (status = 'staging'::text)` |
| `catalog_import_revisions` | `idx_catalog_import_revisions_package` | `CREATE INDEX idx_catalog_import_revisions_package ON public.catalog_import_revisions USING btree (package_id)` |
| `catalog_import_revisions` | `idx_fk_catalog_import_revisions_catalog_import_revisio_04e72fe3` | `CREATE INDEX idx_fk_catalog_import_revisions_catalog_import_revisio_04e72fe3 ON public.catalog_import_revisions USING btree (job_id)` |
| `catalog_import_revisions` | `idx_fk_catalog_import_revisions_catalog_import_revisio_f1c2995d` | `CREATE INDEX idx_fk_catalog_import_revisions_catalog_import_revisio_f1c2995d ON public.catalog_import_revisions USING btree (submitted_by)` |
| `catalog_import_revisions` | `idx_fk_catalog_import_revisions_fk_catalog_import_revi_4062ce62` | `CREATE INDEX idx_fk_catalog_import_revisions_fk_catalog_import_revi_4062ce62 ON public.catalog_import_revisions USING btree (target_version_id)` |
| `catalog_import_structures` | `catalog_import_structures_pkey` | `CREATE UNIQUE INDEX catalog_import_structures_pkey ON public.catalog_import_structures USING btree (id)` |
| `catalog_import_structures` | `catalog_import_structures_revision_id_structure_id_key` | `CREATE UNIQUE INDEX catalog_import_structures_revision_id_structure_id_key ON public.catalog_import_structures USING btree (revision_id, structure_id)` |
| `catalog_import_structures` | `idx_fk_catalog_import_structures_catalog_import_struct_d96d27b8` | `CREATE INDEX idx_fk_catalog_import_structures_catalog_import_struct_d96d27b8 ON public.catalog_import_structures USING btree (template_blob_id)` |
| `catalog_import_text_assets` | `catalog_import_text_assets_pkey` | `CREATE UNIQUE INDEX catalog_import_text_assets_pkey ON public.catalog_import_text_assets USING btree (revision_id, asset_path)` |
| `catalog_resource_definitions` | `catalog_resource_definitions_pkey` | `CREATE UNIQUE INDEX catalog_resource_definitions_pkey ON public.catalog_resource_definitions USING btree (resource_id)` |
| `catalog_resource_definitions` | `idx_fk_catalog_resource_definitions_catalog_resource_d_0e1094a8` | `CREATE INDEX idx_fk_catalog_resource_definitions_catalog_resource_d_0e1094a8 ON public.catalog_resource_definitions USING btree (updated_by)` |
| `catalog_resource_definitions` | `idx_fk_catalog_resource_definitions_catalog_resource_d_4fb07612` | `CREATE INDEX idx_fk_catalog_resource_definitions_catalog_resource_d_4fb07612 ON public.catalog_resource_definitions USING btree (icon_file_id)` |
| `catalog_resource_definitions` | `idx_fk_catalog_resource_definitions_catalog_resource_d_f0594b02` | `CREATE INDEX idx_fk_catalog_resource_definitions_catalog_resource_d_f0594b02 ON public.catalog_resource_definitions USING btree (render_file_id)` |
| `catalog_resource_definitions` | `idx_fk_catalog_resource_definitions_fk_catalog_resourc_40d86c31` | `CREATE INDEX idx_fk_catalog_resource_definitions_fk_catalog_resourc_40d86c31 ON public.catalog_resource_definitions USING btree (published_revision_id)` |
| `catalog_tag_members` | `catalog_tag_members_pkey` | `CREATE UNIQUE INDEX catalog_tag_members_pkey ON public.catalog_tag_members USING btree (tag_id, resource_id)` |
| `catalog_tag_members` | `catalog_tag_members_tag_id_ordinal_key` | `CREATE UNIQUE INDEX catalog_tag_members_tag_id_ordinal_key ON public.catalog_tag_members USING btree (tag_id, ordinal)` |
| `catalog_tag_members` | `idx_catalog_tag_members_resource` | `CREATE INDEX idx_catalog_tag_members_resource ON public.catalog_tag_members USING btree (resource_id, tag_id)` |
| `catalog_tag_members` | `idx_fk_catalog_tag_members_fk_catalog_tag_members_revi_23b481ff` | `CREATE INDEX idx_fk_catalog_tag_members_fk_catalog_tag_members_revi_23b481ff ON public.catalog_tag_members USING btree (published_revision_id)` |
| `catalog_tags` | `catalog_tags_pkey` | `CREATE UNIQUE INDEX catalog_tags_pkey ON public.catalog_tags USING btree (entity_id)` |
| `catalog_tags` | `catalog_tags_registry_canonical_id_key` | `CREATE UNIQUE INDEX catalog_tags_registry_canonical_id_key ON public.catalog_tags USING btree (registry, canonical_id)` |
| `change_requests` | `change_requests_pkey` | `CREATE UNIQUE INDEX change_requests_pkey ON public.change_requests USING btree (id)` |
| `change_requests` | `change_requests_proposed_revision_id_key` | `CREATE UNIQUE INDEX change_requests_proposed_revision_id_key ON public.change_requests USING btree (proposed_revision_id)` |
| `change_requests` | `change_requests_public_id_key` | `CREATE UNIQUE INDEX change_requests_public_id_key ON public.change_requests USING btree (public_id)` |
| `change_requests` | `idx_change_requests_contributor` | `CREATE INDEX idx_change_requests_contributor ON public.change_requests USING btree (submitted_by, status, entity_type, entity_id) WHERE (submitted_by IS NOT NULL)` |
| `change_requests` | `idx_change_requests_pending_aggregate` | `CREATE UNIQUE INDEX idx_change_requests_pending_aggregate ON public.change_requests USING btree (aggregate_type, aggregate_key) WHERE (status = 'pending'::text)` |
| `change_requests` | `idx_change_requests_queue` | `CREATE INDEX idx_change_requests_queue ON public.change_requests USING btree (status, submitted_at, id)` |
| `change_requests` | `idx_change_requests_submitted_at` | `CREATE INDEX idx_change_requests_submitted_at ON public.change_requests USING btree (submitted_at, id)` |
| `change_requests` | `idx_fk_change_requests_change_requests_base_revision_i_1bfbf1f2` | `CREATE INDEX idx_fk_change_requests_change_requests_base_revision_i_1bfbf1f2 ON public.change_requests USING btree (base_revision_id)` |
| `change_requests` | `idx_fk_change_requests_change_requests_entity_type_ent_2ae4d1aa` | `CREATE INDEX idx_fk_change_requests_change_requests_entity_type_ent_2ae4d1aa ON public.change_requests USING btree (entity_type, entity_id)` |
| `change_requests` | `idx_fk_change_requests_change_requests_submitted_by_fk_d3b185a6` | `CREATE INDEX idx_fk_change_requests_change_requests_submitted_by_fk_d3b185a6 ON public.change_requests USING btree (submitted_by)` |
| `comment_attachments` | `comment_attachments_pkey` | `CREATE UNIQUE INDEX comment_attachments_pkey ON public.comment_attachments USING btree (comment_id, attachment_file_id)` |
| `comment_attachments` | `idx_comment_attachments_file` | `CREATE INDEX idx_comment_attachments_file ON public.comment_attachments USING btree (attachment_file_id, comment_id)` |
| `comment_closure` | `comment_closure_pkey` | `CREATE UNIQUE INDEX comment_closure_pkey ON public.comment_closure USING btree (ancestor_id, descendant_id)` |
| `comment_closure` | `idx_comment_closure_descendant` | `CREATE INDEX idx_comment_closure_descendant ON public.comment_closure USING btree (descendant_id, depth, ancestor_id)` |
| `comment_floor_counters` | `comment_floor_counters_pkey` | `CREATE UNIQUE INDEX comment_floor_counters_pkey ON public.comment_floor_counters USING btree (target_type, target_id, target_version_key)` |
| `comment_heat_refresh_queue` | `comment_heat_refresh_queue_pkey` | `CREATE UNIQUE INDEX comment_heat_refresh_queue_pkey ON public.comment_heat_refresh_queue USING btree (comment_id)` |
| `comment_heat_refresh_queue` | `idx_comment_heat_refresh_ready` | `CREATE INDEX idx_comment_heat_refresh_ready ON public.comment_heat_refresh_queue USING btree (available_at, updated_at, comment_id)` |
| `comment_log_bindings` | `comment_log_bindings_comment_id_log_share_id_key` | `CREATE UNIQUE INDEX comment_log_bindings_comment_id_log_share_id_key ON public.comment_log_bindings USING btree (comment_id, log_share_id)` |
| `comment_log_bindings` | `comment_log_bindings_pkey` | `CREATE UNIQUE INDEX comment_log_bindings_pkey ON public.comment_log_bindings USING btree (comment_id, attachment_file_id)` |
| `comment_log_bindings` | `idx_comment_log_bindings_attachment` | `CREATE INDEX idx_comment_log_bindings_attachment ON public.comment_log_bindings USING btree (attachment_file_id, comment_id)` |
| `comment_log_bindings` | `idx_comment_log_bindings_share` | `CREATE INDEX idx_comment_log_bindings_share ON public.comment_log_bindings USING btree (log_share_id, comment_id)` |
| `comment_reactions` | `comment_reactions_pkey` | `CREATE UNIQUE INDEX comment_reactions_pkey ON public.comment_reactions USING btree (comment_id, user_id, reaction)` |
| `comment_reactions` | `idx_fk_comment_reactions_comment_reactions_user_id_fke_84ea7c4a` | `CREATE INDEX idx_fk_comment_reactions_comment_reactions_user_id_fke_84ea7c4a ON public.comment_reactions USING btree (user_id)` |
| `comment_watch_replies` | `comment_watch_replies_pkey` | `CREATE UNIQUE INDEX comment_watch_replies_pkey ON public.comment_watch_replies USING btree (watch_id, comment_id)` |
| `comment_watch_replies` | `idx_comment_watch_replies_unread` | `CREATE INDEX idx_comment_watch_replies_unread ON public.comment_watch_replies USING btree (watch_id, created_at, comment_id) WHERE (read_at IS NULL)` |
| `comment_watch_replies` | `idx_fk_comment_watch_replies_comment_watch_replies_com_dcccaa41` | `CREATE INDEX idx_fk_comment_watch_replies_comment_watch_replies_com_dcccaa41 ON public.comment_watch_replies USING btree (comment_id)` |
| `comment_watch_replies` | `idx_fk_comment_watch_replies_comment_watch_replies_not_32c61eeb` | `CREATE INDEX idx_fk_comment_watch_replies_comment_watch_replies_not_32c61eeb ON public.comment_watch_replies USING btree (notification_id)` |
| `comment_watches` | `comment_watches_pkey` | `CREATE UNIQUE INDEX comment_watches_pkey ON public.comment_watches USING btree (id)` |
| `comment_watches` | `comment_watches_public_id_key` | `CREATE UNIQUE INDEX comment_watches_public_id_key ON public.comment_watches USING btree (public_id)` |
| `comment_watches` | `comment_watches_user_id_comment_id_key` | `CREATE UNIQUE INDEX comment_watches_user_id_comment_id_key ON public.comment_watches USING btree (user_id, comment_id)` |
| `comment_watches` | `idx_comment_watches_comment_active` | `CREATE INDEX idx_comment_watches_comment_active ON public.comment_watches USING btree (comment_id, user_id) WHERE (status = 'active'::text)` |
| `comment_watches` | `idx_comment_watches_user_activity` | `CREATE INDEX idx_comment_watches_user_activity ON public.comment_watches USING btree (user_id, status, last_activity_at DESC, id DESC)` |
| `comment_watches` | `idx_fk_comment_watches_comment_watches_comment_id_fkey_e07720f3` | `CREATE INDEX idx_fk_comment_watches_comment_watches_comment_id_fkey_e07720f3 ON public.comment_watches USING btree (comment_id)` |
| `comment_watches` | `idx_fk_comment_watches_comment_watches_last_read_comme_722f5245` | `CREATE INDEX idx_fk_comment_watches_comment_watches_last_read_comme_722f5245 ON public.comment_watches USING btree (last_read_comment_id)` |
| `comments` | `comments_pkey` | `CREATE UNIQUE INDEX comments_pkey ON public.comments USING btree (id)` |
| `comments` | `comments_public_id_key` | `CREATE UNIQUE INDEX comments_public_id_key ON public.comments USING btree (public_id)` |
| `comments` | `idx_comments_author_created` | `CREATE INDEX idx_comments_author_created ON public.comments USING btree (author_id, created_at DESC)` |
| `comments` | `idx_comments_author_idempotency` | `CREATE UNIQUE INDEX idx_comments_author_idempotency ON public.comments USING btree (author_id, idempotency_key) WHERE (idempotency_key <> ''::text)` |
| `comments` | `idx_comments_floor_lookup` | `CREATE INDEX idx_comments_floor_lookup ON public.comments USING btree (target_type, target_id, COALESCE(target_version_id, (0)::bigint), floor_number, status) WHERE (parent_id IS NULL)` |
| `comments` | `idx_comments_parent_created` | `CREATE INDEX idx_comments_parent_created ON public.comments USING btree (parent_id, created_at, id)` |
| `comments` | `idx_comments_root_created` | `CREATE INDEX idx_comments_root_created ON public.comments USING btree (root_id, created_at, id)` |
| `comments` | `idx_comments_target_floor` | `CREATE UNIQUE INDEX idx_comments_target_floor ON public.comments USING btree (target_type, target_id, COALESCE(target_version_id, (0)::bigint), floor_number) WHERE ((parent_id IS NULL) AND (floor_number IS NOT NULL))` |
| `comments` | `idx_comments_target_hot` | `CREATE INDEX idx_comments_target_hot ON public.comments USING btree (target_type, target_id, target_version_id, pinned_at DESC, hot_score DESC, id DESC) WHERE ((parent_id IS NULL) AND (status = 'published'::text))` |
| `comments` | `idx_comments_target_roots` | `CREATE INDEX idx_comments_target_roots ON public.comments USING btree (target_type, target_id, target_version_id, pinned_at DESC, created_at DESC, id DESC) WHERE (parent_id IS NULL)` |
| `comments` | `idx_comments_target_version_published_author` | `CREATE INDEX idx_comments_target_version_published_author ON public.comments USING btree (target_type, target_version_id, author_id) WHERE (status = 'published'::text)` |
| `comments` | `idx_fk_comments_comments_pinned_by_fkey_b88b6677` | `CREATE INDEX idx_fk_comments_comments_pinned_by_fkey_b88b6677 ON public.comments USING btree (pinned_by)` |
| `comments` | `idx_fk_comments_comments_target_version_id_fkey_44f1dc20` | `CREATE INDEX idx_fk_comments_comments_target_version_id_fkey_44f1dc20 ON public.comments USING btree (target_version_id)` |
| `community_post_bounties` | `community_post_bounties_pkey` | `CREATE UNIQUE INDEX community_post_bounties_pkey ON public.community_post_bounties USING btree (post_id)` |
| `community_post_bounties` | `idx_fk_community_post_bounties_community_post_bounties_2dd76959` | `CREATE INDEX idx_fk_community_post_bounties_community_post_bounties_2dd76959 ON public.community_post_bounties USING btree (recipient_id)` |
| `community_post_bounties` | `idx_fk_community_post_bounties_community_post_bounties_47db23b7` | `CREATE INDEX idx_fk_community_post_bounties_community_post_bounties_47db23b7 ON public.community_post_bounties USING btree (currency_id)` |
| `community_post_project_refs` | `community_post_project_refs_pkey` | `CREATE UNIQUE INDEX community_post_project_refs_pkey ON public.community_post_project_refs USING btree (id)` |
| `community_post_project_refs` | `idx_community_post_project_identity` | `CREATE UNIQUE INDEX idx_community_post_project_identity ON public.community_post_project_refs USING btree (post_id, target_type, COALESCE(target_id, (0)::bigint), raw_identifier)` |
| `community_post_project_refs` | `idx_community_post_project_target` | `CREATE INDEX idx_community_post_project_target ON public.community_post_project_refs USING btree (target_type, target_id, post_id) WHERE (target_id IS NOT NULL)` |
| `community_post_project_refs` | `idx_fk_community_post_project_refs_community_post_proj_c08888a7` | `CREATE INDEX idx_fk_community_post_project_refs_community_post_proj_c08888a7 ON public.community_post_project_refs USING btree (target_type, target_id)` |
| `community_post_project_refs` | `idx_fk_community_post_project_refs_community_post_proj_cefd4ba1` | `CREATE INDEX idx_fk_community_post_project_refs_community_post_proj_cefd4ba1 ON public.community_post_project_refs USING btree (post_id)` |
| `community_post_resource_refs` | `community_post_resource_refs_pkey` | `CREATE UNIQUE INDEX community_post_resource_refs_pkey ON public.community_post_resource_refs USING btree (id)` |
| `community_post_resource_refs` | `idx_community_post_resource_identity` | `CREATE UNIQUE INDEX idx_community_post_resource_identity ON public.community_post_resource_refs USING btree (post_id, kind_code, COALESCE(resource_id, (0)::bigint), raw_resource_id)` |
| `community_post_resource_refs` | `idx_community_post_resource_target` | `CREATE INDEX idx_community_post_resource_target ON public.community_post_resource_refs USING btree (resource_id, post_id) WHERE (resource_id IS NOT NULL)` |
| `community_post_resource_refs` | `idx_fk_community_post_resource_refs_community_post_res_9f004787` | `CREATE INDEX idx_fk_community_post_resource_refs_community_post_res_9f004787 ON public.community_post_resource_refs USING btree (resource_id)` |
| `community_post_resource_refs` | `idx_fk_community_post_resource_refs_community_post_res_f5cb87dc` | `CREATE INDEX idx_fk_community_post_resource_refs_community_post_res_f5cb87dc ON public.community_post_resource_refs USING btree (post_id)` |
| `community_post_translations` | `community_post_translations_pkey` | `CREATE UNIQUE INDEX community_post_translations_pkey ON public.community_post_translations USING btree (post_id, locale)` |
| `community_post_translations` | `idx_fk_community_post_translations_community_post_tran_380737bd` | `CREATE INDEX idx_fk_community_post_translations_community_post_tran_380737bd ON public.community_post_translations USING btree (source_revision_id)` |
| `community_post_translations` | `idx_fk_community_post_translations_community_post_tran_82f8f1f2` | `CREATE INDEX idx_fk_community_post_translations_community_post_tran_82f8f1f2 ON public.community_post_translations USING btree (ai_task_id)` |
| `community_posts` | `community_posts_pkey` | `CREATE UNIQUE INDEX community_posts_pkey ON public.community_posts USING btree (id)` |
| `community_posts` | `community_posts_public_id_key` | `CREATE UNIQUE INDEX community_posts_public_id_key ON public.community_posts USING btree (public_id)` |
| `community_posts` | `idx_community_posts_author` | `CREATE INDEX idx_community_posts_author ON public.community_posts USING btree (author_id, created_at DESC, id DESC)` |
| `community_posts` | `idx_community_posts_catalog` | `CREATE INDEX idx_community_posts_catalog ON public.community_posts USING btree (kind, review_status, published_at DESC, id DESC) WHERE (status = 'active'::text)` |
| `community_posts` | `idx_community_posts_category_catalog` | `CREATE INDEX idx_community_posts_category_catalog ON public.community_posts USING btree (kind, category, review_status, published_at DESC, id DESC) WHERE (status = 'active'::text)` |
| `community_posts` | `idx_fk_community_posts_community_posts_accepted_commen_e4fc9855` | `CREATE INDEX idx_fk_community_posts_community_posts_accepted_commen_e4fc9855 ON public.community_posts USING btree (accepted_comment_id)` |
| `community_posts` | `idx_fk_community_posts_community_posts_cover_file_id_f_a0c11e0d` | `CREATE INDEX idx_fk_community_posts_community_posts_cover_file_id_f_a0c11e0d ON public.community_posts USING btree (cover_file_id)` |
| `community_posts` | `idx_fk_community_posts_community_posts_published_revis_e45cd286` | `CREATE INDEX idx_fk_community_posts_community_posts_published_revis_e45cd286 ON public.community_posts USING btree (published_revision_id)` |
| `content_change_items` | `content_change_items_pkey` | `CREATE UNIQUE INDEX content_change_items_pkey ON public.content_change_items USING btree (id)` |
| `content_change_items` | `idx_content_change_items_revision` | `CREATE INDEX idx_content_change_items_revision ON public.content_change_items USING btree (revision_id, id)` |
| `content_creator_bindings` | `content_creator_bindings_pkey` | `CREATE UNIQUE INDEX content_creator_bindings_pkey ON public.content_creator_bindings USING btree (id)` |
| `content_creator_bindings` | `content_creator_bindings_public_id_key` | `CREATE UNIQUE INDEX content_creator_bindings_public_id_key ON public.content_creator_bindings USING btree (public_id)` |
| `content_creator_bindings` | `idx_content_creator_bindings_access` | `CREATE INDEX idx_content_creator_bindings_access ON public.content_creator_bindings USING btree (creator_id, subject_type, subject_id) WHERE ((status = 'approved'::text) AND permission_granting)` |
| `content_creator_bindings` | `idx_content_creator_bindings_creator` | `CREATE INDEX idx_content_creator_bindings_creator ON public.content_creator_bindings USING btree (creator_id, subject_type, subject_id)` |
| `content_creator_bindings` | `idx_content_creator_bindings_subject_order` | `CREATE INDEX idx_content_creator_bindings_subject_order ON public.content_creator_bindings USING btree (subject_type, subject_id, display_order, id)` |
| `content_creator_bindings` | `idx_content_creator_bindings_unique` | `CREATE UNIQUE INDEX idx_content_creator_bindings_unique ON public.content_creator_bindings USING btree (subject_type, subject_id, creator_id, COALESCE(role_id, (0)::bigint))` |
| `content_creator_bindings` | `idx_fk_content_creator_bindings_content_creator_bindin_5795d565` | `CREATE INDEX idx_fk_content_creator_bindings_content_creator_bindin_5795d565 ON public.content_creator_bindings USING btree (approved_by)` |
| `content_creator_bindings` | `idx_fk_content_creator_bindings_fk_content_creator_bin_9a588bf3` | `CREATE INDEX idx_fk_content_creator_bindings_fk_content_creator_bin_9a588bf3 ON public.content_creator_bindings USING btree (role_id)` |
| `content_download_counters` | `content_download_counters_pkey` | `CREATE UNIQUE INDEX content_download_counters_pkey ON public.content_download_counters USING btree (object_route_id, owner_id)` |
| `content_download_counters` | `idx_content_download_counters_owner` | `CREATE INDEX idx_content_download_counters_owner ON public.content_download_counters USING btree (owner_id, downloads DESC)` |
| `content_download_reward_counters` | `content_download_reward_counters_pkey` | `CREATE UNIQUE INDEX content_download_reward_counters_pkey ON public.content_download_reward_counters USING btree (object_route_id, owner_id, currency_id)` |
| `content_download_reward_counters` | `idx_fk_content_download_reward_counters_content_downlo_6babdfcf` | `CREATE INDEX idx_fk_content_download_reward_counters_content_downlo_6babdfcf ON public.content_download_reward_counters USING btree (currency_id)` |
| `content_download_reward_counters` | `idx_fk_content_download_reward_counters_content_downlo_a562a8ef` | `CREATE INDEX idx_fk_content_download_reward_counters_content_downlo_a562a8ef ON public.content_download_reward_counters USING btree (owner_id)` |
| `content_heat_promotions` | `content_heat_promotions_pkey` | `CREATE UNIQUE INDEX content_heat_promotions_pkey ON public.content_heat_promotions USING btree (id)` |
| `content_heat_promotions` | `idx_content_heat_promotions_active` | `CREATE INDEX idx_content_heat_promotions_active ON public.content_heat_promotions USING btree (object_route_id, expires_at DESC, started_at DESC)` |
| `content_heat_promotions` | `idx_content_heat_promotions_user` | `CREATE INDEX idx_content_heat_promotions_user ON public.content_heat_promotions USING btree (applied_by, started_at DESC, id DESC)` |
| `content_heat_promotions` | `idx_fk_content_heat_promotions_content_heat_promotions_96e60280` | `CREATE INDEX idx_fk_content_heat_promotions_content_heat_promotions_96e60280 ON public.content_heat_promotions USING btree (shop_item_id)` |
| `content_localizations` | `content_localizations_pkey` | `CREATE UNIQUE INDEX content_localizations_pkey ON public.content_localizations USING btree (subject_type, subject_id, locale)` |
| `content_localizations` | `idx_content_localizations_catalog` | `CREATE INDEX idx_content_localizations_catalog ON public.content_localizations USING btree (catalog_entity_id, locale) WHERE (catalog_entity_id IS NOT NULL)` |
| `content_localizations` | `idx_content_localizations_locale_name` | `CREATE INDEX idx_content_localizations_locale_name ON public.content_localizations USING btree (locale, lower(name), subject_type, subject_id)` |
| `content_localizations` | `idx_fk_content_localizations_content_localizations_ai__a35cfae5` | `CREATE INDEX idx_fk_content_localizations_content_localizations_ai__a35cfae5 ON public.content_localizations USING btree (ai_task_id)` |
| `content_localizations` | `idx_fk_content_localizations_content_localizations_cat_a9788a37` | `CREATE INDEX idx_fk_content_localizations_content_localizations_cat_a9788a37 ON public.content_localizations USING btree (catalog_entity_id, subject_type)` |
| `content_localizations` | `idx_fk_content_localizations_content_localizations_cat_c3c0fbf7` | `CREATE INDEX idx_fk_content_localizations_content_localizations_cat_c3c0fbf7 ON public.content_localizations USING btree (catalog_entity_id)` |
| `content_localizations` | `idx_fk_content_localizations_content_localizations_upd_0bd615ab` | `CREATE INDEX idx_fk_content_localizations_content_localizations_upd_0bd615ab ON public.content_localizations USING btree (updated_by)` |
| `content_localizations` | `idx_fk_content_localizations_fk_content_localizations__fad9b9ac` | `CREATE INDEX idx_fk_content_localizations_fk_content_localizations__fad9b9ac ON public.content_localizations USING btree (published_revision_id)` |
| `content_popularity_daily_snapshots` | `content_popularity_daily_snapshots_pkey` | `CREATE UNIQUE INDEX content_popularity_daily_snapshots_pkey ON public.content_popularity_daily_snapshots USING btree (object_route_id, metric_date)` |
| `content_popularity_daily_snapshots` | `idx_content_popularity_snapshots_date` | `CREATE INDEX idx_content_popularity_snapshots_date ON public.content_popularity_daily_snapshots USING btree (metric_date, object_route_id)` |
| `content_popularity_events_daily` | `content_popularity_events_daily_pkey` | `CREATE UNIQUE INDEX content_popularity_events_daily_pkey ON public.content_popularity_events_daily USING btree (object_route_id, event_date)` |
| `content_popularity_events_daily` | `idx_content_popularity_events_retention` | `CREATE INDEX idx_content_popularity_events_retention ON public.content_popularity_events_daily USING btree (event_date, object_route_id)` |
| `content_popularity_stats` | `content_popularity_stats_pkey` | `CREATE UNIQUE INDEX content_popularity_stats_pkey ON public.content_popularity_stats USING btree (object_route_id)` |
| `content_popularity_stats` | `idx_content_popularity_heat` | `CREATE INDEX idx_content_popularity_heat ON public.content_popularity_stats USING btree (heat_score DESC, object_route_id)` |
| `content_popularity_stats` | `idx_content_popularity_rating` | `CREATE INDEX idx_content_popularity_rating ON public.content_popularity_stats USING btree (bayesian_rating DESC, rating_count DESC, object_route_id)` |
| `content_popularity_thresholds` | `content_popularity_thresholds_pkey` | `CREATE UNIQUE INDEX content_popularity_thresholds_pkey ON public.content_popularity_thresholds USING btree (entity_type)` |
| `content_project_pages` | `content_project_pages_pkey` | `CREATE UNIQUE INDEX content_project_pages_pkey ON public.content_project_pages USING btree (object_route_id, page_hash)` |
| `content_rating_global_stats` | `content_rating_global_stats_pkey` | `CREATE UNIQUE INDEX content_rating_global_stats_pkey ON public.content_rating_global_stats USING btree (entity_type)` |
| `content_rating_scores` | `content_rating_scores_pkey` | `CREATE UNIQUE INDEX content_rating_scores_pkey ON public.content_rating_scores USING btree (rating_id, dimension_code)` |
| `content_rating_scores` | `idx_content_rating_scores_dimension` | `CREATE INDEX idx_content_rating_scores_dimension ON public.content_rating_scores USING btree (dimension_code, rating_id)` |
| `content_ratings` | `content_ratings_object_route_id_author_id_key` | `CREATE UNIQUE INDEX content_ratings_object_route_id_author_id_key ON public.content_ratings USING btree (object_route_id, author_id)` |
| `content_ratings` | `content_ratings_pkey` | `CREATE UNIQUE INDEX content_ratings_pkey ON public.content_ratings USING btree (id)` |
| `content_ratings` | `content_ratings_public_id_key` | `CREATE UNIQUE INDEX content_ratings_public_id_key ON public.content_ratings USING btree (public_id)` |
| `content_ratings` | `idx_content_ratings_author` | `CREATE INDEX idx_content_ratings_author ON public.content_ratings USING btree (author_id, updated_at DESC, id DESC)` |
| `content_ratings` | `idx_content_ratings_target` | `CREATE INDEX idx_content_ratings_target ON public.content_ratings USING btree (object_route_id, status, updated_at DESC, id DESC)` |
| `content_revisions` | `content_revisions_aggregate_type_aggregate_key_revision_no_key` | `CREATE UNIQUE INDEX content_revisions_aggregate_type_aggregate_key_revision_no_key ON public.content_revisions USING btree (aggregate_type, aggregate_key, revision_no)` |
| `content_revisions` | `content_revisions_pkey` | `CREATE UNIQUE INDEX content_revisions_pkey ON public.content_revisions USING btree (id)` |
| `content_revisions` | `content_revisions_public_id_key` | `CREATE UNIQUE INDEX content_revisions_public_id_key ON public.content_revisions USING btree (public_id)` |
| `content_revisions` | `idx_content_revisions_aggregate` | `CREATE INDEX idx_content_revisions_aggregate ON public.content_revisions USING btree (aggregate_type, aggregate_key, revision_no DESC)` |
| `content_revisions` | `idx_content_revisions_entity` | `CREATE INDEX idx_content_revisions_entity ON public.content_revisions USING btree (entity_type, entity_id, revision_no DESC) WHERE (entity_id IS NOT NULL)` |
| `content_revisions` | `idx_fk_content_revisions_content_revisions_base_revisi_7630ae8b` | `CREATE INDEX idx_fk_content_revisions_content_revisions_base_revisi_7630ae8b ON public.content_revisions USING btree (base_revision_id)` |
| `content_revisions` | `idx_fk_content_revisions_content_revisions_created_by__9901d3c8` | `CREATE INDEX idx_fk_content_revisions_content_revisions_created_by__9901d3c8 ON public.content_revisions USING btree (created_by)` |
| `content_revisions` | `idx_fk_content_revisions_content_revisions_entity_type_07fa3c36` | `CREATE INDEX idx_fk_content_revisions_content_revisions_entity_type_07fa3c36 ON public.content_revisions USING btree (entity_type, entity_id)` |
| `content_route_metrics` | `content_route_metrics_pkey` | `CREATE UNIQUE INDEX content_route_metrics_pkey ON public.content_route_metrics USING btree (object_route_id)` |
| `content_route_metrics` | `idx_content_route_metrics_views` | `CREATE INDEX idx_content_route_metrics_views ON public.content_route_metrics USING btree (total_view_count DESC, object_route_id)` |
| `content_stats_refresh_queue` | `content_stats_refresh_queue_pkey` | `CREATE UNIQUE INDEX content_stats_refresh_queue_pkey ON public.content_stats_refresh_queue USING btree (object_route_id)` |
| `content_stats_refresh_queue` | `idx_content_stats_refresh_ready` | `CREATE INDEX idx_content_stats_refresh_ready ON public.content_stats_refresh_queue USING btree (available_at, updated_at, object_route_id)` |
| `content_subjects` | `content_subjects_pkey` | `CREATE UNIQUE INDEX content_subjects_pkey ON public.content_subjects USING btree (subject_type, subject_id)` |
| `content_unique_views` | `content_unique_views_pkey` | `CREATE UNIQUE INDEX content_unique_views_pkey ON public.content_unique_views USING btree (object_route_id, viewer_hash)` |
| `content_unique_views` | `idx_content_unique_views_route_recent_user` | `CREATE INDEX idx_content_unique_views_route_recent_user ON public.content_unique_views USING btree (object_route_id, last_seen_at DESC, viewer_user_id) WHERE (viewer_user_id IS NOT NULL)` |
| `content_unique_views` | `idx_content_unique_views_user` | `CREATE INDEX idx_content_unique_views_user ON public.content_unique_views USING btree (viewer_user_id, object_route_id) WHERE (viewer_user_id IS NOT NULL)` |
| `content_unique_views` | `idx_fk_content_unique_views_content_unique_views_viewe_15648cb1` | `CREATE INDEX idx_fk_content_unique_views_content_unique_views_viewe_15648cb1 ON public.content_unique_views USING btree (viewer_user_id)` |
| `content_view_daily` | `content_view_daily_pkey` | `CREATE UNIQUE INDEX content_view_daily_pkey ON public.content_view_daily USING btree (object_route_id, view_date, counter_shard)` |
| `content_view_daily` | `idx_content_view_daily_retention` | `CREATE INDEX idx_content_view_daily_retention ON public.content_view_daily USING btree (view_date, object_route_id)` |
| `creator_claim_attachments` | `creator_claim_attachments_pkey` | `CREATE UNIQUE INDEX creator_claim_attachments_pkey ON public.creator_claim_attachments USING btree (claim_id, oss_file_id)` |
| `creator_claim_attachments` | `idx_creator_claim_attachments_file` | `CREATE INDEX idx_creator_claim_attachments_file ON public.creator_claim_attachments USING btree (oss_file_id, claim_id)` |
| `creator_claims` | `creator_claims_pkey` | `CREATE UNIQUE INDEX creator_claims_pkey ON public.creator_claims USING btree (id)` |
| `creator_claims` | `creator_claims_public_id_key` | `CREATE UNIQUE INDEX creator_claims_public_id_key ON public.creator_claims USING btree (public_id)` |
| `creator_claims` | `idx_creator_claims_open` | `CREATE UNIQUE INDEX idx_creator_claims_open ON public.creator_claims USING btree (creator_id, user_id) WHERE (status = 'pending'::text)` |
| `creator_claims` | `idx_creator_claims_queue` | `CREATE INDEX idx_creator_claims_queue ON public.creator_claims USING btree (status, created_at, id)` |
| `creator_claims` | `idx_creator_claims_user_approved` | `CREATE INDEX idx_creator_claims_user_approved ON public.creator_claims USING btree (user_id, creator_id) WHERE (status = 'approved'::text)` |
| `creator_claims` | `idx_fk_creator_claims_creator_claims_creator_id_fkey_829883ad` | `CREATE INDEX idx_fk_creator_claims_creator_claims_creator_id_fkey_829883ad ON public.creator_claims USING btree (creator_id)` |
| `creator_claims` | `idx_fk_creator_claims_creator_claims_reviewed_by_fkey_b344a5df` | `CREATE INDEX idx_fk_creator_claims_creator_claims_reviewed_by_fkey_b344a5df ON public.creator_claims USING btree (reviewed_by)` |
| `creator_claims` | `idx_fk_creator_claims_creator_claims_user_id_fkey_d98e016a` | `CREATE INDEX idx_fk_creator_claims_creator_claims_user_id_fkey_d98e016a ON public.creator_claims USING btree (user_id)` |
| `creator_claims` | `uq_creator_claims_approved_author` | `CREATE UNIQUE INDEX uq_creator_claims_approved_author ON public.creator_claims USING btree (creator_id) WHERE (status = 'approved'::text)` |
| `creator_links` | `creator_links_creator_id_link_type_url_key` | `CREATE UNIQUE INDEX creator_links_creator_id_link_type_url_key ON public.creator_links USING btree (creator_id, link_type, url)` |
| `creator_links` | `creator_links_pkey` | `CREATE UNIQUE INDEX creator_links_pkey ON public.creator_links USING btree (id)` |
| `creator_links` | `idx_creator_links_order` | `CREATE INDEX idx_creator_links_order ON public.creator_links USING btree (creator_id, display_order, id)` |
| `creator_role_definitions` | `creator_role_definitions_code_key` | `CREATE UNIQUE INDEX creator_role_definitions_code_key ON public.creator_role_definitions USING btree (code)` |
| `creator_role_definitions` | `creator_role_definitions_pkey` | `CREATE UNIQUE INDEX creator_role_definitions_pkey ON public.creator_role_definitions USING btree (id)` |
| `creator_role_definitions` | `creator_role_definitions_public_id_key` | `CREATE UNIQUE INDEX creator_role_definitions_public_id_key ON public.creator_role_definitions USING btree (public_id)` |
| `creator_role_definitions` | `idx_fk_creator_role_definitions_creator_role_definitio_20ec6ac8` | `CREATE INDEX idx_fk_creator_role_definitions_creator_role_definitio_20ec6ac8 ON public.creator_role_definitions USING btree (created_by)` |
| `creator_team_members` | `creator_team_members_pkey` | `CREATE UNIQUE INDEX creator_team_members_pkey ON public.creator_team_members USING btree (team_id, member_creator_id, role_id)` |
| `creator_team_members` | `idx_creator_team_members_approved_member` | `CREATE INDEX idx_creator_team_members_approved_member ON public.creator_team_members USING btree (member_creator_id, team_id) WHERE (status = 'approved'::text)` |
| `creator_team_members` | `idx_creator_team_members_member` | `CREATE INDEX idx_creator_team_members_member ON public.creator_team_members USING btree (member_creator_id, team_id)` |
| `creator_team_members` | `idx_fk_creator_team_members_creator_team_members_appro_0d62f658` | `CREATE INDEX idx_fk_creator_team_members_creator_team_members_appro_0d62f658 ON public.creator_team_members USING btree (approved_by)` |
| `creator_team_members` | `idx_fk_creator_team_members_creator_team_members_creat_b8a2757d` | `CREATE INDEX idx_fk_creator_team_members_creator_team_members_creat_b8a2757d ON public.creator_team_members USING btree (created_by)` |
| `creator_team_members` | `idx_fk_creator_team_members_creator_team_members_role__94583194` | `CREATE INDEX idx_fk_creator_team_members_creator_team_members_role__94583194 ON public.creator_team_members USING btree (role_id)` |
| `creators` | `creators_pkey` | `CREATE UNIQUE INDEX creators_pkey ON public.creators USING btree (id)` |
| `creators` | `creators_public_id_key` | `CREATE UNIQUE INDEX creators_public_id_key ON public.creators USING btree (public_id)` |
| `creators` | `idx_creators_catalog` | `CREATE INDEX idx_creators_catalog ON public.creators USING btree (kind, review_status, lower(name), id)` |
| `creators` | `idx_creators_kind_normalized_name` | `CREATE INDEX idx_creators_kind_normalized_name ON public.creators USING btree (kind, normalized_name)` |
| `creators` | `idx_fk_creators_creators_avatar_file_id_fkey_d621ff29` | `CREATE INDEX idx_fk_creators_creators_avatar_file_id_fkey_d621ff29 ON public.creators USING btree (avatar_file_id)` |
| `creators` | `idx_fk_creators_creators_created_by_fkey_6b7cecbb` | `CREATE INDEX idx_fk_creators_creators_created_by_fkey_6b7cecbb ON public.creators USING btree (created_by)` |
| `creators` | `idx_fk_creators_creators_published_revision_id_fkey_ff7ed9f1` | `CREATE INDEX idx_fk_creators_creators_published_revision_id_fkey_ff7ed9f1 ON public.creators USING btree (published_revision_id)` |
| `currencies` | `currencies_code_key` | `CREATE UNIQUE INDEX currencies_code_key ON public.currencies USING btree (code)` |
| `currencies` | `currencies_pkey` | `CREATE UNIQUE INDEX currencies_pkey ON public.currencies USING btree (id)` |
| `currencies` | `currencies_public_id_key` | `CREATE UNIQUE INDEX currencies_public_id_key ON public.currencies USING btree (public_id)` |
| `currency_transactions` | `currency_transactions_pkey` | `CREATE UNIQUE INDEX currency_transactions_pkey ON public.currency_transactions USING btree (id)` |
| `currency_transactions` | `idx_currency_transactions_reference` | `CREATE INDEX idx_currency_transactions_reference ON public.currency_transactions USING btree (reference_type, reference_key, created_at DESC)` |
| `currency_transactions` | `idx_currency_transactions_user` | `CREATE INDEX idx_currency_transactions_user ON public.currency_transactions USING btree (user_id, currency_id, created_at DESC, id DESC)` |
| `currency_transactions` | `idx_fk_currency_transactions_currency_transactions_cou_6dc4f699` | `CREATE INDEX idx_fk_currency_transactions_currency_transactions_cou_6dc4f699 ON public.currency_transactions USING btree (counterparty_user_id)` |
| `currency_transactions` | `idx_fk_currency_transactions_currency_transactions_cur_8617e080` | `CREATE INDEX idx_fk_currency_transactions_currency_transactions_cur_8617e080 ON public.currency_transactions USING btree (currency_id)` |
| `dead_letter_events` | `dead_letter_events_event_id_failure_stage_key` | `CREATE UNIQUE INDEX dead_letter_events_event_id_failure_stage_key ON public.dead_letter_events USING btree (event_id, failure_stage)` |
| `dead_letter_events` | `dead_letter_events_pkey` | `CREATE UNIQUE INDEX dead_letter_events_pkey ON public.dead_letter_events USING btree (id)` |
| `dead_letter_events` | `idx_dead_letter_events_pending` | `CREATE INDEX idx_dead_letter_events_pending ON public.dead_letter_events USING btree (failed_at DESC) WHERE (replayed_at IS NULL)` |
| `direct_conversations` | `direct_conversations_pkey` | `CREATE UNIQUE INDEX direct_conversations_pkey ON public.direct_conversations USING btree (id)` |
| `direct_conversations` | `direct_conversations_public_id_key` | `CREATE UNIQUE INDEX direct_conversations_public_id_key ON public.direct_conversations USING btree (public_id)` |
| `direct_conversations` | `direct_conversations_user_low_id_user_high_id_key` | `CREATE UNIQUE INDEX direct_conversations_user_low_id_user_high_id_key ON public.direct_conversations USING btree (user_low_id, user_high_id)` |
| `direct_conversations` | `idx_direct_conversations_updated_at` | `CREATE INDEX idx_direct_conversations_updated_at ON public.direct_conversations USING btree (updated_at DESC)` |
| `direct_conversations` | `idx_fk_direct_conversations_direct_conversations_user__d3329f91` | `CREATE INDEX idx_fk_direct_conversations_direct_conversations_user__d3329f91 ON public.direct_conversations USING btree (user_high_id)` |
| `direct_messages` | `direct_messages_pkey` | `CREATE UNIQUE INDEX direct_messages_pkey ON public.direct_messages USING btree (id)` |
| `direct_messages` | `direct_messages_public_id_key` | `CREATE UNIQUE INDEX direct_messages_public_id_key ON public.direct_messages USING btree (public_id)` |
| `direct_messages` | `idx_direct_messages_conversation_created_at` | `CREATE INDEX idx_direct_messages_conversation_created_at ON public.direct_messages USING btree (conversation_id, created_at DESC)` |
| `direct_messages` | `idx_direct_messages_recipient_read` | `CREATE INDEX idx_direct_messages_recipient_read ON public.direct_messages USING btree (recipient_id, read_at, created_at DESC)` |
| `direct_messages` | `idx_fk_direct_messages_direct_messages_sender_id_fkey_1758d40f` | `CREATE INDEX idx_fk_direct_messages_direct_messages_sender_id_fkey_1758d40f ON public.direct_messages USING btree (sender_id)` |
| `email_verification_codes` | `email_verification_codes_pkey` | `CREATE UNIQUE INDEX email_verification_codes_pkey ON public.email_verification_codes USING btree (id)` |
| `email_verification_codes` | `idx_email_verification_codes_lookup` | `CREATE INDEX idx_email_verification_codes_lookup ON public.email_verification_codes USING btree (email, purpose, expires_at DESC)` |
| `email_verification_codes` | `idx_email_verification_codes_rate_email` | `CREATE INDEX idx_email_verification_codes_rate_email ON public.email_verification_codes USING btree (email, purpose, created_at DESC)` |
| `email_verification_codes` | `idx_email_verification_codes_rate_ip` | `CREATE INDEX idx_email_verification_codes_rate_ip ON public.email_verification_codes USING btree (request_ip, created_at DESC) WHERE (request_ip <> ''::text)` |
| `experience_transactions` | `experience_transactions_pkey` | `CREATE UNIQUE INDEX experience_transactions_pkey ON public.experience_transactions USING btree (id)` |
| `experience_transactions` | `idx_experience_transactions_user` | `CREATE INDEX idx_experience_transactions_user ON public.experience_transactions USING btree (user_id, created_at DESC, id DESC)` |
| `external_release_bindings` | `external_release_bindings_pkey` | `CREATE UNIQUE INDEX external_release_bindings_pkey ON public.external_release_bindings USING btree (id)` |
| `external_release_bindings` | `external_release_bindings_source_type_external_release_id_key` | `CREATE UNIQUE INDEX external_release_bindings_source_type_external_release_id_key ON public.external_release_bindings USING btree (source_type, external_release_id)` |
| `external_release_bindings` | `idx_fk_external_release_bindings_external_release_bind_cde13408` | `CREATE INDEX idx_fk_external_release_bindings_external_release_bind_cde13408 ON public.external_release_bindings USING btree (project_route_id)` |
| `favorite_collection_items` | `favorite_collection_items_collection_id_entity_type_entity__key` | `CREATE UNIQUE INDEX favorite_collection_items_collection_id_entity_type_entity__key ON public.favorite_collection_items USING btree (collection_id, entity_type, entity_id)` |
| `favorite_collection_items` | `favorite_collection_items_pkey` | `CREATE UNIQUE INDEX favorite_collection_items_pkey ON public.favorite_collection_items USING btree (id)` |
| `favorite_collection_items` | `idx_favorite_items_entity` | `CREATE INDEX idx_favorite_items_entity ON public.favorite_collection_items USING btree (entity_type, entity_id)` |
| `favorite_collections` | `favorite_collections_pkey` | `CREATE UNIQUE INDEX favorite_collections_pkey ON public.favorite_collections USING btree (id)` |
| `favorite_collections` | `favorite_collections_public_id_key` | `CREATE UNIQUE INDEX favorite_collections_public_id_key ON public.favorite_collections USING btree (public_id)` |
| `favorite_collections` | `favorite_collections_user_id_name_key` | `CREATE UNIQUE INDEX favorite_collections_user_id_name_key ON public.favorite_collections USING btree (user_id, name)` |
| `favorite_collections` | `idx_favorite_collections_default` | `CREATE UNIQUE INDEX idx_favorite_collections_default ON public.favorite_collections USING btree (user_id) WHERE is_default` |
| `favorite_collections` | `idx_favorite_collections_public` | `CREATE INDEX idx_favorite_collections_public ON public.favorite_collections USING btree (user_id, created_at, id) WHERE is_public` |
| `favorite_modpack_export_items` | `favorite_modpack_export_items_pkey` | `CREATE UNIQUE INDEX favorite_modpack_export_items_pkey ON public.favorite_modpack_export_items USING btree (id)` |
| `favorite_modpack_export_items` | `idx_favorite_modpack_export_items_task_result` | `CREATE INDEX idx_favorite_modpack_export_items_task_result ON public.favorite_modpack_export_items USING btree (task_id, result_type, id)` |
| `favorite_modpack_export_items` | `idx_fk_favorite_modpack_export_items_favorite_modpack__a60679ce` | `CREATE INDEX idx_fk_favorite_modpack_export_items_favorite_modpack__a60679ce ON public.favorite_modpack_export_items USING btree (source_project_route_id)` |
| `favorite_modpack_export_items` | `uq_favorite_modpack_export_item_file` | `CREATE UNIQUE INDEX uq_favorite_modpack_export_item_file ON public.favorite_modpack_export_items USING btree (task_id, modrinth_project_id, modrinth_version_id, selected_file_name) WHERE (result_type = ANY (ARRAY['exported'::text, 'auto_dependency'::text]))` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_pkey` | `CREATE UNIQUE INDEX favorite_modpack_export_tasks_pkey ON public.favorite_modpack_export_tasks USING btree (id)` |
| `favorite_modpack_export_tasks` | `favorite_modpack_export_tasks_public_id_key` | `CREATE UNIQUE INDEX favorite_modpack_export_tasks_public_id_key ON public.favorite_modpack_export_tasks USING btree (public_id)` |
| `favorite_modpack_export_tasks` | `idx_favorite_modpack_export_tasks_expiry` | `CREATE INDEX idx_favorite_modpack_export_tasks_expiry ON public.favorite_modpack_export_tasks USING btree (expires_at, id) WHERE (status = 'ready'::text)` |
| `favorite_modpack_export_tasks` | `idx_favorite_modpack_export_tasks_owner_created` | `CREATE INDEX idx_favorite_modpack_export_tasks_owner_created ON public.favorite_modpack_export_tasks USING btree (owner_user_id, created_at DESC, id DESC)` |
| `favorite_modpack_export_tasks` | `idx_favorite_modpack_export_tasks_queue` | `CREATE INDEX idx_favorite_modpack_export_tasks_queue ON public.favorite_modpack_export_tasks USING btree (status, created_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'processing'::text]))` |
| `favorite_modpack_export_tasks` | `idx_fk_favorite_modpack_export_tasks_favorite_modpack__24f4ae5f` | `CREATE INDEX idx_fk_favorite_modpack_export_tasks_favorite_modpack__24f4ae5f ON public.favorite_modpack_export_tasks USING btree (result_file_id)` |
| `favorite_modpack_export_tasks` | `idx_fk_favorite_modpack_export_tasks_favorite_modpack__83c276a8` | `CREATE INDEX idx_fk_favorite_modpack_export_tasks_favorite_modpack__83c276a8 ON public.favorite_modpack_export_tasks USING btree (collection_id)` |
| `game_resource_aliases` | `game_resource_aliases_pkey` | `CREATE UNIQUE INDEX game_resource_aliases_pkey ON public.game_resource_aliases USING btree (kind_code, alias_id)` |
| `game_resource_aliases` | `idx_fk_game_resource_aliases_game_resource_aliases_res_ed146eec` | `CREATE INDEX idx_fk_game_resource_aliases_game_resource_aliases_res_ed146eec ON public.game_resource_aliases USING btree (resource_id)` |
| `game_resource_asset_bindings` | `game_resource_asset_bindings_pkey` | `CREATE UNIQUE INDEX game_resource_asset_bindings_pkey ON public.game_resource_asset_bindings USING btree (snapshot_id)` |
| `game_resource_asset_bindings` | `idx_fk_game_resource_asset_bindings_game_resource_asse_bd36e021` | `CREATE INDEX idx_fk_game_resource_asset_bindings_game_resource_asse_bd36e021 ON public.game_resource_asset_bindings USING btree (item_resource_id)` |
| `game_resource_asset_bindings` | `idx_fk_game_resource_asset_bindings_game_resource_asse_e7ceb4fa` | `CREATE INDEX idx_fk_game_resource_asset_bindings_game_resource_asse_e7ceb4fa ON public.game_resource_asset_bindings USING btree (block_resource_id)` |
| `game_resource_asset_bindings` | `idx_game_resource_asset_bindings_block` | `CREATE INDEX idx_game_resource_asset_bindings_block ON public.game_resource_asset_bindings USING btree (block_resource_id) WHERE (block_resource_id IS NOT NULL)` |
| `game_resource_asset_bindings` | `idx_game_resource_asset_bindings_item` | `CREATE INDEX idx_game_resource_asset_bindings_item ON public.game_resource_asset_bindings USING btree (item_resource_id) WHERE (item_resource_id IS NOT NULL)` |
| `game_resources` | `game_resources_kind_code_canonical_id_key` | `CREATE UNIQUE INDEX game_resources_kind_code_canonical_id_key ON public.game_resources USING btree (kind_code, canonical_id)` |
| `game_resources` | `game_resources_pkey` | `CREATE UNIQUE INDEX game_resources_pkey ON public.game_resources USING btree (entity_id)` |
| `game_resources` | `idx_fk_game_resources_game_resources_created_from_revi_51ff62b2` | `CREATE INDEX idx_fk_game_resources_game_resources_created_from_revi_51ff62b2 ON public.game_resources USING btree (created_from_revision_id)` |
| `game_resources` | `idx_fk_game_resources_game_resources_owner_mod_id_fkey_c7ee7682` | `CREATE INDEX idx_fk_game_resources_game_resources_owner_mod_id_fkey_c7ee7682 ON public.game_resources USING btree (owner_mod_id)` |
| `game_resources` | `idx_game_resources_canonical` | `CREATE INDEX idx_game_resources_canonical ON public.game_resources USING btree (canonical_id, kind_code)` |
| `game_resources` | `idx_game_resources_namespace` | `CREATE INDEX idx_game_resources_namespace ON public.game_resources USING btree (namespace, kind_code, resource_path)` |
| `knowledge_pages` | `idx_fk_knowledge_pages_fk_knowledge_pages_revision_224e72ff` | `CREATE INDEX idx_fk_knowledge_pages_fk_knowledge_pages_revision_224e72ff ON public.knowledge_pages USING btree (published_revision_id)` |
| `knowledge_pages` | `idx_fk_knowledge_pages_knowledge_pages_updated_by_fkey_e365af4b` | `CREATE INDEX idx_fk_knowledge_pages_knowledge_pages_updated_by_fkey_e365af4b ON public.knowledge_pages USING btree (updated_by)` |
| `knowledge_pages` | `knowledge_pages_pkey` | `CREATE UNIQUE INDEX knowledge_pages_pkey ON public.knowledge_pages USING btree (entity_id, locale)` |
| `level_system_config` | `idx_fk_level_system_config_level_system_config_role_tr_3717b2d0` | `CREATE INDEX idx_fk_level_system_config_level_system_config_role_tr_3717b2d0 ON public.level_system_config USING btree (role_track_code)` |
| `level_system_config` | `idx_fk_level_system_config_level_system_config_updated_febc42c2` | `CREATE INDEX idx_fk_level_system_config_level_system_config_updated_febc42c2 ON public.level_system_config USING btree (updated_by)` |
| `level_system_config` | `level_system_config_pkey` | `CREATE UNIQUE INDEX level_system_config_pkey ON public.level_system_config USING btree (singleton)` |
| `license_policies` | `idx_fk_license_policies_license_policies_updated_by_fk_81cdd73e` | `CREATE INDEX idx_fk_license_policies_license_policies_updated_by_fk_81cdd73e ON public.license_policies USING btree (updated_by)` |
| `license_policies` | `license_policies_pkey` | `CREATE UNIQUE INDEX license_policies_pkey ON public.license_policies USING btree (spdx_id)` |
| `log_share_entries` | `log_share_entries_log_share_id_entry_index_key` | `CREATE UNIQUE INDEX log_share_entries_log_share_id_entry_index_key ON public.log_share_entries USING btree (log_share_id, entry_index)` |
| `log_share_entries` | `log_share_entries_pkey` | `CREATE UNIQUE INDEX log_share_entries_pkey ON public.log_share_entries USING btree (id)` |
| `log_shares` | `idx_log_shares_active_source_file` | `CREATE UNIQUE INDEX idx_log_shares_active_source_file ON public.log_shares USING btree (source_file_id, redaction_version) WHERE ((source_file_id IS NOT NULL) AND (status = ANY (ARRAY['processing'::text, 'ready'::text])) AND (deleted_at IS NULL))` |
| `log_shares` | `idx_log_shares_expiry` | `CREATE INDEX idx_log_shares_expiry ON public.log_shares USING btree (expires_at, id) WHERE (status = ANY (ARRAY['processing'::text, 'ready'::text]))` |
| `log_shares` | `idx_log_shares_owner_created` | `CREATE INDEX idx_log_shares_owner_created ON public.log_shares USING btree (owner_user_id, created_at DESC, id DESC) WHERE (owner_user_id IS NOT NULL)` |
| `log_shares` | `idx_log_shares_owner_fk` | `CREATE INDEX idx_log_shares_owner_fk ON public.log_shares USING btree (owner_user_id)` |
| `log_shares` | `idx_log_shares_source_file_fk` | `CREATE INDEX idx_log_shares_source_file_fk ON public.log_shares USING btree (source_file_id)` |
| `log_shares` | `log_shares_pkey` | `CREATE UNIQUE INDEX log_shares_pkey ON public.log_shares USING btree (id)` |
| `log_shares` | `log_shares_public_code_key` | `CREATE UNIQUE INDEX log_shares_public_code_key ON public.log_shares USING btree (public_code)` |
| `markdown_playground_drafts` | `markdown_playground_drafts_pkey` | `CREATE UNIQUE INDEX markdown_playground_drafts_pkey ON public.markdown_playground_drafts USING btree (user_id)` |
| `minecraft_server_links` | `idx_minecraft_server_links_order` | `CREATE INDEX idx_minecraft_server_links_order ON public.minecraft_server_links USING btree (server_id, display_order, id)` |
| `minecraft_server_links` | `minecraft_server_links_pkey` | `CREATE UNIQUE INDEX minecraft_server_links_pkey ON public.minecraft_server_links USING btree (id)` |
| `minecraft_server_mods` | `idx_fk_minecraft_server_mods_minecraft_server_mods_mod_feacc78c` | `CREATE INDEX idx_fk_minecraft_server_mods_minecraft_server_mods_mod_feacc78c ON public.minecraft_server_mods USING btree (mod_id)` |
| `minecraft_server_mods` | `idx_minecraft_server_mods_mod` | `CREATE INDEX idx_minecraft_server_mods_mod ON public.minecraft_server_mods USING btree (mod_id, server_id) WHERE (mod_id IS NOT NULL)` |
| `minecraft_server_mods` | `idx_minecraft_server_mods_raw` | `CREATE INDEX idx_minecraft_server_mods_raw ON public.minecraft_server_mods USING btree (lower(raw_mod_id), server_id)` |
| `minecraft_server_mods` | `minecraft_server_mods_pkey` | `CREATE UNIQUE INDEX minecraft_server_mods_pkey ON public.minecraft_server_mods USING btree (id)` |
| `minecraft_server_mods` | `minecraft_server_mods_server_id_raw_mod_id_key` | `CREATE UNIQUE INDEX minecraft_server_mods_server_id_raw_mod_id_key ON public.minecraft_server_mods USING btree (server_id, raw_mod_id)` |
| `minecraft_server_proof_files` | `idx_fk_minecraft_server_proof_files_minecraft_server_p_cad33bbd` | `CREATE INDEX idx_fk_minecraft_server_proof_files_minecraft_server_p_cad33bbd ON public.minecraft_server_proof_files USING btree (oss_file_id)` |
| `minecraft_server_proof_files` | `minecraft_server_proof_files_pkey` | `CREATE UNIQUE INDEX minecraft_server_proof_files_pkey ON public.minecraft_server_proof_files USING btree (server_id, oss_file_id)` |
| `minecraft_server_status_samples` | `idx_minecraft_server_samples_history` | `CREATE INDEX idx_minecraft_server_samples_history ON public.minecraft_server_status_samples USING btree (server_id, checked_at DESC, id DESC)` |
| `minecraft_server_status_samples` | `idx_minecraft_server_samples_retention` | `CREATE INDEX idx_minecraft_server_samples_retention ON public.minecraft_server_status_samples USING btree (checked_at, id)` |
| `minecraft_server_status_samples` | `minecraft_server_status_samples_pkey` | `CREATE UNIQUE INDEX minecraft_server_status_samples_pkey ON public.minecraft_server_status_samples USING btree (id)` |
| `minecraft_servers` | `idx_fk_minecraft_servers_minecraft_servers_reviewed_by_81e16754` | `CREATE INDEX idx_fk_minecraft_servers_minecraft_servers_reviewed_by_81e16754 ON public.minecraft_servers USING btree (reviewed_by)` |
| `minecraft_servers` | `idx_minecraft_servers_active_address` | `CREATE UNIQUE INDEX idx_minecraft_servers_active_address ON public.minecraft_servers USING btree (normalized_address) WHERE (review_status = ANY (ARRAY['pending'::text, 'approved'::text]))` |
| `minecraft_servers` | `idx_minecraft_servers_catalog` | `CREATE INDEX idx_minecraft_servers_catalog ON public.minecraft_servers USING btree (review_status, primary_tag, updated_at DESC, id DESC)` |
| `minecraft_servers` | `idx_minecraft_servers_languages` | `CREATE INDEX idx_minecraft_servers_languages ON public.minecraft_servers USING gin (languages)` |
| `minecraft_servers` | `idx_minecraft_servers_probe` | `CREATE INDEX idx_minecraft_servers_probe ON public.minecraft_servers USING btree (next_probe_at, id) WHERE (review_status = 'approved'::text)` |
| `minecraft_servers` | `idx_minecraft_servers_search` | `CREATE INDEX idx_minecraft_servers_search ON public.minecraft_servers USING gin (to_tsvector('simple'::regconfig, ((((name \|\| ' '::text) \|\| short_description) \|\| ' '::text) \|\| body_markdown)))` |
| `minecraft_servers` | `idx_minecraft_servers_submitted_by` | `CREATE INDEX idx_minecraft_servers_submitted_by ON public.minecraft_servers USING btree (submitted_by, updated_at DESC, id DESC)` |
| `minecraft_servers` | `idx_minecraft_servers_versions` | `CREATE INDEX idx_minecraft_servers_versions ON public.minecraft_servers USING gin (minecraft_versions)` |
| `minecraft_servers` | `minecraft_servers_pkey` | `CREATE UNIQUE INDEX minecraft_servers_pkey ON public.minecraft_servers USING btree (id)` |
| `minecraft_servers` | `minecraft_servers_public_id_key` | `CREATE UNIQUE INDEX minecraft_servers_public_id_key ON public.minecraft_servers USING btree (public_id)` |
| `minecraft_servers` | `minecraft_servers_slug_key` | `CREATE UNIQUE INDEX minecraft_servers_slug_key ON public.minecraft_servers USING btree (slug)` |
| `mirrored_project_files` | `idx_fk_mirrored_project_files_mirrored_project_files_o_efbab52a` | `CREATE INDEX idx_fk_mirrored_project_files_mirrored_project_files_o_efbab52a ON public.mirrored_project_files USING btree (oss_file_id)` |
| `mirrored_project_files` | `idx_fk_mirrored_project_files_mirrored_project_files_p_963a8754` | `CREATE INDEX idx_fk_mirrored_project_files_mirrored_project_files_p_963a8754 ON public.mirrored_project_files USING btree (project_route_id)` |
| `mirrored_project_files` | `mirrored_project_files_pkey` | `CREATE UNIQUE INDEX mirrored_project_files_pkey ON public.mirrored_project_files USING btree (id)` |
| `mirrored_project_files` | `mirrored_project_files_source_type_external_file_id_key` | `CREATE UNIQUE INDEX mirrored_project_files_source_type_external_file_id_key ON public.mirrored_project_files USING btree (source_type, external_file_id)` |
| `mirrored_project_files` | `mirrored_project_files_source_type_file_sha256_byte_size_key` | `CREATE UNIQUE INDEX mirrored_project_files_source_type_file_sha256_byte_size_key ON public.mirrored_project_files USING btree (source_type, file_sha256, byte_size)` |
| `mod_content_section_localizations` | `mod_content_section_localizations_pkey` | `CREATE UNIQUE INDEX mod_content_section_localizations_pkey ON public.mod_content_section_localizations USING btree (section_id, locale)` |
| `mod_content_section_resources` | `idx_mod_content_section_resources_resource` | `CREATE INDEX idx_mod_content_section_resources_resource ON public.mod_content_section_resources USING btree (resource_id, version_id)` |
| `mod_content_section_resources` | `idx_mod_content_section_resources_similar_group` | `CREATE INDEX idx_mod_content_section_resources_similar_group ON public.mod_content_section_resources USING btree (version_id, similar_group_id, ordinal) WHERE (similar_group_id <> ''::text)` |
| `mod_content_section_resources` | `mod_content_section_resources_pkey` | `CREATE UNIQUE INDEX mod_content_section_resources_pkey ON public.mod_content_section_resources USING btree (section_id, version_id, resource_id)` |
| `mod_content_section_resources` | `mod_content_section_resources_section_id_version_id_ordinal_key` | `CREATE UNIQUE INDEX mod_content_section_resources_section_id_version_id_ordinal_key ON public.mod_content_section_resources USING btree (section_id, version_id, ordinal)` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_placement_identity_key` | `CREATE UNIQUE INDEX mod_content_section_resources_version_id_placement_identity_key ON public.mod_content_section_resources USING btree (version_id, placement_identity_key)` |
| `mod_content_section_resources` | `mod_content_section_resources_version_id_resource_id_key` | `CREATE UNIQUE INDEX mod_content_section_resources_version_id_resource_id_key ON public.mod_content_section_resources USING btree (version_id, resource_id)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_creat_b22cbc3c` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_creat_b22cbc3c ON public.mod_content_sections USING btree (created_by)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_mod_i_adffc012` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_mod_i_adffc012 ON public.mod_content_sections USING btree (mod_id)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_paren_d0856ad3` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_paren_d0856ad3 ON public.mod_content_sections USING btree (parent_id)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_publi_edb1c37f` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_publi_edb1c37f ON public.mod_content_sections USING btree (published_revision_id)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_templ_4dde3e39` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_templ_4dde3e39 ON public.mod_content_sections USING btree (template_id)` |
| `mod_content_sections` | `idx_fk_mod_content_sections_mod_content_sections_updat_56c9e73e` | `CREATE INDEX idx_fk_mod_content_sections_mod_content_sections_updat_56c9e73e ON public.mod_content_sections USING btree (updated_by)` |
| `mod_content_sections` | `idx_mod_content_sections_system_key` | `CREATE UNIQUE INDEX idx_mod_content_sections_system_key ON public.mod_content_sections USING btree (version_id, parent_id, system_key) WHERE ((system_key <> ''::text) AND (status = 'active'::text))` |
| `mod_content_sections` | `idx_mod_content_sections_tree` | `CREATE INDEX idx_mod_content_sections_tree ON public.mod_content_sections USING btree (version_id, parent_id, ordinal)` |
| `mod_content_sections` | `mod_content_sections_id_version_id_key` | `CREATE UNIQUE INDEX mod_content_sections_id_version_id_key ON public.mod_content_sections USING btree (id, version_id)` |
| `mod_content_sections` | `mod_content_sections_pkey` | `CREATE UNIQUE INDEX mod_content_sections_pkey ON public.mod_content_sections USING btree (id)` |
| `mod_content_sections` | `mod_content_sections_public_id_key` | `CREATE UNIQUE INDEX mod_content_sections_public_id_key ON public.mod_content_sections USING btree (public_id)` |
| `mod_content_sections` | `mod_content_sections_version_id_parent_id_ordinal_key` | `CREATE UNIQUE INDEX mod_content_sections_version_id_parent_id_ordinal_key ON public.mod_content_sections USING btree (version_id, parent_id, ordinal)` |
| `mod_content_template_localizations` | `mod_content_template_localizations_pkey` | `CREATE UNIQUE INDEX mod_content_template_localizations_pkey ON public.mod_content_template_localizations USING btree (template_id, locale)` |
| `mod_content_templates` | `idx_fk_mod_content_templates_mod_content_templates_cre_0599500c` | `CREATE INDEX idx_fk_mod_content_templates_mod_content_templates_cre_0599500c ON public.mod_content_templates USING btree (created_by)` |
| `mod_content_templates` | `idx_fk_mod_content_templates_mod_content_templates_own_26843f7f` | `CREATE INDEX idx_fk_mod_content_templates_mod_content_templates_own_26843f7f ON public.mod_content_templates USING btree (owner_mod_id)` |
| `mod_content_templates` | `idx_fk_mod_content_templates_mod_content_templates_pub_4e418a61` | `CREATE INDEX idx_fk_mod_content_templates_mod_content_templates_pub_4e418a61 ON public.mod_content_templates USING btree (published_revision_id)` |
| `mod_content_templates` | `idx_mod_content_templates_builtin_code` | `CREATE UNIQUE INDEX idx_mod_content_templates_builtin_code ON public.mod_content_templates USING btree (code) WHERE builtin` |
| `mod_content_templates` | `idx_mod_content_templates_custom_code` | `CREATE UNIQUE INDEX idx_mod_content_templates_custom_code ON public.mod_content_templates USING btree (owner_mod_id, code) WHERE (NOT builtin)` |
| `mod_content_templates` | `mod_content_templates_pkey` | `CREATE UNIQUE INDEX mod_content_templates_pkey ON public.mod_content_templates USING btree (id)` |
| `mod_content_templates` | `mod_content_templates_public_id_key` | `CREATE UNIQUE INDEX mod_content_templates_public_id_key ON public.mod_content_templates USING btree (public_id)` |
| `mod_content_versions` | `idx_fk_mod_content_versions_mod_content_versions_creat_355e7938` | `CREATE INDEX idx_fk_mod_content_versions_mod_content_versions_creat_355e7938 ON public.mod_content_versions USING btree (created_by)` |
| `mod_content_versions` | `idx_fk_mod_content_versions_mod_content_versions_publi_178742ec` | `CREATE INDEX idx_fk_mod_content_versions_mod_content_versions_publi_178742ec ON public.mod_content_versions USING btree (published_revision_id)` |
| `mod_content_versions` | `idx_fk_mod_content_versions_mod_content_versions_updat_f102c55e` | `CREATE INDEX idx_fk_mod_content_versions_mod_content_versions_updat_f102c55e ON public.mod_content_versions USING btree (updated_by)` |
| `mod_content_versions` | `idx_mod_content_versions_mod_status` | `CREATE INDEX idx_mod_content_versions_mod_status ON public.mod_content_versions USING btree (mod_id, status, updated_at DESC)` |
| `mod_content_versions` | `mod_content_versions_pkey` | `CREATE UNIQUE INDEX mod_content_versions_pkey ON public.mod_content_versions USING btree (id)` |
| `mod_content_versions` | `mod_content_versions_public_id_key` | `CREATE UNIQUE INDEX mod_content_versions_public_id_key ON public.mod_content_versions USING btree (public_id)` |
| `mod_gallery_images` | `idx_fk_mod_gallery_images_fk_mod_gallery_images_revisi_ed5864f8` | `CREATE INDEX idx_fk_mod_gallery_images_fk_mod_gallery_images_revisi_ed5864f8 ON public.mod_gallery_images USING btree (published_revision_id)` |
| `mod_gallery_images` | `idx_fk_mod_gallery_images_mod_gallery_images_created_b_e6575e57` | `CREATE INDEX idx_fk_mod_gallery_images_mod_gallery_images_created_b_e6575e57 ON public.mod_gallery_images USING btree (created_by)` |
| `mod_gallery_images` | `idx_fk_mod_gallery_images_mod_gallery_images_oss_file__50d61237` | `CREATE INDEX idx_fk_mod_gallery_images_mod_gallery_images_oss_file__50d61237 ON public.mod_gallery_images USING btree (oss_file_id)` |
| `mod_gallery_images` | `idx_mod_gallery_images_mod_order` | `CREATE INDEX idx_mod_gallery_images_mod_order ON public.mod_gallery_images USING btree (mod_id, display_order, id)` |
| `mod_gallery_images` | `mod_gallery_images_mod_id_oss_file_id_key` | `CREATE UNIQUE INDEX mod_gallery_images_mod_id_oss_file_id_key ON public.mod_gallery_images USING btree (mod_id, oss_file_id)` |
| `mod_gallery_images` | `mod_gallery_images_pkey` | `CREATE UNIQUE INDEX mod_gallery_images_pkey ON public.mod_gallery_images USING btree (id)` |
| `mod_gallery_images` | `mod_gallery_images_public_id_key` | `CREATE UNIQUE INDEX mod_gallery_images_public_id_key ON public.mod_gallery_images USING btree (public_id)` |
| `mod_identifiers` | `idx_mod_identifiers_mod_order` | `CREATE INDEX idx_mod_identifiers_mod_order ON public.mod_identifiers USING btree (mod_id, display_order, id)` |
| `mod_identifiers` | `idx_mod_identifiers_namespace` | `CREATE UNIQUE INDEX idx_mod_identifiers_namespace ON public.mod_identifiers USING btree (lower(identifier))` |
| `mod_identifiers` | `idx_mod_identifiers_primary` | `CREATE UNIQUE INDEX idx_mod_identifiers_primary ON public.mod_identifiers USING btree (mod_id) WHERE is_primary` |
| `mod_identifiers` | `mod_identifiers_pkey` | `CREATE UNIQUE INDEX mod_identifiers_pkey ON public.mod_identifiers USING btree (id)` |
| `mod_links` | `idx_mod_links_mod_order` | `CREATE INDEX idx_mod_links_mod_order ON public.mod_links USING btree (mod_id, display_order, id)` |
| `mod_links` | `mod_links_mod_id_link_type_url_key` | `CREATE UNIQUE INDEX mod_links_mod_id_link_type_url_key ON public.mod_links USING btree (mod_id, link_type, url)` |
| `mod_links` | `mod_links_pkey` | `CREATE UNIQUE INDEX mod_links_pkey ON public.mod_links USING btree (id)` |
| `mod_loader_compatibilities` | `idx_mod_loader_compatibilities_lookup` | `CREATE INDEX idx_mod_loader_compatibilities_lookup ON public.mod_loader_compatibilities USING btree (loader, minecraft_version, mod_id)` |
| `mod_loader_compatibilities` | `mod_loader_compatibilities_pkey` | `CREATE UNIQUE INDEX mod_loader_compatibilities_pkey ON public.mod_loader_compatibilities USING btree (mod_id, loader, minecraft_version)` |
| `mod_metadata_import_jobs` | `idx_mod_metadata_import_jobs_project_type` | `CREATE INDEX idx_mod_metadata_import_jobs_project_type ON public.mod_metadata_import_jobs USING btree (project_type, user_id, created_at DESC)` |
| `mod_metadata_import_jobs` | `idx_mod_metadata_import_jobs_queued` | `CREATE INDEX idx_mod_metadata_import_jobs_queued ON public.mod_metadata_import_jobs USING btree (created_at) WHERE (status = 'queued'::text)` |
| `mod_metadata_import_jobs` | `idx_mod_metadata_import_jobs_user_created` | `CREATE INDEX idx_mod_metadata_import_jobs_user_created ON public.mod_metadata_import_jobs USING btree (user_id, created_at DESC)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_pkey` | `CREATE UNIQUE INDEX mod_metadata_import_jobs_pkey ON public.mod_metadata_import_jobs USING btree (id)` |
| `mod_metadata_import_jobs` | `mod_metadata_import_jobs_public_id_key` | `CREATE UNIQUE INDEX mod_metadata_import_jobs_public_id_key ON public.mod_metadata_import_jobs USING btree (public_id)` |
| `mod_relationship_groups` | `idx_mod_relationship_groups_mod_order` | `CREATE INDEX idx_mod_relationship_groups_mod_order ON public.mod_relationship_groups USING btree (mod_id, display_order, id)` |
| `mod_relationship_groups` | `mod_relationship_groups_pkey` | `CREATE UNIQUE INDEX mod_relationship_groups_pkey ON public.mod_relationship_groups USING btree (id)` |
| `mod_relationships` | `idx_fk_mod_relationships_mod_relationships_related_mod_2d84d860` | `CREATE INDEX idx_fk_mod_relationships_mod_relationships_related_mod_2d84d860 ON public.mod_relationships USING btree (related_mod_id)` |
| `mod_relationships` | `idx_mod_relationships_group_order` | `CREATE INDEX idx_mod_relationships_group_order ON public.mod_relationships USING btree (group_id, display_order, id)` |
| `mod_relationships` | `idx_mod_relationships_mod_order` | `CREATE INDEX idx_mod_relationships_mod_order ON public.mod_relationships USING btree (mod_id, display_order, id)` |
| `mod_relationships` | `idx_mod_relationships_related_mod` | `CREATE INDEX idx_mod_relationships_related_mod ON public.mod_relationships USING btree (related_mod_id, id) WHERE (related_mod_id IS NOT NULL)` |
| `mod_relationships` | `mod_relationships_pkey` | `CREATE UNIQUE INDEX mod_relationships_pkey ON public.mod_relationships USING btree (id)` |
| `mod_resource_bindings` | `idx_mod_resource_bindings_mod` | `CREATE INDEX idx_mod_resource_bindings_mod ON public.mod_resource_bindings USING btree (mod_id, resource_id)` |
| `mod_resource_bindings` | `mod_resource_bindings_pkey` | `CREATE UNIQUE INDEX mod_resource_bindings_pkey ON public.mod_resource_bindings USING btree (resource_id)` |
| `mod_resource_version_detail_localizations` | `mod_resource_version_detail_localizations_pkey` | `CREATE UNIQUE INDEX mod_resource_version_detail_localizations_pkey ON public.mod_resource_version_detail_localizations USING btree (resource_id, version_id, locale)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_29bc312e` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_29bc312e ON public.mod_resource_version_details USING btree (render_file_id)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_4351a99c` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_4351a99c ON public.mod_resource_version_details USING btree (created_by)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_76a7bab3` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_76a7bab3 ON public.mod_resource_version_details USING btree (published_revision_id)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_931002f0` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_931002f0 ON public.mod_resource_version_details USING btree (icon_file_id)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_e9e03cb6` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_e9e03cb6 ON public.mod_resource_version_details USING btree (icon_small_file_id)` |
| `mod_resource_version_details` | `idx_fk_mod_resource_version_details_mod_resource_versi_ed6d0f75` | `CREATE INDEX idx_fk_mod_resource_version_details_mod_resource_versi_ed6d0f75 ON public.mod_resource_version_details USING btree (updated_by)` |
| `mod_resource_version_details` | `idx_mod_resource_version_details_status` | `CREATE INDEX idx_mod_resource_version_details_status ON public.mod_resource_version_details USING btree (version_id, status, updated_at DESC)` |
| `mod_resource_version_details` | `mod_resource_version_details_pkey` | `CREATE UNIQUE INDEX mod_resource_version_details_pkey ON public.mod_resource_version_details USING btree (resource_id, version_id)` |
| `mod_tags` | `mod_tags_pkey` | `CREATE UNIQUE INDEX mod_tags_pkey ON public.mod_tags USING btree (mod_id, tag)` |
| `moderation_actions` | `idx_fk_moderation_actions_moderation_actions_actor_id__c3f73b7f` | `CREATE INDEX idx_fk_moderation_actions_moderation_actions_actor_id__c3f73b7f ON public.moderation_actions USING btree (actor_id)` |
| `moderation_actions` | `idx_fk_moderation_actions_moderation_actions_report_id_7d921b56` | `CREATE INDEX idx_fk_moderation_actions_moderation_actions_report_id_7d921b56 ON public.moderation_actions USING btree (report_id)` |
| `moderation_actions` | `idx_fk_moderation_actions_moderation_actions_target_us_7b484d72` | `CREATE INDEX idx_fk_moderation_actions_moderation_actions_target_us_7b484d72 ON public.moderation_actions USING btree (target_user_id)` |
| `moderation_actions` | `moderation_actions_action_type_target_type_target_public_id_key` | `CREATE UNIQUE INDEX moderation_actions_action_type_target_type_target_public_id_key ON public.moderation_actions USING btree (action_type, target_type, target_public_id, idempotency_key)` |
| `moderation_actions` | `moderation_actions_pkey` | `CREATE UNIQUE INDEX moderation_actions_pkey ON public.moderation_actions USING btree (id)` |
| `moderation_actions` | `moderation_actions_public_id_key` | `CREATE UNIQUE INDEX moderation_actions_public_id_key ON public.moderation_actions USING btree (public_id)` |
| `modpack_gallery_images` | `idx_fk_modpack_gallery_images_modpack_gallery_images_c_8e627c6e` | `CREATE INDEX idx_fk_modpack_gallery_images_modpack_gallery_images_c_8e627c6e ON public.modpack_gallery_images USING btree (created_by)` |
| `modpack_gallery_images` | `idx_fk_modpack_gallery_images_modpack_gallery_images_o_69df0ca5` | `CREATE INDEX idx_fk_modpack_gallery_images_modpack_gallery_images_o_69df0ca5 ON public.modpack_gallery_images USING btree (oss_file_id)` |
| `modpack_gallery_images` | `idx_fk_modpack_gallery_images_modpack_gallery_images_p_02d9cffa` | `CREATE INDEX idx_fk_modpack_gallery_images_modpack_gallery_images_p_02d9cffa ON public.modpack_gallery_images USING btree (published_revision_id)` |
| `modpack_gallery_images` | `idx_modpack_gallery_order` | `CREATE INDEX idx_modpack_gallery_order ON public.modpack_gallery_images USING btree (modpack_id, display_order, id)` |
| `modpack_gallery_images` | `modpack_gallery_images_modpack_id_oss_file_id_key` | `CREATE UNIQUE INDEX modpack_gallery_images_modpack_id_oss_file_id_key ON public.modpack_gallery_images USING btree (modpack_id, oss_file_id)` |
| `modpack_gallery_images` | `modpack_gallery_images_pkey` | `CREATE UNIQUE INDEX modpack_gallery_images_pkey ON public.modpack_gallery_images USING btree (id)` |
| `modpack_gallery_images` | `modpack_gallery_images_public_id_key` | `CREATE UNIQUE INDEX modpack_gallery_images_public_id_key ON public.modpack_gallery_images USING btree (public_id)` |
| `modpack_links` | `idx_modpack_links_order` | `CREATE INDEX idx_modpack_links_order ON public.modpack_links USING btree (modpack_id, display_order, id)` |
| `modpack_links` | `modpack_links_modpack_id_link_type_url_key` | `CREATE UNIQUE INDEX modpack_links_modpack_id_link_type_url_key ON public.modpack_links USING btree (modpack_id, link_type, url)` |
| `modpack_links` | `modpack_links_pkey` | `CREATE UNIQUE INDEX modpack_links_pkey ON public.modpack_links USING btree (id)` |
| `modpack_loader_compatibilities` | `idx_modpack_compatibilities_version` | `CREATE INDEX idx_modpack_compatibilities_version ON public.modpack_loader_compatibilities USING btree (minecraft_version, loader, modpack_id)` |
| `modpack_loader_compatibilities` | `modpack_loader_compatibilities_pkey` | `CREATE UNIQUE INDEX modpack_loader_compatibilities_pkey ON public.modpack_loader_compatibilities USING btree (modpack_id, loader, minecraft_version)` |
| `modpack_mods` | `idx_fk_modpack_mods_modpack_mods_mod_id_fkey_2e28d2ce` | `CREATE INDEX idx_fk_modpack_mods_modpack_mods_mod_id_fkey_2e28d2ce ON public.modpack_mods USING btree (mod_id)` |
| `modpack_mods` | `idx_modpack_mods_identity` | `CREATE UNIQUE INDEX idx_modpack_mods_identity ON public.modpack_mods USING btree (modpack_id, provider, provider_project_id, provider_version_id, file_name, identifier, COALESCE(mod_id, (0)::bigint))` |
| `modpack_mods` | `idx_modpack_mods_order` | `CREATE INDEX idx_modpack_mods_order ON public.modpack_mods USING btree (modpack_id, display_order, id)` |
| `modpack_mods` | `idx_modpack_mods_resolved` | `CREATE INDEX idx_modpack_mods_resolved ON public.modpack_mods USING btree (mod_id, modpack_id) WHERE (mod_id IS NOT NULL)` |
| `modpack_mods` | `modpack_mods_pkey` | `CREATE UNIQUE INDEX modpack_mods_pkey ON public.modpack_mods USING btree (id)` |
| `modpack_tags` | `modpack_tags_pkey` | `CREATE UNIQUE INDEX modpack_tags_pkey ON public.modpack_tags USING btree (modpack_id, tag)` |
| `modpacks` | `idx_fk_modpacks_modpacks_published_revision_id_fkey_72c94491` | `CREATE INDEX idx_fk_modpacks_modpacks_published_revision_id_fkey_72c94491 ON public.modpacks USING btree (published_revision_id)` |
| `modpacks` | `idx_modpacks_catalog` | `CREATE INDEX idx_modpacks_catalog ON public.modpacks USING btree (review_status, updated_at DESC, id DESC)` |
| `modpacks` | `idx_modpacks_primary_name_lower` | `CREATE INDEX idx_modpacks_primary_name_lower ON public.modpacks USING btree (lower(primary_name))` |
| `modpacks` | `idx_modpacks_submitted_by` | `CREATE INDEX idx_modpacks_submitted_by ON public.modpacks USING btree (submitted_by, updated_at DESC)` |
| `modpacks` | `modpacks_pkey` | `CREATE UNIQUE INDEX modpacks_pkey ON public.modpacks USING btree (id)` |
| `modpacks` | `modpacks_public_id_key` | `CREATE UNIQUE INDEX modpacks_public_id_key ON public.modpacks USING btree (public_id)` |
| `modpacks` | `modpacks_slug_key` | `CREATE UNIQUE INDEX modpacks_slug_key ON public.modpacks USING btree (slug)` |
| `mods` | `idx_fk_mods_fk_mods_published_revision_f7bc3416` | `CREATE INDEX idx_fk_mods_fk_mods_published_revision_f7bc3416 ON public.mods USING btree (published_revision_id)` |
| `mods` | `idx_mods_primary_name_lower` | `CREATE INDEX idx_mods_primary_name_lower ON public.mods USING btree (lower(primary_name))` |
| `mods` | `idx_mods_project_code` | `CREATE UNIQUE INDEX idx_mods_project_code ON public.mods USING btree (project_code)` |
| `mods` | `idx_mods_review_updated_at` | `CREATE INDEX idx_mods_review_updated_at ON public.mods USING btree (review_status, updated_at DESC)` |
| `mods` | `idx_mods_submitted_by_updated_at` | `CREATE INDEX idx_mods_submitted_by_updated_at ON public.mods USING btree (submitted_by, updated_at DESC)` |
| `mods` | `mods_pkey` | `CREATE UNIQUE INDEX mods_pkey ON public.mods USING btree (id)` |
| `mods` | `mods_project_code_key` | `CREATE UNIQUE INDEX mods_project_code_key ON public.mods USING btree (project_code)` |
| `mods` | `mods_slug_key` | `CREATE UNIQUE INDEX mods_slug_key ON public.mods USING btree (slug)` |
| `nats_outbox` | `idx_nats_outbox_pending` | `CREATE INDEX idx_nats_outbox_pending ON public.nats_outbox USING btree (available_at, id) WHERE ((status = ANY (ARRAY['pending'::text, 'failed'::text])) AND (published_at IS NULL))` |
| `nats_outbox` | `idx_nats_outbox_status_created` | `CREATE INDEX idx_nats_outbox_status_created ON public.nats_outbox USING btree (status, created_at)` |
| `nats_outbox` | `nats_outbox_event_id_key` | `CREATE UNIQUE INDEX nats_outbox_event_id_key ON public.nats_outbox USING btree (event_id)` |
| `nats_outbox` | `nats_outbox_pkey` | `CREATE UNIQUE INDEX nats_outbox_pkey ON public.nats_outbox USING btree (id)` |
| `notification_actors` | `idx_fk_notification_actors_notification_actors_actor_i_b376dd8d` | `CREATE INDEX idx_fk_notification_actors_notification_actors_actor_i_b376dd8d ON public.notification_actors USING btree (actor_id)` |
| `notification_actors` | `notification_actors_pkey` | `CREATE UNIQUE INDEX notification_actors_pkey ON public.notification_actors USING btree (notification_id, actor_id)` |
| `notification_receipts` | `idx_notification_receipts_user_read` | `CREATE INDEX idx_notification_receipts_user_read ON public.notification_receipts USING btree (user_id, read_at)` |
| `notification_receipts` | `notification_receipts_pkey` | `CREATE UNIQUE INDEX notification_receipts_pkey ON public.notification_receipts USING btree (notification_id, user_id)` |
| `notification_translations` | `idx_fk_notification_translations_notification_translat_dc8cfb38` | `CREATE INDEX idx_fk_notification_translations_notification_translat_dc8cfb38 ON public.notification_translations USING btree (user_id)` |
| `notification_translations` | `notification_translations_pkey` | `CREATE UNIQUE INDEX notification_translations_pkey ON public.notification_translations USING btree (notification_id, user_id, locale)` |
| `notifications` | `idx_fk_notifications_notifications_project_update_even_edddc38a` | `CREATE INDEX idx_fk_notifications_notifications_project_update_even_edddc38a ON public.notifications USING btree (project_update_event_id)` |
| `notifications` | `idx_notifications_broadcast_updated_at` | `CREATE INDEX idx_notifications_broadcast_updated_at ON public.notifications USING btree (updated_at DESC) WHERE (recipient_id IS NULL)` |
| `notifications` | `idx_notifications_recipient_updated_at` | `CREATE INDEX idx_notifications_recipient_updated_at ON public.notifications USING btree (recipient_id, updated_at DESC)` |
| `notifications` | `notifications_pkey` | `CREATE UNIQUE INDEX notifications_pkey ON public.notifications USING btree (id)` |
| `notifications` | `notifications_public_id_key` | `CREATE UNIQUE INDEX notifications_public_id_key ON public.notifications USING btree (public_id)` |
| `notifications` | `uq_notifications_project_update_recipient` | `CREATE UNIQUE INDEX uq_notifications_project_update_recipient ON public.notifications USING btree (recipient_id, project_update_event_id) WHERE (project_update_event_id IS NOT NULL)` |
| `notifications` | `uq_notifications_source_event` | `CREATE UNIQUE INDEX uq_notifications_source_event ON public.notifications USING btree (source_event_id) WHERE (source_event_id IS NOT NULL)` |
| `oauth_accounts` | `idx_oauth_accounts_user_id` | `CREATE INDEX idx_oauth_accounts_user_id ON public.oauth_accounts USING btree (user_id)` |
| `oauth_accounts` | `oauth_accounts_pkey` | `CREATE UNIQUE INDEX oauth_accounts_pkey ON public.oauth_accounts USING btree (id)` |
| `oauth_accounts` | `oauth_accounts_provider_provider_user_id_key` | `CREATE UNIQUE INDEX oauth_accounts_provider_provider_user_id_key ON public.oauth_accounts USING btree (provider, provider_user_id)` |
| `oss_download_stats` | `idx_fk_oss_download_stats_oss_download_stats_file_id_f_0bc0fc3b` | `CREATE INDEX idx_fk_oss_download_stats_oss_download_stats_file_id_f_0bc0fc3b ON public.oss_download_stats USING btree (file_id)` |
| `oss_download_stats` | `oss_download_stats_pkey` | `CREATE UNIQUE INDEX oss_download_stats_pkey ON public.oss_download_stats USING btree (object_key)` |
| `oss_files` | `idx_oss_files_category` | `CREATE INDEX idx_oss_files_category ON public.oss_files USING btree (category, created_at DESC)` |
| `oss_files` | `idx_oss_files_object_key_prefix` | `CREATE INDEX idx_oss_files_object_key_prefix ON public.oss_files USING btree (object_key text_pattern_ops)` |
| `oss_files` | `idx_oss_files_scan_status` | `CREATE INDEX idx_oss_files_scan_status ON public.oss_files USING btree (scan_status, created_at DESC) WHERE (status = 'active'::text)` |
| `oss_files` | `idx_oss_files_sha256_size` | `CREATE INDEX idx_oss_files_sha256_size ON public.oss_files USING btree (sha256, size_bytes) WHERE (sha256 <> ''::text)` |
| `oss_files` | `idx_oss_files_sha256_source_size` | `CREATE INDEX idx_oss_files_sha256_source_size ON public.oss_files USING btree (sha256, source_size_bytes) WHERE (sha256 <> ''::text)` |
| `oss_files` | `idx_oss_files_uploader_created_at` | `CREATE INDEX idx_oss_files_uploader_created_at ON public.oss_files USING btree (uploader_id, created_at DESC)` |
| `oss_files` | `oss_files_object_key_key` | `CREATE UNIQUE INDEX oss_files_object_key_key ON public.oss_files USING btree (object_key)` |
| `oss_files` | `oss_files_pkey` | `CREATE UNIQUE INDEX oss_files_pkey ON public.oss_files USING btree (id)` |
| `oss_files` | `oss_files_public_id_key` | `CREATE UNIQUE INDEX oss_files_public_id_key ON public.oss_files USING btree (public_id)` |
| `oss_object_deletion_outbox` | `idx_fk_oss_object_deletion_outbox_oss_object_deletion__b5081108` | `CREATE INDEX idx_fk_oss_object_deletion_outbox_oss_object_deletion__b5081108 ON public.oss_object_deletion_outbox USING btree (oss_file_id)` |
| `oss_object_deletion_outbox` | `idx_oss_object_deletion_outbox_pending` | `CREATE INDEX idx_oss_object_deletion_outbox_pending ON public.oss_object_deletion_outbox USING btree (next_attempt_at, id) WHERE (status = ANY (ARRAY['pending'::text, 'processing'::text]))` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_bucket_endpoint_object_key_key` | `CREATE UNIQUE INDEX oss_object_deletion_outbox_bucket_endpoint_object_key_key ON public.oss_object_deletion_outbox USING btree (bucket, endpoint, object_key)` |
| `oss_object_deletion_outbox` | `oss_object_deletion_outbox_pkey` | `CREATE UNIQUE INDEX oss_object_deletion_outbox_pkey ON public.oss_object_deletion_outbox USING btree (id)` |
| `oss_scan_logs` | `idx_fk_oss_scan_logs_oss_scan_logs_file_id_fkey_511c7f93` | `CREATE INDEX idx_fk_oss_scan_logs_oss_scan_logs_file_id_fkey_511c7f93 ON public.oss_scan_logs USING btree (file_id)` |
| `oss_scan_logs` | `idx_oss_scan_logs_created_at` | `CREATE INDEX idx_oss_scan_logs_created_at ON public.oss_scan_logs USING btree (created_at DESC)` |
| `oss_scan_logs` | `oss_scan_logs_pkey` | `CREATE UNIQUE INDEX oss_scan_logs_pkey ON public.oss_scan_logs USING btree (id)` |
| `oss_upload_logs` | `idx_fk_oss_upload_logs_oss_upload_logs_file_id_fkey_a6adc712` | `CREATE INDEX idx_fk_oss_upload_logs_oss_upload_logs_file_id_fkey_a6adc712 ON public.oss_upload_logs USING btree (file_id)` |
| `oss_upload_logs` | `idx_fk_oss_upload_logs_oss_upload_logs_uploader_id_fke_9a8fec04` | `CREATE INDEX idx_fk_oss_upload_logs_oss_upload_logs_uploader_id_fke_9a8fec04 ON public.oss_upload_logs USING btree (uploader_id)` |
| `oss_upload_logs` | `idx_oss_upload_logs_created_at` | `CREATE INDEX idx_oss_upload_logs_created_at ON public.oss_upload_logs USING btree (created_at DESC)` |
| `oss_upload_logs` | `oss_upload_logs_pkey` | `CREATE UNIQUE INDEX oss_upload_logs_pkey ON public.oss_upload_logs USING btree (id)` |
| `permission_audit_logs` | `idx_fk_permission_audit_logs_permission_audit_logs_ope_a3d3119c` | `CREATE INDEX idx_fk_permission_audit_logs_permission_audit_logs_ope_a3d3119c ON public.permission_audit_logs USING btree (operator_id)` |
| `permission_audit_logs` | `idx_fk_permission_audit_logs_permission_audit_logs_tar_76e78840` | `CREATE INDEX idx_fk_permission_audit_logs_permission_audit_logs_tar_76e78840 ON public.permission_audit_logs USING btree (target_user_id)` |
| `permission_audit_logs` | `permission_audit_logs_pkey` | `CREATE UNIQUE INDEX permission_audit_logs_pkey ON public.permission_audit_logs USING btree (id)` |
| `permission_role_track_roles` | `idx_fk_permission_role_track_roles_permission_role_tra_47277218` | `CREATE INDEX idx_fk_permission_role_track_roles_permission_role_tra_47277218 ON public.permission_role_track_roles USING btree (role_id)` |
| `permission_role_track_roles` | `permission_role_track_roles_pkey` | `CREATE UNIQUE INDEX permission_role_track_roles_pkey ON public.permission_role_track_roles USING btree (track_code, "position")` |
| `permission_role_track_roles` | `permission_role_track_roles_track_code_role_id_key` | `CREATE UNIQUE INDEX permission_role_track_roles_track_code_role_id_key ON public.permission_role_track_roles USING btree (track_code, role_id)` |
| `permission_role_tracks` | `permission_role_tracks_pkey` | `CREATE UNIQUE INDEX permission_role_tracks_pkey ON public.permission_role_tracks USING btree (code)` |
| `permissions` | `permissions_code_key` | `CREATE UNIQUE INDEX permissions_code_key ON public.permissions USING btree (code)` |
| `permissions` | `permissions_pkey` | `CREATE UNIQUE INDEX permissions_pkey ON public.permissions USING btree (id)` |
| `player_profile_name_history` | `idx_player_profile_name_history_profile` | `CREATE INDEX idx_player_profile_name_history_profile ON public.player_profile_name_history USING btree (profile_id, changed_at DESC, id DESC)` |
| `player_profile_name_history` | `player_profile_name_history_pkey` | `CREATE UNIQUE INDEX player_profile_name_history_pkey ON public.player_profile_name_history USING btree (id)` |
| `player_profile_textures` | `idx_player_profile_textures_asset` | `CREATE INDEX idx_player_profile_textures_asset ON public.player_profile_textures USING btree (asset_id, profile_id)` |
| `player_profile_textures` | `player_profile_textures_pkey` | `CREATE UNIQUE INDEX player_profile_textures_pkey ON public.player_profile_textures USING btree (profile_id, kind)` |
| `player_profiles` | `idx_player_profiles_default` | `CREATE UNIQUE INDEX idx_player_profiles_default ON public.player_profiles USING btree (user_id) WHERE (is_default AND (status = 'active'::text))` |
| `player_profiles` | `idx_player_profiles_name_lower` | `CREATE UNIQUE INDEX idx_player_profiles_name_lower ON public.player_profiles USING btree (lower(name)) WHERE (status = 'active'::text)` |
| `player_profiles` | `idx_player_profiles_user` | `CREATE INDEX idx_player_profiles_user ON public.player_profiles USING btree (user_id, status, created_at, id)` |
| `player_profiles` | `player_profiles_pkey` | `CREATE UNIQUE INDEX player_profiles_pkey ON public.player_profiles USING btree (id)` |
| `player_profiles` | `player_profiles_public_id_key` | `CREATE UNIQUE INDEX player_profiles_public_id_key ON public.player_profiles USING btree (public_id)` |
| `player_profiles` | `player_profiles_uuid_key` | `CREATE UNIQUE INDEX player_profiles_uuid_key ON public.player_profiles USING btree (uuid)` |
| `processed_events` | `idx_processed_events_created` | `CREATE INDEX idx_processed_events_created ON public.processed_events USING btree (processed_at)` |
| `processed_events` | `processed_events_pkey` | `CREATE UNIQUE INDEX processed_events_pkey ON public.processed_events USING btree (consumer, event_id)` |
| `project_auto_update_runs` | `idx_fk_project_auto_update_runs_project_auto_update_ru_8cf47d03` | `CREATE INDEX idx_fk_project_auto_update_runs_project_auto_update_ru_8cf47d03 ON public.project_auto_update_runs USING btree (actor_id)` |
| `project_auto_update_runs` | `idx_fk_project_auto_update_runs_project_auto_update_ru_9882b61b` | `CREATE INDEX idx_fk_project_auto_update_runs_project_auto_update_ru_9882b61b ON public.project_auto_update_runs USING btree (setting_id)` |
| `project_auto_update_runs` | `idx_project_auto_update_runs_ready` | `CREATE INDEX idx_project_auto_update_runs_ready ON public.project_auto_update_runs USING btree (status, next_attempt_at, id)` |
| `project_auto_update_runs` | `project_auto_update_runs_pkey` | `CREATE UNIQUE INDEX project_auto_update_runs_pkey ON public.project_auto_update_runs USING btree (id)` |
| `project_auto_update_runs` | `project_auto_update_runs_public_id_key` | `CREATE UNIQUE INDEX project_auto_update_runs_public_id_key ON public.project_auto_update_runs USING btree (public_id)` |
| `project_auto_update_runs` | `uq_project_auto_update_active_run` | `CREATE UNIQUE INDEX uq_project_auto_update_active_run ON public.project_auto_update_runs USING btree (setting_id) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]))` |
| `project_auto_update_settings` | `idx_fk_project_auto_update_settings_project_auto_updat_07023c1f` | `CREATE INDEX idx_fk_project_auto_update_settings_project_auto_updat_07023c1f ON public.project_auto_update_settings USING btree (configured_by)` |
| `project_auto_update_settings` | `idx_project_auto_update_due` | `CREATE INDEX idx_project_auto_update_due ON public.project_auto_update_settings USING btree (next_run_at, id) WHERE (enabled AND (next_run_at IS NOT NULL))` |
| `project_auto_update_settings` | `project_auto_update_settings_pkey` | `CREATE UNIQUE INDEX project_auto_update_settings_pkey ON public.project_auto_update_settings USING btree (id)` |
| `project_auto_update_settings` | `project_auto_update_settings_project_route_id_update_kind_key` | `CREATE UNIQUE INDEX project_auto_update_settings_project_route_id_update_kind_key ON public.project_auto_update_settings USING btree (project_route_id, update_kind)` |
| `project_automation_activity` | `idx_fk_project_automation_activity_project_automation__35969650` | `CREATE INDEX idx_fk_project_automation_activity_project_automation__35969650 ON public.project_automation_activity USING btree (changed_by)` |
| `project_automation_activity` | `idx_project_automation_activity_stale` | `CREATE INDEX idx_project_automation_activity_stale ON public.project_automation_activity USING btree (last_project_change_at, project_route_id)` |
| `project_automation_activity` | `project_automation_activity_pkey` | `CREATE UNIQUE INDEX project_automation_activity_pkey ON public.project_automation_activity USING btree (project_route_id)` |
| `project_changelog_categories` | `idx_fk_project_changelog_categories_project_changelog__f139362d` | `CREATE INDEX idx_fk_project_changelog_categories_project_changelog__f139362d ON public.project_changelog_categories USING btree (created_by)` |
| `project_changelog_categories` | `idx_project_changelog_categories_target` | `CREATE INDEX idx_project_changelog_categories_target ON public.project_changelog_categories USING btree (object_route_id, created_at, id)` |
| `project_changelog_categories` | `project_changelog_categories_pkey` | `CREATE UNIQUE INDEX project_changelog_categories_pkey ON public.project_changelog_categories USING btree (id)` |
| `project_changelog_categories` | `project_changelog_categories_public_id_key` | `CREATE UNIQUE INDEX project_changelog_categories_public_id_key ON public.project_changelog_categories USING btree (public_id)` |
| `project_changelog_category_localizations` | `project_changelog_category_localizations_pkey` | `CREATE UNIQUE INDEX project_changelog_category_localizations_pkey ON public.project_changelog_category_localizations USING btree (category_id, locale)` |
| `project_changelog_localizations` | `idx_fk_project_changelog_localizations_project_changel_5eba4bdc` | `CREATE INDEX idx_fk_project_changelog_localizations_project_changel_5eba4bdc ON public.project_changelog_localizations USING btree (updated_by)` |
| `project_changelog_localizations` | `idx_fk_project_changelog_localizations_project_changel_c38490e2` | `CREATE INDEX idx_fk_project_changelog_localizations_project_changel_c38490e2 ON public.project_changelog_localizations USING btree (source_revision_id)` |
| `project_changelog_localizations` | `project_changelog_localizations_pkey` | `CREATE UNIQUE INDEX project_changelog_localizations_pkey ON public.project_changelog_localizations USING btree (changelog_id, locale)` |
| `project_changelogs` | `idx_fk_project_changelogs_project_changelogs_category__78705d76` | `CREATE INDEX idx_fk_project_changelogs_project_changelogs_category__78705d76 ON public.project_changelogs USING btree (category_id)` |
| `project_changelogs` | `idx_fk_project_changelogs_project_changelogs_object_ro_138ac9fe` | `CREATE INDEX idx_fk_project_changelogs_project_changelogs_object_ro_138ac9fe ON public.project_changelogs USING btree (object_route_id)` |
| `project_changelogs` | `idx_fk_project_changelogs_project_changelogs_published_cf2c8288` | `CREATE INDEX idx_fk_project_changelogs_project_changelogs_published_cf2c8288 ON public.project_changelogs USING btree (published_revision_id)` |
| `project_changelogs` | `idx_project_changelogs_author` | `CREATE INDEX idx_project_changelogs_author ON public.project_changelogs USING btree (created_by, created_at DESC, id DESC)` |
| `project_changelogs` | `idx_project_changelogs_category` | `CREATE INDEX idx_project_changelogs_category ON public.project_changelogs USING btree (category_id, event_at DESC, id DESC) WHERE (status = 'active'::text)` |
| `project_changelogs` | `idx_project_changelogs_target` | `CREATE INDEX idx_project_changelogs_target ON public.project_changelogs USING btree (object_route_id, review_status, event_at DESC, id DESC) WHERE (status = 'active'::text)` |
| `project_changelogs` | `project_changelogs_pkey` | `CREATE UNIQUE INDEX project_changelogs_pkey ON public.project_changelogs USING btree (id)` |
| `project_changelogs` | `project_changelogs_public_id_key` | `CREATE UNIQUE INDEX project_changelogs_public_id_key ON public.project_changelogs USING btree (public_id)` |
| `project_editor_application_attachments` | `idx_fk_project_editor_application_attachments_project__4d053125` | `CREATE INDEX idx_fk_project_editor_application_attachments_project__4d053125 ON public.project_editor_application_attachments USING btree (oss_file_id)` |
| `project_editor_application_attachments` | `project_editor_application_attachments_pkey` | `CREATE UNIQUE INDEX project_editor_application_attachments_pkey ON public.project_editor_application_attachments USING btree (application_id, oss_file_id)` |
| `project_editor_applications` | `idx_fk_project_editor_applications_project_editor_appl_3824f0d6` | `CREATE INDEX idx_fk_project_editor_applications_project_editor_appl_3824f0d6 ON public.project_editor_applications USING btree (reviewed_by)` |
| `project_editor_applications` | `idx_fk_project_editor_applications_project_editor_appl_8a1d0e3e` | `CREATE INDEX idx_fk_project_editor_applications_project_editor_appl_8a1d0e3e ON public.project_editor_applications USING btree (user_id)` |
| `project_editor_applications` | `idx_fk_project_editor_applications_project_editor_appl_fcc4e436` | `CREATE INDEX idx_fk_project_editor_applications_project_editor_appl_fcc4e436 ON public.project_editor_applications USING btree (target_route_id)` |
| `project_editor_applications` | `idx_project_editor_applications_pending` | `CREATE UNIQUE INDEX idx_project_editor_applications_pending ON public.project_editor_applications USING btree (target_route_id, user_id) WHERE (status = 'pending'::text)` |
| `project_editor_applications` | `idx_project_editor_applications_review` | `CREATE INDEX idx_project_editor_applications_review ON public.project_editor_applications USING btree (status, created_at, id)` |
| `project_editor_applications` | `project_editor_applications_pkey` | `CREATE UNIQUE INDEX project_editor_applications_pkey ON public.project_editor_applications USING btree (id)` |
| `project_editor_applications` | `project_editor_applications_public_id_key` | `CREATE UNIQUE INDEX project_editor_applications_public_id_key ON public.project_editor_applications USING btree (public_id)` |
| `project_editor_assignments` | `idx_fk_project_editor_assignments_project_editor_assig_496c812b` | `CREATE INDEX idx_fk_project_editor_assignments_project_editor_assig_496c812b ON public.project_editor_assignments USING btree (user_id)` |
| `project_editor_assignments` | `idx_fk_project_editor_assignments_project_editor_assig_7bb9b34b` | `CREATE INDEX idx_fk_project_editor_assignments_project_editor_assig_7bb9b34b ON public.project_editor_assignments USING btree (application_id)` |
| `project_editor_assignments` | `idx_fk_project_editor_assignments_project_editor_assig_aab11ebc` | `CREATE INDEX idx_fk_project_editor_assignments_project_editor_assig_aab11ebc ON public.project_editor_assignments USING btree (granted_by)` |
| `project_editor_assignments` | `idx_fk_project_editor_assignments_project_editor_assig_ced72fce` | `CREATE INDEX idx_fk_project_editor_assignments_project_editor_assig_ced72fce ON public.project_editor_assignments USING btree (revoked_by)` |
| `project_editor_assignments` | `idx_project_editor_assignments_user_active` | `CREATE INDEX idx_project_editor_assignments_user_active ON public.project_editor_assignments USING btree (user_id, target_route_id) WHERE (status = 'active'::text)` |
| `project_editor_assignments` | `project_editor_assignments_pkey` | `CREATE UNIQUE INDEX project_editor_assignments_pkey ON public.project_editor_assignments USING btree (target_route_id, user_id)` |
| `project_external_sources` | `idx_fk_project_external_sources_project_external_sourc_1a255e74` | `CREATE INDEX idx_fk_project_external_sources_project_external_sourc_1a255e74 ON public.project_external_sources USING btree (verified_by)` |
| `project_external_sources` | `project_external_sources_pkey` | `CREATE UNIQUE INDEX project_external_sources_pkey ON public.project_external_sources USING btree (id)` |
| `project_external_sources` | `project_external_sources_project_route_id_source_type_key` | `CREATE UNIQUE INDEX project_external_sources_project_route_id_source_type_key ON public.project_external_sources USING btree (project_route_id, source_type)` |
| `project_external_sources` | `project_external_sources_source_type_external_project_id_key` | `CREATE UNIQUE INDEX project_external_sources_source_type_external_project_id_key ON public.project_external_sources USING btree (source_type, external_project_id)` |
| `project_files` | `idx_fk_project_files_project_files_oss_file_id_fkey_d1f25622` | `CREATE INDEX idx_fk_project_files_project_files_oss_file_id_fkey_d1f25622 ON public.project_files USING btree (oss_file_id)` |
| `project_files` | `idx_fk_project_files_project_files_uploaded_by_fkey_b91a23a2` | `CREATE INDEX idx_fk_project_files_project_files_uploaded_by_fkey_b91a23a2 ON public.project_files USING btree (uploaded_by)` |
| `project_files` | `idx_project_files_filters` | `CREATE INDEX idx_project_files_filters ON public.project_files USING btree (project_type, project_internal_id, release_channel) WHERE (status = 'active'::text)` |
| `project_files` | `idx_project_files_project_published` | `CREATE INDEX idx_project_files_project_published ON public.project_files USING btree (project_type, project_internal_id, status, created_at DESC, id DESC)` |
| `project_files` | `project_files_pkey` | `CREATE UNIQUE INDEX project_files_pkey ON public.project_files USING btree (id)` |
| `project_files` | `project_files_project_type_project_internal_id_oss_file_id_key` | `CREATE UNIQUE INDEX project_files_project_type_project_internal_id_oss_file_id_key ON public.project_files USING btree (project_type, project_internal_id, oss_file_id)` |
| `project_files` | `project_files_public_id_key` | `CREATE UNIQUE INDEX project_files_public_id_key ON public.project_files USING btree (public_id)` |
| `project_follows` | `idx_project_follows_route_user` | `CREATE INDEX idx_project_follows_route_user ON public.project_follows USING btree (project_route_id, user_id)` |
| `project_follows` | `idx_project_follows_user_created` | `CREATE INDEX idx_project_follows_user_created ON public.project_follows USING btree (user_id, created_at DESC, project_route_id)` |
| `project_follows` | `project_follows_pkey` | `CREATE UNIQUE INDEX project_follows_pkey ON public.project_follows USING btree (user_id, project_route_id)` |
| `project_update_events` | `idx_fk_project_update_events_project_update_events_act_62365dca` | `CREATE INDEX idx_fk_project_update_events_project_update_events_act_62365dca ON public.project_update_events USING btree (actor_user_id)` |
| `project_update_events` | `idx_project_update_events_route_published` | `CREATE INDEX idx_project_update_events_route_published ON public.project_update_events USING btree (project_route_id, published_at DESC, id DESC)` |
| `project_update_events` | `project_update_events_pkey` | `CREATE UNIQUE INDEX project_update_events_pkey ON public.project_update_events USING btree (id)` |
| `project_update_events` | `project_update_events_project_route_id_publication_batch_id_key` | `CREATE UNIQUE INDEX project_update_events_project_route_id_publication_batch_id_key ON public.project_update_events USING btree (project_route_id, publication_batch_id)` |
| `project_update_events` | `project_update_events_public_id_key` | `CREATE UNIQUE INDEX project_update_events_public_id_key ON public.project_update_events USING btree (public_id)` |
| `project_update_notification_tasks` | `idx_project_update_notification_tasks_ready` | `CREATE INDEX idx_project_update_notification_tasks_ready ON public.project_update_notification_tasks USING btree (status, next_attempt_at, event_id)` |
| `project_update_notification_tasks` | `project_update_notification_tasks_pkey` | `CREATE UNIQUE INDEX project_update_notification_tasks_pkey ON public.project_update_notification_tasks USING btree (event_id)` |
| `public_id_registry` | `public_id_registry_pkey` | `CREATE UNIQUE INDEX public_id_registry_pkey ON public.public_id_registry USING btree (public_id)` |
| `public_routes` | `public_routes_entity_type_internal_id_key` | `CREATE UNIQUE INDEX public_routes_entity_type_internal_id_key ON public.public_routes USING btree (entity_type, internal_id)` |
| `public_routes` | `public_routes_pkey` | `CREATE UNIQUE INDEX public_routes_pkey ON public.public_routes USING btree (id)` |
| `public_routes` | `public_routes_public_id_key` | `CREATE UNIQUE INDEX public_routes_public_id_key ON public.public_routes USING btree (public_id)` |
| `recipe_binding_candidates` | `idx_recipe_binding_candidates_resource` | `CREATE INDEX idx_recipe_binding_candidates_resource ON public.recipe_binding_candidates USING btree (resource_id, binding_id)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_binding_id_candidate_index_key` | `CREATE UNIQUE INDEX recipe_binding_candidates_binding_id_candidate_index_key ON public.recipe_binding_candidates USING btree (binding_id, candidate_index)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_identity_key_key` | `CREATE UNIQUE INDEX recipe_binding_candidates_identity_key_key ON public.recipe_binding_candidates USING btree (identity_key)` |
| `recipe_binding_candidates` | `recipe_binding_candidates_pkey` | `CREATE UNIQUE INDEX recipe_binding_candidates_pkey ON public.recipe_binding_candidates USING btree (id)` |
| `recipe_bindings` | `idx_fk_recipe_bindings_recipe_bindings_template_slot_i_1d228830` | `CREATE INDEX idx_fk_recipe_bindings_recipe_bindings_template_slot_i_1d228830 ON public.recipe_bindings USING btree (template_slot_id)` |
| `recipe_bindings` | `recipe_bindings_identity_key_key` | `CREATE UNIQUE INDEX recipe_bindings_identity_key_key ON public.recipe_bindings USING btree (identity_key)` |
| `recipe_bindings` | `recipe_bindings_pkey` | `CREATE UNIQUE INDEX recipe_bindings_pkey ON public.recipe_bindings USING btree (id)` |
| `recipe_bindings` | `recipe_bindings_recipe_id_template_slot_id_key` | `CREATE UNIQUE INDEX recipe_bindings_recipe_id_template_slot_id_key ON public.recipe_bindings USING btree (recipe_id, template_slot_id)` |
| `recipe_content_overrides` | `idx_fk_recipe_content_overrides_fk_recipe_content_over_2c245ccd` | `CREATE INDEX idx_fk_recipe_content_overrides_fk_recipe_content_over_2c245ccd ON public.recipe_content_overrides USING btree (published_revision_id)` |
| `recipe_content_overrides` | `idx_fk_recipe_content_overrides_recipe_content_overrid_f6a3218b` | `CREATE INDEX idx_fk_recipe_content_overrides_recipe_content_overrid_f6a3218b ON public.recipe_content_overrides USING btree (updated_by)` |
| `recipe_content_overrides` | `recipe_content_overrides_pkey` | `CREATE UNIQUE INDEX recipe_content_overrides_pkey ON public.recipe_content_overrides USING btree (recipe_id)` |
| `recipe_definitions` | `idx_fk_recipe_definitions_fk_recipe_definitions_revisi_b45017fe` | `CREATE INDEX idx_fk_recipe_definitions_fk_recipe_definitions_revisi_b45017fe ON public.recipe_definitions USING btree (published_revision_id)` |
| `recipe_definitions` | `idx_fk_recipe_definitions_recipe_definitions_source_mo_927e14cc` | `CREATE INDEX idx_fk_recipe_definitions_recipe_definitions_source_mo_927e14cc ON public.recipe_definitions USING btree (source_mod_content_version_id)` |
| `recipe_definitions` | `idx_fk_recipe_definitions_recipe_definitions_template__80ef33a5` | `CREATE INDEX idx_fk_recipe_definitions_recipe_definitions_template__80ef33a5 ON public.recipe_definitions USING btree (template_id)` |
| `recipe_definitions` | `idx_fk_recipe_definitions_recipe_definitions_updated_b_e82d6f81` | `CREATE INDEX idx_fk_recipe_definitions_recipe_definitions_updated_b_e82d6f81 ON public.recipe_definitions USING btree (updated_by)` |
| `recipe_definitions` | `idx_recipe_definitions_source_version` | `CREATE INDEX idx_recipe_definitions_source_version ON public.recipe_definitions USING btree (source_mod_content_version_id) WHERE (source_mod_content_version_id IS NOT NULL)` |
| `recipe_definitions` | `recipe_definitions_pkey` | `CREATE UNIQUE INDEX recipe_definitions_pkey ON public.recipe_definitions USING btree (recipe_id)` |
| `recipe_import_binding_candidates` | `idx_fk_recipe_import_binding_candidates_recipe_import__e37dcdcc` | `CREATE INDEX idx_fk_recipe_import_binding_candidates_recipe_import__e37dcdcc ON public.recipe_import_binding_candidates USING btree (resource_id)` |
| `recipe_import_binding_candidates` | `idx_recipe_import_binding_candidates_resource` | `CREATE INDEX idx_recipe_import_binding_candidates_resource ON public.recipe_import_binding_candidates USING btree (resource_id, binding_id) WHERE (resource_id IS NOT NULL)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candida_binding_id_alternative_index__key` | `CREATE UNIQUE INDEX recipe_import_binding_candida_binding_id_alternative_index__key ON public.recipe_import_binding_candidates USING btree (binding_id, alternative_index, raw_resource_id)` |
| `recipe_import_binding_candidates` | `recipe_import_binding_candidates_pkey` | `CREATE UNIQUE INDEX recipe_import_binding_candidates_pkey ON public.recipe_import_binding_candidates USING btree (id)` |
| `recipe_import_bindings` | `idx_fk_recipe_import_bindings_recipe_import_bindings_t_8bde3ce9` | `CREATE INDEX idx_fk_recipe_import_bindings_recipe_import_bindings_t_8bde3ce9 ON public.recipe_import_bindings USING btree (template_slot_id)` |
| `recipe_import_bindings` | `idx_fk_recipe_import_bindings_recipe_import_bindings_t_ba4b36aa` | `CREATE INDEX idx_fk_recipe_import_bindings_recipe_import_bindings_t_ba4b36aa ON public.recipe_import_bindings USING btree (tag_id)` |
| `recipe_import_bindings` | `idx_recipe_import_bindings_tag` | `CREATE INDEX idx_recipe_import_bindings_tag ON public.recipe_import_bindings USING btree (tag_id, recipe_snapshot_id) WHERE (tag_id IS NOT NULL)` |
| `recipe_import_bindings` | `recipe_import_bindings_pkey` | `CREATE UNIQUE INDEX recipe_import_bindings_pkey ON public.recipe_import_bindings USING btree (id)` |
| `recipe_import_bindings` | `recipe_import_bindings_recipe_snapshot_id_source_slot_id_key` | `CREATE UNIQUE INDEX recipe_import_bindings_recipe_snapshot_id_source_slot_id_key ON public.recipe_import_bindings USING btree (recipe_snapshot_id, source_slot_id)` |
| `recipe_import_snapshots` | `idx_fk_recipe_import_snapshots_recipe_import_snapshots_418ff5d6` | `CREATE INDEX idx_fk_recipe_import_snapshots_recipe_import_snapshots_418ff5d6 ON public.recipe_import_snapshots USING btree (template_id)` |
| `recipe_import_snapshots` | `idx_recipe_import_snapshots_revision` | `CREATE INDEX idx_recipe_import_snapshots_revision ON public.recipe_import_snapshots USING btree (revision_id, recipe_id)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_pkey` | `CREATE UNIQUE INDEX recipe_import_snapshots_pkey ON public.recipe_import_snapshots USING btree (id)` |
| `recipe_import_snapshots` | `recipe_import_snapshots_recipe_id_revision_id_source_recipe_key` | `CREATE UNIQUE INDEX recipe_import_snapshots_recipe_id_revision_id_source_recipe_key ON public.recipe_import_snapshots USING btree (recipe_id, revision_id, source_recipe_key)` |
| `recipe_layout_templates` | `idx_fk_recipe_layout_templates_fk_recipe_layout_templa_9f5ff224` | `CREATE INDEX idx_fk_recipe_layout_templates_fk_recipe_layout_templa_9f5ff224 ON public.recipe_layout_templates USING btree (published_revision_id)` |
| `recipe_layout_templates` | `idx_fk_recipe_layout_templates_recipe_layout_templates_b545a4f6` | `CREATE INDEX idx_fk_recipe_layout_templates_recipe_layout_templates_b545a4f6 ON public.recipe_layout_templates USING btree (updated_by)` |
| `recipe_layout_templates` | `idx_fk_recipe_layout_templates_recipe_layout_templates_cea666db` | `CREATE INDEX idx_fk_recipe_layout_templates_recipe_layout_templates_cea666db ON public.recipe_layout_templates USING btree (import_snapshot_id)` |
| `recipe_layout_templates` | `idx_fk_recipe_layout_templates_recipe_layout_templates_dbbca25e` | `CREATE INDEX idx_fk_recipe_layout_templates_recipe_layout_templates_dbbca25e ON public.recipe_layout_templates USING btree (background_file_id)` |
| `recipe_layout_templates` | `idx_recipe_layout_templates_type` | `CREATE INDEX idx_recipe_layout_templates_type ON public.recipe_layout_templates USING btree (recipe_type_id, template_key)` |
| `recipe_layout_templates` | `recipe_layout_templates_pkey` | `CREATE UNIQUE INDEX recipe_layout_templates_pkey ON public.recipe_layout_templates USING btree (entity_id)` |
| `recipe_layout_templates` | `recipe_layout_templates_recipe_type_id_template_key_key` | `CREATE UNIQUE INDEX recipe_layout_templates_recipe_type_id_template_key_key ON public.recipe_layout_templates USING btree (recipe_type_id, template_key)` |
| `recipe_template_import_slots` | `recipe_template_import_slots_pkey` | `CREATE UNIQUE INDEX recipe_template_import_slots_pkey ON public.recipe_template_import_slots USING btree (id)` |
| `recipe_template_import_slots` | `recipe_template_import_slots_template_id_source_slot_id_key` | `CREATE UNIQUE INDEX recipe_template_import_slots_template_id_source_slot_id_key ON public.recipe_template_import_slots USING btree (template_id, source_slot_id)` |
| `recipe_template_import_snapshots` | `idx_fk_recipe_template_import_snapshots_recipe_templat_1815a746` | `CREATE INDEX idx_fk_recipe_template_import_snapshots_recipe_templat_1815a746 ON public.recipe_template_import_snapshots USING btree (recipe_type_id)` |
| `recipe_template_import_snapshots` | `idx_fk_recipe_template_import_snapshots_recipe_templat_5cedbfbc` | `CREATE INDEX idx_fk_recipe_template_import_snapshots_recipe_templat_5cedbfbc ON public.recipe_template_import_snapshots USING btree (canonical_template_id)` |
| `recipe_template_import_snapshots` | `idx_recipe_template_import_snapshots_canonical` | `CREATE INDEX idx_recipe_template_import_snapshots_canonical ON public.recipe_template_import_snapshots USING btree (canonical_template_id, revision_id) WHERE (canonical_template_id IS NOT NULL)` |
| `recipe_template_import_snapshots` | `idx_recipe_template_import_snapshots_revision_type` | `CREATE INDEX idx_recipe_template_import_snapshots_revision_type ON public.recipe_template_import_snapshots USING btree (revision_id, recipe_type_id)` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapsh_recipe_type_snapshot_id_sourc_key` | `CREATE UNIQUE INDEX recipe_template_import_snapsh_recipe_type_snapshot_id_sourc_key ON public.recipe_template_import_snapshots USING btree (recipe_type_snapshot_id, source_template_id)` |
| `recipe_template_import_snapshots` | `recipe_template_import_snapshots_pkey` | `CREATE UNIQUE INDEX recipe_template_import_snapshots_pkey ON public.recipe_template_import_snapshots USING btree (id)` |
| `recipe_template_slots` | `recipe_template_slots_identity_key_key` | `CREATE UNIQUE INDEX recipe_template_slots_identity_key_key ON public.recipe_template_slots USING btree (identity_key)` |
| `recipe_template_slots` | `recipe_template_slots_pkey` | `CREATE UNIQUE INDEX recipe_template_slots_pkey ON public.recipe_template_slots USING btree (id)` |
| `recipe_template_slots` | `recipe_template_slots_template_id_ordinal_key` | `CREATE UNIQUE INDEX recipe_template_slots_template_id_ordinal_key ON public.recipe_template_slots USING btree (template_id, ordinal)` |
| `recipe_template_slots` | `recipe_template_slots_template_id_slot_key_key` | `CREATE UNIQUE INDEX recipe_template_slots_template_id_slot_key_key ON public.recipe_template_slots USING btree (template_id, slot_key)` |
| `recipe_type_catalysts` | `idx_fk_recipe_type_catalysts_fk_recipe_type_catalysts__232d8331` | `CREATE INDEX idx_fk_recipe_type_catalysts_fk_recipe_type_catalysts__232d8331 ON public.recipe_type_catalysts USING btree (published_revision_id)` |
| `recipe_type_catalysts` | `idx_fk_recipe_type_catalysts_recipe_type_catalysts_res_57cade28` | `CREATE INDEX idx_fk_recipe_type_catalysts_recipe_type_catalysts_res_57cade28 ON public.recipe_type_catalysts USING btree (resource_id)` |
| `recipe_type_catalysts` | `recipe_type_catalysts_pkey` | `CREATE UNIQUE INDEX recipe_type_catalysts_pkey ON public.recipe_type_catalysts USING btree (recipe_type_id, resource_id)` |
| `recipe_type_catalysts` | `recipe_type_catalysts_recipe_type_id_ordinal_key` | `CREATE UNIQUE INDEX recipe_type_catalysts_recipe_type_id_ordinal_key ON public.recipe_type_catalysts USING btree (recipe_type_id, ordinal)` |
| `recipe_type_definitions` | `idx_fk_recipe_type_definitions_fk_recipe_type_definiti_a803b1af` | `CREATE INDEX idx_fk_recipe_type_definitions_fk_recipe_type_definiti_a803b1af ON public.recipe_type_definitions USING btree (published_revision_id)` |
| `recipe_type_definitions` | `idx_fk_recipe_type_definitions_recipe_type_definitions_70c0cdb8` | `CREATE INDEX idx_fk_recipe_type_definitions_recipe_type_definitions_70c0cdb8 ON public.recipe_type_definitions USING btree (updated_by)` |
| `recipe_type_definitions` | `recipe_type_definitions_pkey` | `CREATE UNIQUE INDEX recipe_type_definitions_pkey ON public.recipe_type_definitions USING btree (recipe_type_id)` |
| `recipe_type_import_snapshots` | `idx_fk_recipe_type_import_snapshots_recipe_type_import_5280a3e7` | `CREATE INDEX idx_fk_recipe_type_import_snapshots_recipe_type_import_5280a3e7 ON public.recipe_type_import_snapshots USING btree (revision_id)` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_pkey` | `CREATE UNIQUE INDEX recipe_type_import_snapshots_pkey ON public.recipe_type_import_snapshots USING btree (id)` |
| `recipe_type_import_snapshots` | `recipe_type_import_snapshots_recipe_type_id_revision_id_key` | `CREATE UNIQUE INDEX recipe_type_import_snapshots_recipe_type_id_revision_id_key ON public.recipe_type_import_snapshots USING btree (recipe_type_id, revision_id)` |
| `recipe_types` | `recipe_types_canonical_id_key` | `CREATE UNIQUE INDEX recipe_types_canonical_id_key ON public.recipe_types USING btree (canonical_id)` |
| `recipe_types` | `recipe_types_pkey` | `CREATE UNIQUE INDEX recipe_types_pkey ON public.recipe_types USING btree (entity_id)` |
| `recipe_version_bindings` | `idx_recipe_version_bindings_created_by` | `CREATE INDEX idx_recipe_version_bindings_created_by ON public.recipe_version_bindings USING btree (created_by)` |
| `recipe_version_bindings` | `idx_recipe_version_bindings_version` | `CREATE INDEX idx_recipe_version_bindings_version ON public.recipe_version_bindings USING btree (version_code, recipe_id)` |
| `recipe_version_bindings` | `recipe_version_bindings_pkey` | `CREATE UNIQUE INDEX recipe_version_bindings_pkey ON public.recipe_version_bindings USING btree (recipe_id, version_code)` |
| `recipes` | `idx_fk_recipes_recipes_owner_mod_id_fkey_48e5c20c` | `CREATE INDEX idx_fk_recipes_recipes_owner_mod_id_fkey_48e5c20c ON public.recipes USING btree (owner_mod_id)` |
| `recipes` | `idx_recipes_authoritative_identity` | `CREATE UNIQUE INDEX idx_recipes_authoritative_identity ON public.recipes USING btree (recipe_type_id, canonical_source_id) WHERE (canonical_source_id IS NOT NULL)` |
| `recipes` | `idx_recipes_semantic_identity` | `CREATE INDEX idx_recipes_semantic_identity ON public.recipes USING btree (recipe_type_id, semantic_fingerprint)` |
| `recipes` | `recipes_pkey` | `CREATE UNIQUE INDEX recipes_pkey ON public.recipes USING btree (entity_id)` |
| `report_evidence` | `idx_fk_report_evidence_report_evidence_uploader_id_fke_4d53b9e3` | `CREATE INDEX idx_fk_report_evidence_report_evidence_uploader_id_fke_4d53b9e3 ON public.report_evidence USING btree (uploader_id)` |
| `report_evidence` | `idx_report_evidence_cleanup` | `CREATE INDEX idx_report_evidence_cleanup ON public.report_evidence USING btree (status, cleanup_after, id) WHERE (status = ANY (ARRAY['temporary'::text, 'pending_delete'::text]))` |
| `report_evidence` | `idx_report_evidence_report` | `CREATE INDEX idx_report_evidence_report ON public.report_evidence USING btree (report_id, id)` |
| `report_evidence` | `report_evidence_object_key_key` | `CREATE UNIQUE INDEX report_evidence_object_key_key ON public.report_evidence USING btree (object_key)` |
| `report_evidence` | `report_evidence_pkey` | `CREATE UNIQUE INDEX report_evidence_pkey ON public.report_evidence USING btree (id)` |
| `report_evidence` | `report_evidence_public_id_key` | `CREATE UNIQUE INDEX report_evidence_public_id_key ON public.report_evidence USING btree (public_id)` |
| `report_reviews` | `idx_fk_report_reviews_report_reviews_reviewer_id_fkey_142c8f43` | `CREATE INDEX idx_fk_report_reviews_report_reviews_reviewer_id_fkey_142c8f43 ON public.report_reviews USING btree (reviewer_id)` |
| `report_reviews` | `report_reviews_pkey` | `CREATE UNIQUE INDEX report_reviews_pkey ON public.report_reviews USING btree (id)` |
| `report_reviews` | `report_reviews_report_id_idempotency_key_key` | `CREATE UNIQUE INDEX report_reviews_report_id_idempotency_key_key ON public.report_reviews USING btree (report_id, idempotency_key)` |
| `report_snapshots` | `report_snapshots_pkey` | `CREATE UNIQUE INDEX report_snapshots_pkey ON public.report_snapshots USING btree (report_id)` |
| `reports` | `idx_fk_reports_reports_claimed_by_fkey_f05206f3` | `CREATE INDEX idx_fk_reports_reports_claimed_by_fkey_f05206f3 ON public.reports USING btree (claimed_by)` |
| `reports` | `idx_fk_reports_reports_reporter_id_fkey_8e24faa8` | `CREATE INDEX idx_fk_reports_reports_reporter_id_fkey_8e24faa8 ON public.reports USING btree (reporter_id)` |
| `reports` | `idx_fk_reports_reports_target_author_id_fkey_8c5e9762` | `CREATE INDEX idx_fk_reports_reports_target_author_id_fkey_8c5e9762 ON public.reports USING btree (target_author_id)` |
| `reports` | `idx_reports_queue` | `CREATE INDEX idx_reports_queue ON public.reports USING btree (status, created_at, id)` |
| `reports` | `idx_reports_target_history` | `CREATE INDEX idx_reports_target_history ON public.reports USING btree (target_type, target_public_id, created_at DESC, id DESC)` |
| `reports` | `reports_pkey` | `CREATE UNIQUE INDEX reports_pkey ON public.reports USING btree (id)` |
| `reports` | `reports_public_id_key` | `CREATE UNIQUE INDEX reports_public_id_key ON public.reports USING btree (public_id)` |
| `reports` | `uq_reports_open_reporter_target` | `CREATE UNIQUE INDEX uq_reports_open_reporter_target ON public.reports USING btree (reporter_id, target_type, target_public_id) WHERE (status = ANY (ARRAY['pending'::text, 'in_review'::text]))` |
| `resource_import_snapshots` | `idx_resource_import_snapshots_resource` | `CREATE INDEX idx_resource_import_snapshots_resource ON public.resource_import_snapshots USING btree (resource_id, revision_id)` |
| `resource_import_snapshots` | `idx_resource_import_snapshots_revision_registry` | `CREATE INDEX idx_resource_import_snapshots_revision_registry ON public.resource_import_snapshots USING btree (revision_id, registry, resource_id)` |
| `resource_import_snapshots` | `resource_import_snapshots_pkey` | `CREATE UNIQUE INDEX resource_import_snapshots_pkey ON public.resource_import_snapshots USING btree (id)` |
| `resource_import_snapshots` | `resource_import_snapshots_resource_id_revision_id_key` | `CREATE UNIQUE INDEX resource_import_snapshots_resource_id_revision_id_key ON public.resource_import_snapshots USING btree (resource_id, revision_id)` |
| `resource_import_snapshots` | `resource_import_snapshots_revision_id_registry_resource_id_key` | `CREATE UNIQUE INDEX resource_import_snapshots_revision_id_registry_resource_id_key ON public.resource_import_snapshots USING btree (revision_id, registry, resource_id)` |
| `resource_kinds` | `resource_kinds_pkey` | `CREATE UNIQUE INDEX resource_kinds_pkey ON public.resource_kinds USING btree (code)` |
| `review_completion_subscriptions` | `idx_review_completion_subscriptions_user` | `CREATE INDEX idx_review_completion_subscriptions_user ON public.review_completion_subscriptions USING btree (user_id, created_at DESC)` |
| `review_completion_subscriptions` | `review_completion_subscriptions_pkey` | `CREATE UNIQUE INDEX review_completion_subscriptions_pkey ON public.review_completion_subscriptions USING btree (change_request_id, user_id)` |
| `review_events` | `idx_fk_review_events_review_events_actor_id_fkey_1bbfcb5e` | `CREATE INDEX idx_fk_review_events_review_events_actor_id_fkey_1bbfcb5e ON public.review_events USING btree (actor_id)` |
| `review_events` | `idx_review_events_request_created` | `CREATE INDEX idx_review_events_request_created ON public.review_events USING btree (change_request_id, created_at, id)` |
| `review_events` | `review_events_pkey` | `CREATE UNIQUE INDEX review_events_pkey ON public.review_events USING btree (id)` |
| `review_events` | `review_events_public_id_key` | `CREATE UNIQUE INDEX review_events_public_id_key ON public.review_events USING btree (public_id)` |
| `role_permissions` | `idx_role_permissions_permission_role` | `CREATE INDEX idx_role_permissions_permission_role ON public.role_permissions USING btree (permission_id, role_id)` |
| `role_permissions` | `role_permissions_pkey` | `CREATE UNIQUE INDEX role_permissions_pkey ON public.role_permissions USING btree (role_id, permission_id)` |
| `roles` | `roles_code_key` | `CREATE UNIQUE INDEX roles_code_key ON public.roles USING btree (code)` |
| `roles` | `roles_pkey` | `CREATE UNIQUE INDEX roles_pkey ON public.roles USING btree (id)` |
| `runtime_versions` | `runtime_versions_pkey` | `CREATE UNIQUE INDEX runtime_versions_pkey ON public.runtime_versions USING btree (name)` |
| `schema_metadata` | `schema_metadata_pkey` | `CREATE UNIQUE INDEX schema_metadata_pkey ON public.schema_metadata USING btree (singleton)` |
| `search_index_queue` | `idx_search_index_queue_available` | `CREATE INDEX idx_search_index_queue_available ON public.search_index_queue USING btree (available_at, updated_at)` |
| `search_index_queue` | `search_index_queue_pkey` | `CREATE UNIQUE INDEX search_index_queue_pkey ON public.search_index_queue USING btree (document_type, document_id)` |
| `search_index_state` | `search_index_state_pkey` | `CREATE UNIQUE INDEX search_index_state_pkey ON public.search_index_state USING btree (collection_kind)` |
| `seed_crawler_candidates` | `idx_fk_seed_crawler_candidates_seed_crawler_candidates_c74ae237` | `CREATE INDEX idx_fk_seed_crawler_candidates_seed_crawler_candidates_c74ae237 ON public.seed_crawler_candidates USING btree (run_id)` |
| `seed_crawler_candidates` | `seed_crawler_candidates_external_project_id_key` | `CREATE UNIQUE INDEX seed_crawler_candidates_external_project_id_key ON public.seed_crawler_candidates USING btree (external_project_id)` |
| `seed_crawler_candidates` | `seed_crawler_candidates_pkey` | `CREATE UNIQUE INDEX seed_crawler_candidates_pkey ON public.seed_crawler_candidates USING btree (id)` |
| `seed_crawler_configs` | `idx_fk_seed_crawler_configs_seed_crawler_configs_updat_21387538` | `CREATE INDEX idx_fk_seed_crawler_configs_seed_crawler_configs_updat_21387538 ON public.seed_crawler_configs USING btree (updated_by)` |
| `seed_crawler_configs` | `seed_crawler_configs_pkey` | `CREATE UNIQUE INDEX seed_crawler_configs_pkey ON public.seed_crawler_configs USING btree (id)` |
| `seed_crawler_runs` | `idx_fk_seed_crawler_runs_seed_crawler_runs_actor_id_fk_931bd70a` | `CREATE INDEX idx_fk_seed_crawler_runs_seed_crawler_runs_actor_id_fk_931bd70a ON public.seed_crawler_runs USING btree (actor_id)` |
| `seed_crawler_runs` | `idx_fk_seed_crawler_runs_seed_crawler_runs_requested_b_f7a8f311` | `CREATE INDEX idx_fk_seed_crawler_runs_seed_crawler_runs_requested_b_f7a8f311 ON public.seed_crawler_runs USING btree (requested_by)` |
| `seed_crawler_runs` | `idx_seed_crawler_runs_ready` | `CREATE INDEX idx_seed_crawler_runs_ready ON public.seed_crawler_runs USING btree (status, next_attempt_at, id)` |
| `seed_crawler_runs` | `seed_crawler_runs_pkey` | `CREATE UNIQUE INDEX seed_crawler_runs_pkey ON public.seed_crawler_runs USING btree (id)` |
| `seed_crawler_runs` | `seed_crawler_runs_public_id_key` | `CREATE UNIQUE INDEX seed_crawler_runs_public_id_key ON public.seed_crawler_runs USING btree (public_id)` |
| `seed_crawler_translation_tasks` | `seed_crawler_translation_tasks_pkey` | `CREATE UNIQUE INDEX seed_crawler_translation_tasks_pkey ON public.seed_crawler_translation_tasks USING btree (candidate_id, locale)` |
| `shop_items` | `idx_fk_shop_items_shop_items_price_currency_id_fkey_169ba167` | `CREATE INDEX idx_fk_shop_items_shop_items_price_currency_id_fkey_169ba167 ON public.shop_items USING btree (price_currency_id)` |
| `shop_items` | `shop_items_code_key` | `CREATE UNIQUE INDEX shop_items_code_key ON public.shop_items USING btree (code)` |
| `shop_items` | `shop_items_pkey` | `CREATE UNIQUE INDEX shop_items_pkey ON public.shop_items USING btree (id)` |
| `shop_items` | `shop_items_public_id_key` | `CREATE UNIQUE INDEX shop_items_public_id_key ON public.shop_items USING btree (public_id)` |
| `shop_purchases` | `idx_fk_shop_purchases_shop_purchases_currency_id_fkey_e4f22981` | `CREATE INDEX idx_fk_shop_purchases_shop_purchases_currency_id_fkey_e4f22981 ON public.shop_purchases USING btree (currency_id)` |
| `shop_purchases` | `idx_fk_shop_purchases_shop_purchases_shop_item_id_fkey_c5b5e3e2` | `CREATE INDEX idx_fk_shop_purchases_shop_purchases_shop_item_id_fkey_c5b5e3e2 ON public.shop_purchases USING btree (shop_item_id)` |
| `shop_purchases` | `idx_fk_shop_purchases_shop_purchases_user_id_fkey_335351bf` | `CREATE INDEX idx_fk_shop_purchases_shop_purchases_user_id_fkey_335351bf ON public.shop_purchases USING btree (user_id)` |
| `shop_purchases` | `shop_purchases_pkey` | `CREATE UNIQUE INDEX shop_purchases_pkey ON public.shop_purchases USING btree (id)` |
| `simple_project_gallery_images` | `idx_fk_simple_project_gallery_images_simple_project_ga_1aa1d205` | `CREATE INDEX idx_fk_simple_project_gallery_images_simple_project_ga_1aa1d205 ON public.simple_project_gallery_images USING btree (published_revision_id)` |
| `simple_project_gallery_images` | `idx_fk_simple_project_gallery_images_simple_project_ga_6988e688` | `CREATE INDEX idx_fk_simple_project_gallery_images_simple_project_ga_6988e688 ON public.simple_project_gallery_images USING btree (created_by)` |
| `simple_project_gallery_images` | `idx_fk_simple_project_gallery_images_simple_project_ga_f7848f93` | `CREATE INDEX idx_fk_simple_project_gallery_images_simple_project_ga_f7848f93 ON public.simple_project_gallery_images USING btree (oss_file_id)` |
| `simple_project_gallery_images` | `idx_simple_project_gallery_order` | `CREATE INDEX idx_simple_project_gallery_order ON public.simple_project_gallery_images USING btree (project_id, display_order, id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_pkey` | `CREATE UNIQUE INDEX simple_project_gallery_images_pkey ON public.simple_project_gallery_images USING btree (id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_project_id_oss_file_id_key` | `CREATE UNIQUE INDEX simple_project_gallery_images_project_id_oss_file_id_key ON public.simple_project_gallery_images USING btree (project_id, oss_file_id)` |
| `simple_project_gallery_images` | `simple_project_gallery_images_public_id_key` | `CREATE UNIQUE INDEX simple_project_gallery_images_public_id_key ON public.simple_project_gallery_images USING btree (public_id)` |
| `simple_project_links` | `idx_simple_project_links_order` | `CREATE INDEX idx_simple_project_links_order ON public.simple_project_links USING btree (project_id, display_order, id)` |
| `simple_project_links` | `simple_project_links_pkey` | `CREATE UNIQUE INDEX simple_project_links_pkey ON public.simple_project_links USING btree (id)` |
| `simple_project_links` | `simple_project_links_project_id_link_type_url_key` | `CREATE UNIQUE INDEX simple_project_links_project_id_link_type_url_key ON public.simple_project_links USING btree (project_id, link_type, url)` |
| `simple_project_localizations` | `idx_simple_project_localizations_name` | `CREATE INDEX idx_simple_project_localizations_name ON public.simple_project_localizations USING btree (lower(name))` |
| `simple_project_localizations` | `simple_project_localizations_pkey` | `CREATE UNIQUE INDEX simple_project_localizations_pkey ON public.simple_project_localizations USING btree (project_id, locale)` |
| `simple_project_parent_refs` | `idx_fk_simple_project_parent_refs_simple_project_paren_496c5ddb` | `CREATE INDEX idx_fk_simple_project_parent_refs_simple_project_paren_496c5ddb ON public.simple_project_parent_refs USING btree (target_type, target_id)` |
| `simple_project_parent_refs` | `idx_fk_simple_project_parent_refs_simple_project_paren_4a05d95c` | `CREATE INDEX idx_fk_simple_project_parent_refs_simple_project_paren_4a05d95c ON public.simple_project_parent_refs USING btree (project_id)` |
| `simple_project_parent_refs` | `idx_simple_project_parent_identity` | `CREATE UNIQUE INDEX idx_simple_project_parent_identity ON public.simple_project_parent_refs USING btree (project_id, target_type, COALESCE(target_id, (0)::bigint), raw_identifier)` |
| `simple_project_parent_refs` | `idx_simple_project_parent_target` | `CREATE INDEX idx_simple_project_parent_target ON public.simple_project_parent_refs USING btree (target_type, target_id, project_id) WHERE (target_id IS NOT NULL)` |
| `simple_project_parent_refs` | `simple_project_parent_refs_pkey` | `CREATE UNIQUE INDEX simple_project_parent_refs_pkey ON public.simple_project_parent_refs USING btree (id)` |
| `simple_projects` | `idx_fk_simple_projects_simple_projects_published_revis_7c4c1a92` | `CREATE INDEX idx_fk_simple_projects_simple_projects_published_revis_7c4c1a92 ON public.simple_projects USING btree (published_revision_id)` |
| `simple_projects` | `idx_simple_projects_catalog` | `CREATE INDEX idx_simple_projects_catalog ON public.simple_projects USING btree (project_type, review_status, updated_at DESC, id DESC)` |
| `simple_projects` | `idx_simple_projects_categories` | `CREATE INDEX idx_simple_projects_categories ON public.simple_projects USING gin (categories)` |
| `simple_projects` | `idx_simple_projects_submitted_by` | `CREATE INDEX idx_simple_projects_submitted_by ON public.simple_projects USING btree (submitted_by, updated_at DESC)` |
| `simple_projects` | `idx_simple_projects_versions` | `CREATE INDEX idx_simple_projects_versions ON public.simple_projects USING gin (minecraft_versions)` |
| `simple_projects` | `simple_projects_pkey` | `CREATE UNIQUE INDEX simple_projects_pkey ON public.simple_projects USING btree (id)` |
| `simple_projects` | `simple_projects_project_type_slug_key` | `CREATE UNIQUE INDEX simple_projects_project_type_slug_key ON public.simple_projects USING btree (project_type, slug)` |
| `simple_projects` | `simple_projects_public_id_key` | `CREATE UNIQUE INDEX simple_projects_public_id_key ON public.simple_projects USING btree (public_id)` |
| `site_changelog_translations` | `idx_fk_site_changelog_translations_site_changelog_tran_3a048298` | `CREATE INDEX idx_fk_site_changelog_translations_site_changelog_tran_3a048298 ON public.site_changelog_translations USING btree (updated_by)` |
| `site_changelog_translations` | `site_changelog_translations_pkey` | `CREATE UNIQUE INDEX site_changelog_translations_pkey ON public.site_changelog_translations USING btree (changelog_id, locale)` |
| `site_changelogs` | `idx_fk_site_changelogs_site_changelogs_created_by_fkey_16da33fe` | `CREATE INDEX idx_fk_site_changelogs_site_changelogs_created_by_fkey_16da33fe ON public.site_changelogs USING btree (created_by)` |
| `site_changelogs` | `idx_site_changelogs_public` | `CREATE INDEX idx_site_changelogs_public ON public.site_changelogs USING btree (change_date DESC, id DESC) WHERE (status = 'published'::text)` |
| `site_changelogs` | `site_changelogs_pkey` | `CREATE UNIQUE INDEX site_changelogs_pkey ON public.site_changelogs USING btree (id)` |
| `site_changelogs` | `site_changelogs_public_id_key` | `CREATE UNIQUE INDEX site_changelogs_public_id_key ON public.site_changelogs USING btree (public_id)` |
| `site_daily_active_users` | `idx_fk_site_daily_active_users_site_daily_active_users_100520de` | `CREATE INDEX idx_fk_site_daily_active_users_site_daily_active_users_100520de ON public.site_daily_active_users USING btree (user_id)` |
| `site_daily_active_users` | `site_daily_active_users_pkey` | `CREATE UNIQUE INDEX site_daily_active_users_pkey ON public.site_daily_active_users USING btree (activity_date, user_id)` |
| `site_daily_metrics` | `site_daily_metrics_pkey` | `CREATE UNIQUE INDEX site_daily_metrics_pkey ON public.site_daily_metrics USING btree (metric_date)` |
| `site_monthly_active_users` | `idx_fk_site_monthly_active_users_site_monthly_active_u_40bb3f1e` | `CREATE INDEX idx_fk_site_monthly_active_users_site_monthly_active_u_40bb3f1e ON public.site_monthly_active_users USING btree (user_id)` |
| `site_monthly_active_users` | `idx_site_monthly_active_users_recent` | `CREATE INDEX idx_site_monthly_active_users_recent ON public.site_monthly_active_users USING btree (activity_month, last_active_date DESC, user_id)` |
| `site_monthly_active_users` | `site_monthly_active_users_pkey` | `CREATE UNIQUE INDEX site_monthly_active_users_pkey ON public.site_monthly_active_users USING btree (activity_month, user_id)` |
| `site_page_translations` | `idx_fk_site_page_translations_site_page_translations_u_76df1945` | `CREATE INDEX idx_fk_site_page_translations_site_page_translations_u_76df1945 ON public.site_page_translations USING btree (updated_by)` |
| `site_page_translations` | `site_page_translations_pkey` | `CREATE UNIQUE INDEX site_page_translations_pkey ON public.site_page_translations USING btree (page_id, locale)` |
| `site_pages` | `idx_fk_site_pages_site_pages_updated_by_fkey_cbe22dd6` | `CREATE INDEX idx_fk_site_pages_site_pages_updated_by_fkey_cbe22dd6 ON public.site_pages USING btree (updated_by)` |
| `site_pages` | `site_pages_code_key` | `CREATE UNIQUE INDEX site_pages_code_key ON public.site_pages USING btree (code)` |
| `site_pages` | `site_pages_pkey` | `CREATE UNIQUE INDEX site_pages_pkey ON public.site_pages USING btree (id)` |
| `site_view_daily` | `site_view_daily_pkey` | `CREATE UNIQUE INDEX site_view_daily_pkey ON public.site_view_daily USING btree (metric_date, counter_shard)` |
| `skin_asset_adoptions` | `idx_fk_skin_asset_adoptions_skin_asset_adoptions_asset_20471d97` | `CREATE INDEX idx_fk_skin_asset_adoptions_skin_asset_adoptions_asset_20471d97 ON public.skin_asset_adoptions USING btree (asset_id)` |
| `skin_asset_adoptions` | `skin_asset_adoptions_pkey` | `CREATE UNIQUE INDEX skin_asset_adoptions_pkey ON public.skin_asset_adoptions USING btree (user_id, asset_id)` |
| `skin_assets` | `idx_fk_skin_assets_fk_skin_assets_published_revision_6a0f97b9` | `CREATE INDEX idx_fk_skin_assets_fk_skin_assets_published_revision_6a0f97b9 ON public.skin_assets USING btree (published_revision_id)` |
| `skin_assets` | `idx_skin_assets_blob` | `CREATE INDEX idx_skin_assets_blob ON public.skin_assets USING btree (blob_hash, id)` |
| `skin_assets` | `idx_skin_assets_catalog` | `CREATE INDEX idx_skin_assets_catalog ON public.skin_assets USING btree (status, review_status, visibility, kind, created_at DESC, id DESC)` |
| `skin_assets` | `idx_skin_assets_owner` | `CREATE INDEX idx_skin_assets_owner ON public.skin_assets USING btree (owner_id, status, updated_at DESC, id DESC)` |
| `skin_assets` | `idx_skin_assets_tags` | `CREATE INDEX idx_skin_assets_tags ON public.skin_assets USING gin (tags)` |
| `skin_assets` | `skin_assets_pkey` | `CREATE UNIQUE INDEX skin_assets_pkey ON public.skin_assets USING btree (id)` |
| `skin_assets` | `skin_assets_public_id_key` | `CREATE UNIQUE INDEX skin_assets_public_id_key ON public.skin_assets USING btree (public_id)` |
| `skin_texture_blobs` | `skin_texture_blobs_object_key_key` | `CREATE UNIQUE INDEX skin_texture_blobs_object_key_key ON public.skin_texture_blobs USING btree (object_key)` |
| `skin_texture_blobs` | `skin_texture_blobs_oss_file_id_key` | `CREATE UNIQUE INDEX skin_texture_blobs_oss_file_id_key ON public.skin_texture_blobs USING btree (oss_file_id)` |
| `skin_texture_blobs` | `skin_texture_blobs_pkey` | `CREATE UNIQUE INDEX skin_texture_blobs_pkey ON public.skin_texture_blobs USING btree (hash)` |
| `skin_wardrobe` | `idx_fk_skin_wardrobe_skin_wardrobe_asset_id_fkey_f14d820e` | `CREATE INDEX idx_fk_skin_wardrobe_skin_wardrobe_asset_id_fkey_f14d820e ON public.skin_wardrobe USING btree (asset_id)` |
| `skin_wardrobe` | `idx_skin_wardrobe_user_added` | `CREATE INDEX idx_skin_wardrobe_user_added ON public.skin_wardrobe USING btree (user_id, added_at DESC, asset_id)` |
| `skin_wardrobe` | `skin_wardrobe_pkey` | `CREATE UNIQUE INDEX skin_wardrobe_pkey ON public.skin_wardrobe USING btree (user_id, asset_id)` |
| `sticker_catalog_state` | `sticker_catalog_state_pkey` | `CREATE UNIQUE INDEX sticker_catalog_state_pkey ON public.sticker_catalog_state USING btree (singleton)` |
| `sticker_pack_translations` | `sticker_pack_translations_pkey` | `CREATE UNIQUE INDEX sticker_pack_translations_pkey ON public.sticker_pack_translations USING btree (pack_id, locale)` |
| `sticker_packs` | `idx_fk_sticker_packs_sticker_packs_created_by_fkey_331eaef1` | `CREATE INDEX idx_fk_sticker_packs_sticker_packs_created_by_fkey_331eaef1 ON public.sticker_packs USING btree (created_by)` |
| `sticker_packs` | `idx_sticker_packs_active_order` | `CREATE INDEX idx_sticker_packs_active_order ON public.sticker_packs USING btree (status, sort_order, id)` |
| `sticker_packs` | `sticker_packs_code_key` | `CREATE UNIQUE INDEX sticker_packs_code_key ON public.sticker_packs USING btree (code)` |
| `sticker_packs` | `sticker_packs_pkey` | `CREATE UNIQUE INDEX sticker_packs_pkey ON public.sticker_packs USING btree (id)` |
| `sticker_translations` | `sticker_translations_pkey` | `CREATE UNIQUE INDEX sticker_translations_pkey ON public.sticker_translations USING btree (sticker_id, locale)` |
| `stickers` | `idx_fk_stickers_stickers_created_by_fkey_2a0864e8` | `CREATE INDEX idx_fk_stickers_stickers_created_by_fkey_2a0864e8 ON public.stickers USING btree (created_by)` |
| `stickers` | `idx_fk_stickers_stickers_image_file_id_fkey_f0980c6d` | `CREATE INDEX idx_fk_stickers_stickers_image_file_id_fkey_f0980c6d ON public.stickers USING btree (image_file_id)` |
| `stickers` | `idx_stickers_pack_active_order` | `CREATE INDEX idx_stickers_pack_active_order ON public.stickers USING btree (pack_id, status, sort_order, id)` |
| `stickers` | `stickers_pack_id_code_key` | `CREATE UNIQUE INDEX stickers_pack_id_code_key ON public.stickers USING btree (pack_id, code)` |
| `stickers` | `stickers_pkey` | `CREATE UNIQUE INDEX stickers_pkey ON public.stickers USING btree (id)` |
| `system_settings` | `idx_fk_system_settings_system_settings_updated_by_fkey_54faac53` | `CREATE INDEX idx_fk_system_settings_system_settings_updated_by_fkey_54faac53 ON public.system_settings USING btree (updated_by)` |
| `system_settings` | `system_settings_pkey` | `CREATE UNIQUE INDEX system_settings_pkey ON public.system_settings USING btree (key)` |
| `tag_import_members` | `idx_fk_tag_import_members_tag_import_members_resource__951836fb` | `CREATE INDEX idx_fk_tag_import_members_tag_import_members_resource__951836fb ON public.tag_import_members USING btree (resource_id)` |
| `tag_import_members` | `idx_tag_import_members_resource` | `CREATE INDEX idx_tag_import_members_resource ON public.tag_import_members USING btree (resource_id) WHERE (resource_id IS NOT NULL)` |
| `tag_import_members` | `tag_import_members_pkey` | `CREATE UNIQUE INDEX tag_import_members_pkey ON public.tag_import_members USING btree (tag_snapshot_id, raw_member_id)` |
| `tag_import_snapshots` | `idx_fk_tag_import_snapshots_tag_import_snapshots_revis_1bfedb16` | `CREATE INDEX idx_fk_tag_import_snapshots_tag_import_snapshots_revis_1bfedb16 ON public.tag_import_snapshots USING btree (revision_id)` |
| `tag_import_snapshots` | `tag_import_snapshots_pkey` | `CREATE UNIQUE INDEX tag_import_snapshots_pkey ON public.tag_import_snapshots USING btree (id)` |
| `tag_import_snapshots` | `tag_import_snapshots_tag_id_revision_id_key` | `CREATE UNIQUE INDEX tag_import_snapshots_tag_id_revision_id_key ON public.tag_import_snapshots USING btree (tag_id, revision_id)` |
| `task_definitions` | `idx_fk_task_definitions_task_definitions_created_by_fk_4b7fea53` | `CREATE INDEX idx_fk_task_definitions_task_definitions_created_by_fk_4b7fea53 ON public.task_definitions USING btree (created_by)` |
| `task_definitions` | `task_definitions_code_key` | `CREATE UNIQUE INDEX task_definitions_code_key ON public.task_definitions USING btree (code)` |
| `task_definitions` | `task_definitions_pkey` | `CREATE UNIQUE INDEX task_definitions_pkey ON public.task_definitions USING btree (id)` |
| `task_definitions` | `task_definitions_public_id_key` | `CREATE UNIQUE INDEX task_definitions_public_id_key ON public.task_definitions USING btree (public_id)` |
| `unresolved_references` | `idx_unresolved_references_pending` | `CREATE INDEX idx_unresolved_references_pending ON public.unresolved_references USING btree (reference_type, normalized_identifier, id) WHERE (status = 'pending'::text)` |
| `unresolved_references` | `idx_unresolved_references_source` | `CREATE INDEX idx_unresolved_references_source ON public.unresolved_references USING btree (source_type, source_id, id)` |
| `unresolved_references` | `unresolved_references_pkey` | `CREATE UNIQUE INDEX unresolved_references_pkey ON public.unresolved_references USING btree (id)` |
| `unresolved_references` | `unresolved_references_source_type_source_id_field_path_refe_key` | `CREATE UNIQUE INDEX unresolved_references_source_type_source_id_field_path_refe_key ON public.unresolved_references USING btree (source_type, source_id, field_path, reference_type, normalized_identifier)` |
| `unresolved_resource_references` | `idx_fk_unresolved_resource_references_unresolved_resou_0958a704` | `CREATE INDEX idx_fk_unresolved_resource_references_unresolved_resou_0958a704 ON public.unresolved_resource_references USING btree (source_revision_id)` |
| `unresolved_resource_references` | `idx_fk_unresolved_resource_references_unresolved_resou_e288c8f5` | `CREATE INDEX idx_fk_unresolved_resource_references_unresolved_resou_e288c8f5 ON public.unresolved_resource_references USING btree (resolved_resource_id)` |
| `unresolved_resource_references` | `idx_unresolved_resource_references_pending` | `CREATE INDEX idx_unresolved_resource_references_pending ON public.unresolved_resource_references USING btree (kind_code, raw_resource_id) WHERE (status = 'pending'::text)` |
| `unresolved_resource_references` | `unresolved_resource_reference_source_entity_id_source_revis_key` | `CREATE UNIQUE INDEX unresolved_resource_reference_source_entity_id_source_revis_key ON public.unresolved_resource_references USING btree (source_entity_id, source_revision_id, field_path, kind_code, raw_resource_id)` |
| `unresolved_resource_references` | `unresolved_resource_references_pkey` | `CREATE UNIQUE INDEX unresolved_resource_references_pkey ON public.unresolved_resource_references USING btree (id)` |
| `user_activity_events` | `idx_activity_action_time` | `CREATE INDEX idx_activity_action_time ON public.user_activity_events USING btree (action_id, occurred_at, id)` |
| `user_activity_events` | `idx_activity_object_time` | `CREATE INDEX idx_activity_object_time ON public.user_activity_events USING btree (object_type_id, object_route_id, occurred_at DESC, id DESC)` |
| `user_activity_events` | `idx_activity_time_brin` | `CREATE INDEX idx_activity_time_brin ON public.user_activity_events USING brin (occurred_at)` |
| `user_activity_events` | `idx_activity_user_time` | `CREATE INDEX idx_activity_user_time ON public.user_activity_events USING btree (user_id, occurred_at DESC, id DESC)` |
| `user_activity_events` | `idx_fk_user_activity_events_user_activity_events_objec_fe31e7f7` | `CREATE INDEX idx_fk_user_activity_events_user_activity_events_objec_fe31e7f7 ON public.user_activity_events USING btree (object_route_id)` |
| `user_activity_events` | `user_activity_events_pkey` | `CREATE UNIQUE INDEX user_activity_events_pkey ON public.user_activity_events USING btree (id)` |
| `user_blocks` | `idx_user_blocks_blocked_blocker` | `CREATE INDEX idx_user_blocks_blocked_blocker ON public.user_blocks USING btree (blocked_id, blocker_id)` |
| `user_blocks` | `user_blocks_pkey` | `CREATE UNIQUE INDEX user_blocks_pkey ON public.user_blocks USING btree (blocker_id, blocked_id)` |
| `user_chat_presence` | `idx_fk_user_chat_presence_user_chat_presence_conversat_9af870d2` | `CREATE INDEX idx_fk_user_chat_presence_user_chat_presence_conversat_9af870d2 ON public.user_chat_presence USING btree (conversation_id)` |
| `user_chat_presence` | `user_chat_presence_pkey` | `CREATE UNIQUE INDEX user_chat_presence_pkey ON public.user_chat_presence USING btree (user_id)` |
| `user_checkins` | `idx_user_checkins_claimed` | `CREATE INDEX idx_user_checkins_claimed ON public.user_checkins USING btree (user_id, claimed_at DESC)` |
| `user_checkins` | `user_checkins_pkey` | `CREATE UNIQUE INDEX user_checkins_pkey ON public.user_checkins USING btree (user_id, local_date)` |
| `user_content_creation_facts` | `idx_user_content_creation_facts_user` | `CREATE INDEX idx_user_content_creation_facts_user ON public.user_content_creation_facts USING btree (user_id, created_at DESC, content_type)` |
| `user_content_creation_facts` | `user_content_creation_facts_pkey` | `CREATE UNIQUE INDEX user_content_creation_facts_pkey ON public.user_content_creation_facts USING btree (content_type, object_key)` |
| `user_currency_balances` | `idx_fk_user_currency_balances_user_currency_balances_c_a08cbe71` | `CREATE INDEX idx_fk_user_currency_balances_user_currency_balances_c_a08cbe71 ON public.user_currency_balances USING btree (currency_id)` |
| `user_currency_balances` | `user_currency_balances_pkey` | `CREATE UNIQUE INDEX user_currency_balances_pkey ON public.user_currency_balances USING btree (user_id, currency_id)` |
| `user_daily_contributions` | `user_daily_contributions_pkey` | `CREATE UNIQUE INDEX user_daily_contributions_pkey ON public.user_daily_contributions USING btree (user_id, contribution_date)` |
| `user_drafts` | `idx_fk_user_drafts_user_drafts_change_request_id_fkey_257dfc44` | `CREATE INDEX idx_fk_user_drafts_user_drafts_change_request_id_fkey_257dfc44 ON public.user_drafts USING btree (change_request_id)` |
| `user_drafts` | `idx_fk_user_drafts_user_drafts_review_target_id_fkey_30470395` | `CREATE INDEX idx_fk_user_drafts_user_drafts_review_target_id_fkey_30470395 ON public.user_drafts USING btree (review_target_id)` |
| `user_drafts` | `idx_user_drafts_active_key` | `CREATE UNIQUE INDEX idx_user_drafts_active_key ON public.user_drafts USING btree (user_id, draft_key) WHERE (submitted_at IS NULL)` |
| `user_drafts` | `idx_user_drafts_change_request` | `CREATE INDEX idx_user_drafts_change_request ON public.user_drafts USING btree (change_request_id) WHERE (change_request_id IS NOT NULL)` |
| `user_drafts` | `idx_user_drafts_expiration` | `CREATE INDEX idx_user_drafts_expiration ON public.user_drafts USING btree (expires_at)` |
| `user_drafts` | `idx_user_drafts_owner_project` | `CREATE INDEX idx_user_drafts_owner_project ON public.user_drafts USING btree (user_id, project_key, COALESCE(submitted_at, updated_at) DESC, id DESC)` |
| `user_drafts` | `idx_user_drafts_owner_updated` | `CREATE INDEX idx_user_drafts_owner_updated ON public.user_drafts USING btree (user_id, updated_at DESC, id DESC)` |
| `user_drafts` | `idx_user_drafts_review_target` | `CREATE INDEX idx_user_drafts_review_target ON public.user_drafts USING btree (review_target_id) WHERE (review_target_id IS NOT NULL)` |
| `user_drafts` | `user_drafts_pkey` | `CREATE UNIQUE INDEX user_drafts_pkey ON public.user_drafts USING btree (id)` |
| `user_drafts` | `user_drafts_public_id_key` | `CREATE UNIQUE INDEX user_drafts_public_id_key ON public.user_drafts USING btree (public_id)` |
| `user_experience` | `user_experience_pkey` | `CREATE UNIQUE INDEX user_experience_pkey ON public.user_experience USING btree (user_id)` |
| `user_follows` | `idx_user_follows_followed_created_at` | `CREATE INDEX idx_user_follows_followed_created_at ON public.user_follows USING btree (followed_id, created_at DESC)` |
| `user_follows` | `user_follows_pkey` | `CREATE UNIQUE INDEX user_follows_pkey ON public.user_follows USING btree (follower_id, followed_id)` |
| `user_inventory` | `idx_fk_user_inventory_user_inventory_shop_item_id_fkey_34c183bd` | `CREATE INDEX idx_fk_user_inventory_user_inventory_shop_item_id_fkey_34c183bd ON public.user_inventory USING btree (shop_item_id)` |
| `user_inventory` | `user_inventory_pkey` | `CREATE UNIQUE INDEX user_inventory_pkey ON public.user_inventory USING btree (user_id, shop_item_id)` |
| `user_login_logs` | `idx_fk_user_login_logs_user_login_logs_user_id_fkey_e029c52d` | `CREATE INDEX idx_fk_user_login_logs_user_login_logs_user_id_fkey_e029c52d ON public.user_login_logs USING btree (user_id)` |
| `user_login_logs` | `idx_user_login_logs_failed_account` | `CREATE INDEX idx_user_login_logs_failed_account ON public.user_login_logs USING btree (lower(account), created_at DESC) WHERE (success = false)` |
| `user_login_logs` | `idx_user_login_logs_failed_ip` | `CREATE INDEX idx_user_login_logs_failed_ip ON public.user_login_logs USING btree (ip, created_at DESC) WHERE (success = false)` |
| `user_login_logs` | `idx_user_login_logs_yggdrasil_failures` | `CREATE INDEX idx_user_login_logs_yggdrasil_failures ON public.user_login_logs USING btree (user_id, created_at DESC) WHERE ((success = false) AND (reason ~~ 'yggdrasil_%'::text))` |
| `user_login_logs` | `idx_user_login_logs_yggdrasil_ip_failures` | `CREATE INDEX idx_user_login_logs_yggdrasil_ip_failures ON public.user_login_logs USING btree (ip, created_at DESC) WHERE ((success = false) AND (reason ~~ 'yggdrasil_%'::text))` |
| `user_login_logs` | `user_login_logs_pkey` | `CREATE UNIQUE INDEX user_login_logs_pkey ON public.user_login_logs USING btree (id)` |
| `user_notification_settings` | `user_notification_settings_pkey` | `CREATE UNIQUE INDEX user_notification_settings_pkey ON public.user_notification_settings USING btree (user_id)` |
| `user_permissions` | `idx_user_permissions_permission_user` | `CREATE INDEX idx_user_permissions_permission_user ON public.user_permissions USING btree (permission_id, user_id)` |
| `user_permissions` | `user_permissions_pkey` | `CREATE UNIQUE INDEX user_permissions_pkey ON public.user_permissions USING btree (user_id, permission_id)` |
| `user_presence_sessions` | `idx_user_presence_sessions_user_active` | `CREATE INDEX idx_user_presence_sessions_user_active ON public.user_presence_sessions USING btree (user_id, last_active_at DESC)` |
| `user_presence_sessions` | `user_presence_sessions_pkey` | `CREATE UNIQUE INDEX user_presence_sessions_pkey ON public.user_presence_sessions USING btree (session_hash)` |
| `user_registration_attempts` | `idx_user_registration_attempts_ip` | `CREATE INDEX idx_user_registration_attempts_ip ON public.user_registration_attempts USING btree (ip, created_at DESC)` |
| `user_registration_attempts` | `user_registration_attempts_pkey` | `CREATE UNIQUE INDEX user_registration_attempts_pkey ON public.user_registration_attempts USING btree (id)` |
| `user_role_bindings` | `idx_user_role_bindings_role_user` | `CREATE INDEX idx_user_role_bindings_role_user ON public.user_role_bindings USING btree (role_id, user_id)` |
| `user_role_bindings` | `user_role_bindings_pkey` | `CREATE UNIQUE INDEX user_role_bindings_pkey ON public.user_role_bindings USING btree (user_id, role_id)` |
| `user_statistics_daily` | `idx_user_statistics_daily_range` | `CREATE INDEX idx_user_statistics_daily_range ON public.user_statistics_daily USING btree (user_id, stat_date DESC)` |
| `user_statistics_daily` | `user_statistics_daily_pkey` | `CREATE UNIQUE INDEX user_statistics_daily_pkey ON public.user_statistics_daily USING btree (user_id, stat_date)` |
| `user_statistics_totals` | `user_statistics_totals_pkey` | `CREATE UNIQUE INDEX user_statistics_totals_pkey ON public.user_statistics_totals USING btree (user_id)` |
| `user_task_progress` | `idx_fk_user_task_progress_user_task_progress_task_id_f_ac63f8a6` | `CREATE INDEX idx_fk_user_task_progress_user_task_progress_task_id_f_ac63f8a6 ON public.user_task_progress USING btree (task_id)` |
| `user_task_progress` | `idx_user_task_progress_active` | `CREATE INDEX idx_user_task_progress_active ON public.user_task_progress USING btree (user_id, updated_at DESC)` |
| `user_task_progress` | `user_task_progress_pkey` | `CREATE UNIQUE INDEX user_task_progress_pkey ON public.user_task_progress USING btree (user_id, task_id, period_key)` |
| `user_timezone_changes` | `idx_user_timezone_changes` | `CREATE INDEX idx_user_timezone_changes ON public.user_timezone_changes USING btree (user_id, changed_at DESC)` |
| `user_timezone_changes` | `user_timezone_changes_pkey` | `CREATE UNIQUE INDEX user_timezone_changes_pkey ON public.user_timezone_changes USING btree (id)` |
| `users` | `idx_fk_users_fk_users_avatar_file_ee36211d` | `CREATE INDEX idx_fk_users_fk_users_avatar_file_ee36211d ON public.users USING btree (avatar_file_id)` |
| `users` | `idx_fk_users_fk_users_profile_revision_3566c1ed` | `CREATE INDEX idx_fk_users_fk_users_profile_revision_3566c1ed ON public.users USING btree (profile_revision_id)` |
| `users` | `idx_fk_users_users_profile_background_file_id_fkey_69a87369` | `CREATE INDEX idx_fk_users_users_profile_background_file_id_fkey_69a87369 ON public.users USING btree (profile_background_file_id)` |
| `users` | `idx_users_created_at` | `CREATE INDEX idx_users_created_at ON public.users USING btree (created_at, id)` |
| `users` | `idx_users_registration_ip_created` | `CREATE INDEX idx_users_registration_ip_created ON public.users USING btree (registration_ip, created_at DESC) WHERE (registration_ip <> ''::text)` |
| `users` | `uq_users_email_ci` | `CREATE UNIQUE INDEX uq_users_email_ci ON public.users USING btree (lower(email))` |
| `users` | `uq_users_username_ci` | `CREATE UNIQUE INDEX uq_users_username_ci ON public.users USING btree (lower(username))` |
| `users` | `users_email_key` | `CREATE UNIQUE INDEX users_email_key ON public.users USING btree (email)` |
| `users` | `users_pkey` | `CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)` |
| `users` | `users_public_id_key` | `CREATE UNIQUE INDEX users_public_id_key ON public.users USING btree (public_id)` |
| `users` | `users_username_key` | `CREATE UNIQUE INDEX users_username_key ON public.users USING btree (username)` |
| `yggdrasil_accounts` | `yggdrasil_accounts_account_uuid_key` | `CREATE UNIQUE INDEX yggdrasil_accounts_account_uuid_key ON public.yggdrasil_accounts USING btree (account_uuid)` |
| `yggdrasil_accounts` | `yggdrasil_accounts_pkey` | `CREATE UNIQUE INDEX yggdrasil_accounts_pkey ON public.yggdrasil_accounts USING btree (user_id)` |
| `yggdrasil_join_sessions` | `idx_fk_yggdrasil_join_sessions_yggdrasil_join_sessions_1e2fc0f0` | `CREATE INDEX idx_fk_yggdrasil_join_sessions_yggdrasil_join_sessions_1e2fc0f0 ON public.yggdrasil_join_sessions USING btree (token_id)` |
| `yggdrasil_join_sessions` | `idx_fk_yggdrasil_join_sessions_yggdrasil_join_sessions_e7a2b5c2` | `CREATE INDEX idx_fk_yggdrasil_join_sessions_yggdrasil_join_sessions_e7a2b5c2 ON public.yggdrasil_join_sessions USING btree (player_profile_id)` |
| `yggdrasil_join_sessions` | `idx_yggdrasil_join_sessions_expires` | `CREATE INDEX idx_yggdrasil_join_sessions_expires ON public.yggdrasil_join_sessions USING btree (expires_at)` |
| `yggdrasil_join_sessions` | `yggdrasil_join_sessions_pkey` | `CREATE UNIQUE INDEX yggdrasil_join_sessions_pkey ON public.yggdrasil_join_sessions USING btree (server_id)` |
| `yggdrasil_tokens` | `idx_fk_yggdrasil_tokens_yggdrasil_tokens_player_profil_65b09bb0` | `CREATE INDEX idx_fk_yggdrasil_tokens_yggdrasil_tokens_player_profil_65b09bb0 ON public.yggdrasil_tokens USING btree (player_profile_id)` |
| `yggdrasil_tokens` | `idx_fk_yggdrasil_tokens_yggdrasil_tokens_replaced_by_i_c0fcb985` | `CREATE INDEX idx_fk_yggdrasil_tokens_yggdrasil_tokens_replaced_by_i_c0fcb985 ON public.yggdrasil_tokens USING btree (replaced_by_id)` |
| `yggdrasil_tokens` | `idx_yggdrasil_tokens_active` | `CREATE INDEX idx_yggdrasil_tokens_active ON public.yggdrasil_tokens USING btree (user_id, expires_at, id) WHERE (status = ANY (ARRAY['active'::text, 'stale'::text]))` |
| `yggdrasil_tokens` | `idx_yggdrasil_tokens_profile` | `CREATE INDEX idx_yggdrasil_tokens_profile ON public.yggdrasil_tokens USING btree (player_profile_id, status, expires_at) WHERE (player_profile_id IS NOT NULL)` |
| `yggdrasil_tokens` | `idx_yggdrasil_tokens_user_sessions` | `CREATE INDEX idx_yggdrasil_tokens_user_sessions ON public.yggdrasil_tokens USING btree (user_id, issued_at DESC, id DESC)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_access_token_hash_key` | `CREATE UNIQUE INDEX yggdrasil_tokens_access_token_hash_key ON public.yggdrasil_tokens USING btree (access_token_hash)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_pkey` | `CREATE UNIQUE INDEX yggdrasil_tokens_pkey ON public.yggdrasil_tokens USING btree (id)` |
| `yggdrasil_tokens` | `yggdrasil_tokens_public_id_key` | `CREATE UNIQUE INDEX yggdrasil_tokens_public_id_key ON public.yggdrasil_tokens USING btree (public_id)` |

## 非内部触发器

| 表 | 名称 | 定义 |
| --- | --- | --- |
| `anti_abuse_events` | `trg_anti_abuse_daily_stats` | `CREATE TRIGGER trg_anti_abuse_daily_stats AFTER INSERT ON anti_abuse_events FOR EACH ROW EXECUTE FUNCTION aggregate_anti_abuse_event()` |
| `audit_events` | `trg_audit_events_immutable` | `CREATE TRIGGER trg_audit_events_immutable BEFORE DELETE OR UPDATE ON audit_events FOR EACH ROW EXECUTE FUNCTION prevent_immutable_history_mutation()` |
| `blueprint_jobs` | `trg_blueprint_jobs_public_route` | `CREATE TRIGGER trg_blueprint_jobs_public_route AFTER INSERT ON blueprint_jobs FOR EACH ROW EXECUTE FUNCTION register_blueprint_job_public_route()` |
| `blueprint_jobs` | `trg_blueprint_jobs_remove_public_route` | `CREATE TRIGGER trg_blueprint_jobs_remove_public_route AFTER DELETE ON blueprint_jobs FOR EACH ROW EXECUTE FUNCTION remove_blueprint_job_public_route()` |
| `blueprint_variants` | `trg_blueprint_variants_public_route` | `CREATE TRIGGER trg_blueprint_variants_public_route AFTER INSERT ON blueprint_variants FOR EACH ROW EXECUTE FUNCTION register_blueprint_variant_public_route()` |
| `blueprint_variants` | `trg_blueprint_variants_remove_public_route` | `CREATE TRIGGER trg_blueprint_variants_remove_public_route AFTER DELETE ON blueprint_variants FOR EACH ROW EXECUTE FUNCTION remove_blueprint_variant_public_route()` |
| `blueprints` | `trg_blueprints_public_route` | `CREATE TRIGGER trg_blueprints_public_route AFTER INSERT ON blueprints FOR EACH ROW EXECUTE FUNCTION register_blueprint_public_route()` |
| `blueprints` | `trg_blueprints_remove_public_route` | `CREATE TRIGGER trg_blueprints_remove_public_route AFTER DELETE ON blueprints FOR EACH ROW EXECUTE FUNCTION remove_blueprint_public_route()` |
| `catalog_entities` | `trg_catalog_entities_content_subject_locale` | `CREATE TRIGGER trg_catalog_entities_content_subject_locale AFTER UPDATE OF default_locale ON catalog_entities FOR EACH ROW EXECUTE FUNCTION sync_catalog_content_subject_locale()` |
| `catalog_entities` | `trg_catalog_entities_ensure_public_id` | `CREATE TRIGGER trg_catalog_entities_ensure_public_id BEFORE INSERT ON catalog_entities FOR EACH ROW EXECUTE FUNCTION ensure_catalog_public_id()` |
| `catalog_entities` | `trg_catalog_entities_public_route` | `CREATE TRIGGER trg_catalog_entities_public_route AFTER INSERT ON catalog_entities FOR EACH ROW EXECUTE FUNCTION register_catalog_public_route()` |
| `catalog_entities` | `trg_catalog_entities_remove_public_route` | `CREATE TRIGGER trg_catalog_entities_remove_public_route AFTER DELETE ON catalog_entities FOR EACH ROW EXECUTE FUNCTION remove_catalog_public_route()` |
| `catalog_entities` | `trg_search_catalog_entities` | `CREATE TRIGGER trg_search_catalog_entities AFTER INSERT OR DELETE OR UPDATE ON catalog_entities FOR EACH ROW EXECUTE FUNCTION enqueue_search_catalog_entity()` |
| `catalog_import_revisions` | `trg_catalog_import_revision_submitter` | `CREATE TRIGGER trg_catalog_import_revision_submitter BEFORE INSERT ON catalog_import_revisions FOR EACH ROW EXECUTE FUNCTION attribute_catalog_import_revision()` |
| `catalog_import_revisions` | `trg_catalog_import_revisions_metrics` | `CREATE TRIGGER trg_catalog_import_revisions_metrics AFTER UPDATE OF status ON catalog_import_revisions FOR EACH ROW EXECUTE FUNCTION enqueue_metrics_from_import_resolution()` |
| `catalog_tags` | `trg_catalog_tags_resolve_unresolved` | `CREATE TRIGGER trg_catalog_tags_resolve_unresolved AFTER INSERT OR UPDATE OF registry, canonical_id ON catalog_tags FOR EACH ROW EXECUTE FUNCTION resolve_catalog_tag_unresolved_references()` |
| `change_requests` | `trg_change_requests_daily_contribution` | `CREATE TRIGGER trg_change_requests_daily_contribution AFTER INSERT OR UPDATE OF status, resolved_at, submitted_by, entity_type ON change_requests FOR EACH ROW EXECUTE FUNCTION sync_user_daily_contribution()` |
| `change_requests` | `trg_change_requests_metrics` | `CREATE TRIGGER trg_change_requests_metrics AFTER UPDATE OF status ON change_requests FOR EACH ROW EXECUTE FUNCTION enqueue_metrics_from_review_resolution()` |
| `comment_reactions` | `trg_comment_reactions_heat` | `CREATE TRIGGER trg_comment_reactions_heat AFTER INSERT OR DELETE ON comment_reactions FOR EACH ROW EXECUTE FUNCTION refresh_comment_heat_from_reaction()` |
| `comment_watches` | `trg_comment_watches_heat` | `CREATE TRIGGER trg_comment_watches_heat AFTER INSERT OR DELETE OR UPDATE OF status ON comment_watches FOR EACH ROW EXECUTE FUNCTION refresh_comment_heat_from_watch()` |
| `comments` | `trg_comments_heat` | `CREATE TRIGGER trg_comments_heat AFTER INSERT OR DELETE OR UPDATE OF body, status ON comments FOR EACH ROW EXECUTE FUNCTION refresh_comment_heat_from_comment()` |
| `comments` | `trg_comments_popularity` | `CREATE TRIGGER trg_comments_popularity AFTER INSERT OR DELETE OR UPDATE OF status ON comments FOR EACH ROW EXECUTE FUNCTION refresh_popularity_from_comment()` |
| `comments` | `trg_comments_public_route` | `CREATE TRIGGER trg_comments_public_route AFTER INSERT ON comments FOR EACH ROW EXECUTE FUNCTION register_comment_public_route()` |
| `community_post_project_refs` | `trg_community_post_project_ref_unresolved` | `CREATE TRIGGER trg_community_post_project_ref_unresolved AFTER DELETE ON community_post_project_refs FOR EACH ROW EXECUTE FUNCTION remove_community_post_reference_unresolved('community_post_project')` |
| `community_post_project_refs` | `trg_search_community_project_refs` | `CREATE TRIGGER trg_search_community_project_refs AFTER INSERT OR DELETE OR UPDATE ON community_post_project_refs FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('community_post', 'post_id')` |
| `community_post_resource_refs` | `trg_community_post_resource_ref_unresolved` | `CREATE TRIGGER trg_community_post_resource_ref_unresolved AFTER DELETE ON community_post_resource_refs FOR EACH ROW EXECUTE FUNCTION remove_community_post_reference_unresolved('community_post_resource')` |
| `community_post_resource_refs` | `trg_search_community_resource_refs` | `CREATE TRIGGER trg_search_community_resource_refs AFTER INSERT OR DELETE OR UPDATE ON community_post_resource_refs FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('community_post', 'post_id')` |
| `community_post_translations` | `trg_search_community_translations` | `CREATE TRIGGER trg_search_community_translations AFTER INSERT OR DELETE OR UPDATE ON community_post_translations FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('community_post', 'post_id')` |
| `community_posts` | `trg_community_posts_public_route` | `CREATE TRIGGER trg_community_posts_public_route AFTER INSERT ON community_posts FOR EACH ROW EXECUTE FUNCTION register_community_post_public_route()` |
| `community_posts` | `trg_community_posts_remove_public_route` | `CREATE TRIGGER trg_community_posts_remove_public_route AFTER DELETE ON community_posts FOR EACH ROW EXECUTE FUNCTION remove_community_post_public_route()` |
| `community_posts` | `trg_community_posts_user_creation_fact` | `CREATE TRIGGER trg_community_posts_user_creation_fact AFTER INSERT OR DELETE OR UPDATE OF review_status, status ON community_posts FOR EACH ROW EXECUTE FUNCTION sync_user_content_creation_fact('')` |
| `community_posts` | `trg_search_community_posts` | `CREATE TRIGGER trg_search_community_posts AFTER INSERT OR DELETE OR UPDATE ON community_posts FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('community_post')` |
| `content_change_items` | `trg_content_change_items_immutable` | `CREATE TRIGGER trg_content_change_items_immutable BEFORE DELETE OR UPDATE ON content_change_items FOR EACH ROW EXECUTE FUNCTION prevent_immutable_history_mutation()` |
| `content_creator_bindings` | `trg_content_creator_bindings_project_acl` | `CREATE TRIGGER trg_content_creator_bindings_project_acl AFTER INSERT OR DELETE OR UPDATE ON content_creator_bindings FOR EACH STATEMENT EXECUTE FUNCTION bump_project_acl_runtime_version()` |
| `content_creator_bindings` | `trg_search_creator_bindings` | `CREATE TRIGGER trg_search_creator_bindings AFTER INSERT OR DELETE OR UPDATE ON content_creator_bindings FOR EACH ROW EXECUTE FUNCTION enqueue_search_creator_binding()` |
| `content_localizations` | `trg_search_content_localizations` | `CREATE TRIGGER trg_search_content_localizations AFTER INSERT OR DELETE OR UPDATE ON content_localizations FOR EACH ROW EXECUTE FUNCTION enqueue_search_content_localization()` |
| `content_ratings` | `trg_content_ratings_popularity` | `CREATE TRIGGER trg_content_ratings_popularity AFTER INSERT OR DELETE OR UPDATE ON content_ratings FOR EACH ROW EXECUTE FUNCTION refresh_popularity_from_rating()` |
| `content_revisions` | `trg_content_revisions_immutable` | `CREATE TRIGGER trg_content_revisions_immutable BEFORE DELETE OR UPDATE ON content_revisions FOR EACH ROW EXECUTE FUNCTION prevent_immutable_history_mutation()` |
| `content_revisions` | `trg_content_revisions_metrics` | `CREATE TRIGGER trg_content_revisions_metrics AFTER INSERT ON content_revisions FOR EACH ROW EXECUTE FUNCTION enqueue_metrics_from_revision()` |
| `creator_claims` | `trg_creator_claims_permission_version` | `CREATE TRIGGER trg_creator_claims_permission_version AFTER INSERT OR DELETE OR UPDATE ON creator_claims FOR EACH ROW EXECUTE FUNCTION bump_project_access_user_permission_version()` |
| `creator_claims` | `trg_creator_claims_personal_author` | `CREATE TRIGGER trg_creator_claims_personal_author BEFORE INSERT OR UPDATE OF creator_id ON creator_claims FOR EACH ROW EXECUTE FUNCTION enforce_personal_author_claim()` |
| `creator_claims` | `trg_creator_claims_public_route` | `CREATE TRIGGER trg_creator_claims_public_route AFTER INSERT ON creator_claims FOR EACH ROW EXECUTE FUNCTION register_creator_claim_public_route()` |
| `creator_claims` | `trg_creator_claims_remove_public_route` | `CREATE TRIGGER trg_creator_claims_remove_public_route AFTER DELETE ON creator_claims FOR EACH ROW EXECUTE FUNCTION remove_creator_claim_public_route()` |
| `creator_role_definitions` | `trg_creator_role_definitions_project_acl` | `CREATE TRIGGER trg_creator_role_definitions_project_acl AFTER UPDATE OF permission_granting ON creator_role_definitions FOR EACH STATEMENT EXECUTE FUNCTION bump_project_acl_runtime_version()` |
| `creator_role_definitions` | `trg_creator_roles_public_route` | `CREATE TRIGGER trg_creator_roles_public_route AFTER INSERT ON creator_role_definitions FOR EACH ROW EXECUTE FUNCTION register_creator_role_public_route()` |
| `creator_role_definitions` | `trg_creator_roles_remove_public_route` | `CREATE TRIGGER trg_creator_roles_remove_public_route AFTER DELETE ON creator_role_definitions FOR EACH ROW EXECUTE FUNCTION remove_creator_role_public_route()` |
| `creator_team_members` | `trg_creator_team_members_kinds` | `CREATE TRIGGER trg_creator_team_members_kinds BEFORE INSERT OR UPDATE OF team_id, member_creator_id ON creator_team_members FOR EACH ROW EXECUTE FUNCTION enforce_team_author_membership()` |
| `creator_team_members` | `trg_creator_team_members_project_acl` | `CREATE TRIGGER trg_creator_team_members_project_acl AFTER INSERT OR DELETE OR UPDATE ON creator_team_members FOR EACH STATEMENT EXECUTE FUNCTION bump_project_acl_runtime_version()` |
| `creators` | `trg_creators_public_route` | `CREATE TRIGGER trg_creators_public_route AFTER INSERT ON creators FOR EACH ROW EXECUTE FUNCTION register_creator_public_route()` |
| `creators` | `trg_creators_remove_public_route` | `CREATE TRIGGER trg_creators_remove_public_route AFTER DELETE ON creators FOR EACH ROW EXECUTE FUNCTION remove_creator_public_route()` |
| `creators` | `trg_search_creators` | `CREATE TRIGGER trg_search_creators AFTER INSERT OR DELETE OR UPDATE ON creators FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('creator')` |
| `currencies` | `trg_currencies_public_route` | `CREATE TRIGGER trg_currencies_public_route AFTER INSERT ON currencies FOR EACH ROW EXECUTE FUNCTION register_community_public_route('currency', '/economy/currencies/')` |
| `favorite_collection_items` | `trg_favorite_items_popularity` | `CREATE TRIGGER trg_favorite_items_popularity AFTER INSERT OR DELETE ON favorite_collection_items FOR EACH ROW EXECUTE FUNCTION refresh_popularity_from_favorite()` |
| `game_resources` | `trg_search_game_resources` | `CREATE TRIGGER trg_search_game_resources AFTER INSERT OR DELETE OR UPDATE ON game_resources FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('resource', 'entity_id')` |
| `minecraft_server_mods` | `trg_minecraft_server_mods_remove_unresolved` | `CREATE TRIGGER trg_minecraft_server_mods_remove_unresolved AFTER DELETE ON minecraft_server_mods FOR EACH ROW EXECUTE FUNCTION remove_minecraft_server_mod_unresolved_reference()` |
| `minecraft_server_mods` | `trg_search_server_mods` | `CREATE TRIGGER trg_search_server_mods AFTER INSERT OR DELETE OR UPDATE ON minecraft_server_mods FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('server', 'server_id')` |
| `minecraft_servers` | `trg_minecraft_servers_public_route` | `CREATE TRIGGER trg_minecraft_servers_public_route AFTER INSERT ON minecraft_servers FOR EACH ROW EXECUTE FUNCTION register_minecraft_server_public_route()` |
| `minecraft_servers` | `trg_minecraft_servers_remove_public_route` | `CREATE TRIGGER trg_minecraft_servers_remove_public_route AFTER DELETE ON minecraft_servers FOR EACH ROW EXECUTE FUNCTION remove_minecraft_server_public_route()` |
| `minecraft_servers` | `trg_search_servers` | `CREATE TRIGGER trg_search_servers AFTER INSERT OR DELETE OR UPDATE ON minecraft_servers FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('server')` |
| `minecraft_servers` | `trg_servers_user_creation_fact` | `CREATE TRIGGER trg_servers_user_creation_fact AFTER INSERT OR DELETE OR UPDATE OF review_status ON minecraft_servers FOR EACH ROW EXECUTE FUNCTION sync_user_content_creation_fact('server')` |
| `mod_content_section_resources` | `trg_mod_content_placement_identity` | `CREATE TRIGGER trg_mod_content_placement_identity BEFORE INSERT OR UPDATE OF resource_id, version_id ON mod_content_section_resources FOR EACH ROW EXECUTE FUNCTION assign_mod_content_placement_identity()` |
| `mod_content_sections` | `trg_mod_content_section_tree` | `CREATE TRIGGER trg_mod_content_section_tree BEFORE INSERT OR UPDATE OF parent_id, mod_id, version_id ON mod_content_sections FOR EACH ROW EXECUTE FUNCTION validate_mod_content_section_tree()` |
| `mod_content_sections` | `trg_mod_content_sections_public_route` | `CREATE TRIGGER trg_mod_content_sections_public_route AFTER INSERT ON mod_content_sections FOR EACH ROW EXECUTE FUNCTION register_mod_content_public_route('mod_content_section')` |
| `mod_content_templates` | `trg_mod_content_templates_public_route` | `CREATE TRIGGER trg_mod_content_templates_public_route AFTER INSERT ON mod_content_templates FOR EACH ROW EXECUTE FUNCTION register_mod_content_public_route('mod_content_template')` |
| `mod_content_versions` | `trg_mod_content_versions_public_route` | `CREATE TRIGGER trg_mod_content_versions_public_route AFTER INSERT ON mod_content_versions FOR EACH ROW EXECUTE FUNCTION register_mod_content_public_route('mod_content_version')` |
| `mod_identifiers` | `trg_search_mod_identifier_servers` | `CREATE TRIGGER trg_search_mod_identifier_servers AFTER INSERT OR DELETE OR UPDATE ON mod_identifiers FOR EACH ROW EXECUTE FUNCTION enqueue_search_servers_for_mod_parent()` |
| `mod_identifiers` | `trg_search_mod_identifiers` | `CREATE TRIGGER trg_search_mod_identifiers AFTER INSERT OR DELETE OR UPDATE ON mod_identifiers FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('mod', 'mod_id')` |
| `mod_loader_compatibilities` | `trg_search_mod_loaders` | `CREATE TRIGGER trg_search_mod_loaders AFTER INSERT OR DELETE OR UPDATE ON mod_loader_compatibilities FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('mod', 'mod_id')` |
| `mod_metadata_import_jobs` | `trg_mod_metadata_import_jobs_public_route` | `CREATE TRIGGER trg_mod_metadata_import_jobs_public_route AFTER INSERT ON mod_metadata_import_jobs FOR EACH ROW EXECUTE FUNCTION register_mod_metadata_import_job_public_route()` |
| `mod_metadata_import_jobs` | `trg_mod_metadata_import_jobs_remove_public_route` | `CREATE TRIGGER trg_mod_metadata_import_jobs_remove_public_route AFTER DELETE ON mod_metadata_import_jobs FOR EACH ROW EXECUTE FUNCTION remove_mod_metadata_import_job_public_route()` |
| `mod_relationships` | `trg_mod_relationships_remove_unresolved` | `CREATE TRIGGER trg_mod_relationships_remove_unresolved AFTER DELETE ON mod_relationships FOR EACH ROW EXECUTE FUNCTION remove_mod_relationship_unresolved_references()` |
| `mod_tags` | `trg_search_mod_tags` | `CREATE TRIGGER trg_search_mod_tags AFTER INSERT OR DELETE OR UPDATE ON mod_tags FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('mod', 'mod_id')` |
| `modpack_loader_compatibilities` | `trg_search_modpack_loaders` | `CREATE TRIGGER trg_search_modpack_loaders AFTER INSERT OR DELETE OR UPDATE ON modpack_loader_compatibilities FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('modpack', 'modpack_id')` |
| `modpack_mods` | `trg_modpack_mods_remove_unresolved` | `CREATE TRIGGER trg_modpack_mods_remove_unresolved AFTER DELETE ON modpack_mods FOR EACH ROW EXECUTE FUNCTION remove_modpack_mod_unresolved_reference()` |
| `modpack_tags` | `trg_search_modpack_tags` | `CREATE TRIGGER trg_search_modpack_tags AFTER INSERT OR DELETE OR UPDATE ON modpack_tags FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('modpack', 'modpack_id')` |
| `modpacks` | `trg_modpack_public_id_immutable` | `CREATE TRIGGER trg_modpack_public_id_immutable BEFORE UPDATE OF public_id ON modpacks FOR EACH ROW EXECUTE FUNCTION prevent_modpack_public_id_update()` |
| `modpacks` | `trg_modpacks_public_route` | `CREATE TRIGGER trg_modpacks_public_route AFTER INSERT ON modpacks FOR EACH ROW EXECUTE FUNCTION register_modpack_public_route()` |
| `modpacks` | `trg_modpacks_remove_public_route` | `CREATE TRIGGER trg_modpacks_remove_public_route AFTER DELETE ON modpacks FOR EACH ROW EXECUTE FUNCTION remove_modpack_public_route()` |
| `modpacks` | `trg_modpacks_update_public_route` | `CREATE TRIGGER trg_modpacks_update_public_route AFTER UPDATE OF slug ON modpacks FOR EACH ROW EXECUTE FUNCTION update_modpack_public_route()` |
| `modpacks` | `trg_modpacks_user_creation_fact` | `CREATE TRIGGER trg_modpacks_user_creation_fact AFTER INSERT OR DELETE OR UPDATE OF review_status ON modpacks FOR EACH ROW EXECUTE FUNCTION sync_user_content_creation_fact('modpack')` |
| `modpacks` | `trg_search_modpacks` | `CREATE TRIGGER trg_search_modpacks AFTER INSERT OR DELETE OR UPDATE ON modpacks FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('modpack')` |
| `mods` | `trg_mod_unique_id_immutable` | `CREATE TRIGGER trg_mod_unique_id_immutable BEFORE UPDATE OF project_code ON mods FOR EACH ROW EXECUTE FUNCTION prevent_mod_unique_id_update()` |
| `mods` | `trg_mods_public_route` | `CREATE TRIGGER trg_mods_public_route AFTER INSERT ON mods FOR EACH ROW EXECUTE FUNCTION register_mod_public_route()` |
| `mods` | `trg_mods_remove_public_route` | `CREATE TRIGGER trg_mods_remove_public_route AFTER DELETE ON mods FOR EACH ROW EXECUTE FUNCTION remove_mod_public_route()` |
| `mods` | `trg_mods_update_public_route` | `CREATE TRIGGER trg_mods_update_public_route AFTER UPDATE OF slug ON mods FOR EACH ROW EXECUTE FUNCTION update_mod_public_route()` |
| `mods` | `trg_mods_user_creation_fact` | `CREATE TRIGGER trg_mods_user_creation_fact AFTER INSERT OR DELETE OR UPDATE OF review_status ON mods FOR EACH ROW EXECUTE FUNCTION sync_user_content_creation_fact('mod')` |
| `mods` | `trg_search_mod_server_names` | `CREATE TRIGGER trg_search_mod_server_names AFTER DELETE OR UPDATE OF primary_name, secondary_name, project_code ON mods FOR EACH ROW EXECUTE FUNCTION enqueue_search_servers_for_mod()` |
| `mods` | `trg_search_mods` | `CREATE TRIGGER trg_search_mods AFTER INSERT OR DELETE OR UPDATE ON mods FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('mod')` |
| `notifications` | `trg_notifications_public_route` | `CREATE TRIGGER trg_notifications_public_route AFTER INSERT ON notifications FOR EACH ROW EXECUTE FUNCTION register_notification_public_route()` |
| `notifications` | `trg_notifications_remove_public_route` | `CREATE TRIGGER trg_notifications_remove_public_route AFTER DELETE ON notifications FOR EACH ROW EXECUTE FUNCTION remove_notification_public_route()` |
| `oss_files` | `trg_oss_files_public_route` | `CREATE TRIGGER trg_oss_files_public_route AFTER INSERT ON oss_files FOR EACH ROW EXECUTE FUNCTION register_oss_file_public_route()` |
| `oss_files` | `trg_oss_files_remove_public_route` | `CREATE TRIGGER trg_oss_files_remove_public_route AFTER DELETE ON oss_files FOR EACH ROW EXECUTE FUNCTION remove_oss_file_public_route()` |
| `permission_audit_logs` | `trg_permission_audit_logs_immutable` | `CREATE TRIGGER trg_permission_audit_logs_immutable BEFORE DELETE OR UPDATE ON permission_audit_logs FOR EACH ROW EXECUTE FUNCTION prevent_immutable_history_mutation()` |
| `permissions` | `trg_permissions_rbac_version` | `CREATE TRIGGER trg_permissions_rbac_version AFTER INSERT OR DELETE OR UPDATE ON permissions FOR EACH STATEMENT EXECUTE FUNCTION bump_rbac_runtime_version()` |
| `player_profiles` | `trg_player_profiles_public_route` | `CREATE TRIGGER trg_player_profiles_public_route AFTER INSERT ON player_profiles FOR EACH ROW EXECUTE FUNCTION register_player_profile_public_route()` |
| `player_profiles` | `trg_player_profiles_remove_public_route` | `CREATE TRIGGER trg_player_profiles_remove_public_route AFTER DELETE ON player_profiles FOR EACH ROW EXECUTE FUNCTION remove_player_profile_public_route()` |
| `project_changelog_categories` | `trg_project_changelog_categories_public_route` | `CREATE TRIGGER trg_project_changelog_categories_public_route AFTER INSERT ON project_changelog_categories FOR EACH ROW EXECUTE FUNCTION register_project_changelog_category_public_route()` |
| `project_changelog_categories` | `trg_project_changelog_categories_remove_public_route` | `CREATE TRIGGER trg_project_changelog_categories_remove_public_route AFTER DELETE ON project_changelog_categories FOR EACH ROW EXECUTE FUNCTION remove_project_changelog_category_public_route()` |
| `project_changelogs` | `trg_project_changelogs_popularity` | `CREATE TRIGGER trg_project_changelogs_popularity AFTER UPDATE OF review_status, status ON project_changelogs FOR EACH ROW EXECUTE FUNCTION record_changelog_popularity_event()` |
| `project_changelogs` | `trg_project_changelogs_public_route` | `CREATE TRIGGER trg_project_changelogs_public_route AFTER INSERT ON project_changelogs FOR EACH ROW EXECUTE FUNCTION register_project_changelog_public_route()` |
| `project_changelogs` | `trg_project_changelogs_remove_public_route` | `CREATE TRIGGER trg_project_changelogs_remove_public_route AFTER DELETE ON project_changelogs FOR EACH ROW EXECUTE FUNCTION remove_project_changelog_public_route()` |
| `project_editor_applications` | `trg_project_editor_applications_public_route` | `CREATE TRIGGER trg_project_editor_applications_public_route AFTER INSERT ON project_editor_applications FOR EACH ROW EXECUTE FUNCTION register_project_editor_application_public_route()` |
| `project_editor_applications` | `trg_project_editor_applications_remove_public_route` | `CREATE TRIGGER trg_project_editor_applications_remove_public_route AFTER DELETE ON project_editor_applications FOR EACH ROW EXECUTE FUNCTION remove_project_editor_application_public_route()` |
| `project_editor_assignments` | `trg_project_editor_assignments_permission_version` | `CREATE TRIGGER trg_project_editor_assignments_permission_version AFTER INSERT OR DELETE OR UPDATE ON project_editor_assignments FOR EACH ROW EXECUTE FUNCTION bump_project_access_user_permission_version()` |
| `project_files` | `trg_project_files_public_route` | `CREATE TRIGGER trg_project_files_public_route AFTER INSERT ON project_files FOR EACH ROW EXECUTE FUNCTION register_project_file_public_route()` |
| `project_files` | `trg_project_files_remove_public_route` | `CREATE TRIGGER trg_project_files_remove_public_route AFTER DELETE ON project_files FOR EACH ROW EXECUTE FUNCTION remove_project_file_public_route()` |
| `public_routes` | `trg_public_routes_content_subject` | `CREATE TRIGGER trg_public_routes_content_subject AFTER INSERT ON public_routes FOR EACH ROW EXECUTE FUNCTION register_content_subject()` |
| `public_routes` | `trg_public_routes_reserve_id` | `CREATE TRIGGER trg_public_routes_reserve_id BEFORE INSERT ON public_routes FOR EACH ROW EXECUTE FUNCTION reserve_public_route_id()` |
| `recipe_binding_candidates` | `trg_recipe_candidate_output_fields` | `CREATE TRIGGER trg_recipe_candidate_output_fields BEFORE INSERT OR UPDATE ON recipe_binding_candidates FOR EACH ROW EXECUTE FUNCTION validate_recipe_candidate_output_fields()` |
| `recipe_import_binding_candidates` | `trg_recipe_import_candidate_output_fields_insert` | `CREATE TRIGGER trg_recipe_import_candidate_output_fields_insert AFTER INSERT ON recipe_import_binding_candidates REFERENCING NEW TABLE AS new_candidates FOR EACH STATEMENT EXECUTE FUNCTION validate_recipe_import_candidate_output_fields()` |
| `recipe_import_binding_candidates` | `trg_recipe_import_candidate_output_fields_update` | `CREATE TRIGGER trg_recipe_import_candidate_output_fields_update AFTER UPDATE ON recipe_import_binding_candidates REFERENCING NEW TABLE AS new_candidates FOR EACH STATEMENT EXECUTE FUNCTION validate_recipe_import_candidate_output_fields()` |
| `resource_import_snapshots` | `trg_resource_import_snapshots_metrics` | `CREATE TRIGGER trg_resource_import_snapshots_metrics AFTER INSERT OR UPDATE OF revision_id ON resource_import_snapshots FOR EACH ROW EXECUTE FUNCTION enqueue_metrics_from_import_snapshot()` |
| `resource_import_snapshots` | `trg_search_resource_snapshots` | `CREATE TRIGGER trg_search_resource_snapshots AFTER INSERT OR DELETE OR UPDATE ON resource_import_snapshots FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('resource', 'resource_id')` |
| `review_events` | `trg_review_events_immutable` | `CREATE TRIGGER trg_review_events_immutable BEFORE DELETE OR UPDATE ON review_events FOR EACH ROW EXECUTE FUNCTION prevent_immutable_history_mutation()` |
| `role_permissions` | `trg_role_permissions_rbac_version` | `CREATE TRIGGER trg_role_permissions_rbac_version AFTER INSERT OR DELETE OR UPDATE ON role_permissions FOR EACH STATEMENT EXECUTE FUNCTION bump_rbac_runtime_version()` |
| `roles` | `trg_roles_rbac_version` | `CREATE TRIGGER trg_roles_rbac_version AFTER INSERT OR DELETE OR UPDATE ON roles FOR EACH STATEMENT EXECUTE FUNCTION bump_rbac_runtime_version()` |
| `shop_items` | `trg_shop_items_public_route` | `CREATE TRIGGER trg_shop_items_public_route AFTER INSERT ON shop_items FOR EACH ROW EXECUTE FUNCTION register_community_public_route('shop_item', '/shop/')` |
| `simple_project_localizations` | `trg_search_simple_project_localizations` | `CREATE TRIGGER trg_search_simple_project_localizations AFTER INSERT OR DELETE OR UPDATE ON simple_project_localizations FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_parent('simple_project', 'project_id')` |
| `simple_project_parent_refs` | `trg_simple_project_parent_remove_unresolved` | `CREATE TRIGGER trg_simple_project_parent_remove_unresolved AFTER DELETE ON simple_project_parent_refs FOR EACH ROW EXECUTE FUNCTION remove_simple_project_parent_unresolved_reference()` |
| `simple_projects` | `trg_search_simple_projects` | `CREATE TRIGGER trg_search_simple_projects AFTER INSERT OR DELETE OR UPDATE ON simple_projects FOR EACH ROW EXECUTE FUNCTION enqueue_search_index_root('simple_project')` |
| `simple_projects` | `trg_simple_project_identity_immutable` | `CREATE TRIGGER trg_simple_project_identity_immutable BEFORE UPDATE OF public_id, project_type ON simple_projects FOR EACH ROW EXECUTE FUNCTION prevent_simple_project_identity_update()` |
| `simple_projects` | `trg_simple_projects_public_route` | `CREATE TRIGGER trg_simple_projects_public_route AFTER INSERT ON simple_projects FOR EACH ROW EXECUTE FUNCTION register_simple_project_public_route()` |
| `simple_projects` | `trg_simple_projects_remove_public_route` | `CREATE TRIGGER trg_simple_projects_remove_public_route AFTER DELETE ON simple_projects FOR EACH ROW EXECUTE FUNCTION remove_simple_project_public_route()` |
| `simple_projects` | `trg_simple_projects_update_public_route` | `CREATE TRIGGER trg_simple_projects_update_public_route AFTER UPDATE OF slug ON simple_projects FOR EACH ROW EXECUTE FUNCTION update_simple_project_public_route()` |
| `simple_projects` | `trg_simple_projects_user_creation_fact` | `CREATE TRIGGER trg_simple_projects_user_creation_fact AFTER INSERT OR DELETE OR UPDATE OF review_status ON simple_projects FOR EACH ROW EXECUTE FUNCTION sync_user_content_creation_fact('')` |
| `skin_assets` | `trg_skin_assets_public_route` | `CREATE TRIGGER trg_skin_assets_public_route AFTER INSERT ON skin_assets FOR EACH ROW EXECUTE FUNCTION register_skin_asset_public_route()` |
| `skin_assets` | `trg_skin_assets_remove_public_route` | `CREATE TRIGGER trg_skin_assets_remove_public_route AFTER DELETE ON skin_assets FOR EACH ROW EXECUTE FUNCTION remove_skin_asset_public_route()` |
| `system_settings` | `trg_system_settings_runtime_version` | `CREATE TRIGGER trg_system_settings_runtime_version AFTER INSERT OR DELETE OR UPDATE ON system_settings FOR EACH STATEMENT EXECUTE FUNCTION bump_settings_runtime_version()` |
| `task_definitions` | `trg_task_definitions_public_route` | `CREATE TRIGGER trg_task_definitions_public_route AFTER INSERT ON task_definitions FOR EACH ROW EXECUTE FUNCTION register_community_public_route('task', '/tasks/')` |
| `user_activity_events` | `trg_user_activity_statistics` | `CREATE TRIGGER trg_user_activity_statistics AFTER INSERT ON user_activity_events FOR EACH ROW EXECUTE FUNCTION accumulate_user_statistics()` |
| `user_permissions` | `trg_user_permissions_permission_version` | `CREATE TRIGGER trg_user_permissions_permission_version AFTER INSERT OR DELETE OR UPDATE ON user_permissions FOR EACH ROW EXECUTE FUNCTION bump_direct_user_permission_version()` |
| `user_role_bindings` | `trg_user_role_bindings_permission_version` | `CREATE TRIGGER trg_user_role_bindings_permission_version AFTER INSERT OR DELETE OR UPDATE ON user_role_bindings FOR EACH ROW EXECUTE FUNCTION bump_direct_user_permission_version()` |
| `users` | `trg_users_public_route` | `CREATE TRIGGER trg_users_public_route AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION register_user_public_route()` |
| `users` | `trg_users_remove_public_route` | `CREATE TRIGGER trg_users_remove_public_route AFTER DELETE ON users FOR EACH ROW EXECUTE FUNCTION remove_user_public_route()` |
| `users` | `trg_users_secure_auth_material` | `CREATE TRIGGER trg_users_secure_auth_material BEFORE UPDATE OF status, password_hash ON users FOR EACH ROW EXECUTE FUNCTION secure_user_auth_material_change()` |
| `yggdrasil_tokens` | `trg_yggdrasil_tokens_public_route` | `CREATE TRIGGER trg_yggdrasil_tokens_public_route AFTER INSERT ON yggdrasil_tokens FOR EACH ROW EXECUTE FUNCTION register_launcher_session_public_route()` |
| `yggdrasil_tokens` | `trg_yggdrasil_tokens_remove_public_route` | `CREATE TRIGGER trg_yggdrasil_tokens_remove_public_route AFTER DELETE ON yggdrasil_tokens FOR EACH ROW EXECUTE FUNCTION remove_launcher_session_public_route()` |
## 视图

| 名称 | 定义 |
| --- | --- |
| `effective_project_access` | `SELECT assignment.user_id, route.entity_type AS project_type, route.internal_id AS project_id, route.public_id AS project_public_id, 'editor'::text AS access_level, 'editor_assignment'::text AS source_type, assignment.target_route_id AS source_id, ('editor_assignment:'::text \|\| (assignment.target_route_id)::text) AS source_path FROM (project_editor_assignments assignment JOIN published_project_routes route ON ((route.id = assignment.target_route_id))) WHERE (assignment.status = 'active'::text) U…` |
| `published_project_routes` | `SELECT route.id, route.public_id, route.entity_type, route.internal_id FROM (public_routes route JOIN mods target ON (((route.entity_type = 'mod'::text) AND (route.internal_id = target.id)))) WHERE (target.review_status = 'approved'::text) UNION ALL SELECT route.id, route.public_id, route.entity_type, route.internal_id FROM (public_routes route JOIN modpacks target ON (((route.entity_type = 'modpack'::text) AND (route.internal_id = target.id)))) WHERE (target.review_status = 'approved'::text) UN…` |
| `top_level_project_catalog` | `SELECT route.id AS object_route_id, route.public_id, route.entity_type, route.canonical_path, project.primary_name AS name, project.review_status, project.submitted_by, project.created_at, project.updated_at FROM (mods project JOIN public_routes route ON (((route.entity_type = 'mod'::text) AND (route.internal_id = project.id)))) UNION ALL SELECT route.id AS object_route_id, route.public_id, route.entity_type, route.canonical_path, project.primary_name AS name, project.review_status, project.subm…` |

## 序列

- `activity_cleanup_runs_id_seq`
- `activity_event_outbox_id_seq`
- `ai_task_logs_id_seq`
- `ai_tasks_id_seq`
- `anti_abuse_bot_rules_id_seq`
- `anti_abuse_challenges_id_seq`
- `anti_abuse_content_fingerprints_id_seq`
- `anti_abuse_events_id_seq`
- `anti_abuse_restrictions_id_seq`
- `app_logs_id_seq`
- `audit_events_id_seq`
- `auth_sessions_id_seq`
- `ban_records_id_seq`
- `blueprint_jobs_id_seq`
- `blueprint_variants_id_seq`
- `blueprints_id_seq`
- `catalog_entities_id_seq`
- `catalog_import_job_logs_id_seq`
- `change_requests_id_seq`
- `comment_watches_id_seq`
- `comments_id_seq`
- `community_post_project_refs_id_seq`
- `community_post_resource_refs_id_seq`
- `community_posts_id_seq`
- `content_change_items_id_seq`
- `content_creator_bindings_id_seq`
- `content_heat_promotions_id_seq`
- `content_ratings_id_seq`
- `content_revisions_id_seq`
- `creator_claims_id_seq`
- `creator_links_id_seq`
- `creator_role_definitions_id_seq`
- `creators_id_seq`
- `currencies_id_seq`
- `currency_transactions_id_seq`
- `dead_letter_events_id_seq`
- `direct_conversations_id_seq`
- `direct_messages_id_seq`
- `email_verification_codes_id_seq`
- `experience_transactions_id_seq`
- `external_release_bindings_id_seq`
- `favorite_collection_items_id_seq`
- `favorite_collections_id_seq`
- `favorite_modpack_export_items_id_seq`
- `favorite_modpack_export_tasks_id_seq`
- `log_share_entries_id_seq`
- `log_shares_id_seq`
- `minecraft_server_links_id_seq`
- `minecraft_server_mods_id_seq`
- `minecraft_server_status_samples_id_seq`
- `minecraft_servers_id_seq`
- `mirrored_project_files_id_seq`
- `mod_content_sections_id_seq`
- `mod_content_templates_id_seq`
- `mod_content_versions_id_seq`
- `mod_gallery_images_id_seq`
- `mod_identifiers_id_seq`
- `mod_links_id_seq`
- `mod_metadata_import_jobs_id_seq`
- `mod_relationship_groups_id_seq`
- `mod_relationships_id_seq`
- `moderation_actions_id_seq`
- `modpack_gallery_images_id_seq`
- `modpack_links_id_seq`
- `modpack_mods_id_seq`
- `modpacks_id_seq`
- `mods_id_seq`
- `nats_outbox_id_seq`
- `notifications_id_seq`
- `oauth_accounts_id_seq`
- `oss_files_id_seq`
- `oss_object_deletion_outbox_id_seq`
- `oss_scan_logs_id_seq`
- `oss_upload_logs_id_seq`
- `permission_audit_logs_id_seq`
- `permissions_id_seq`
- `player_profile_name_history_id_seq`
- `player_profiles_id_seq`
- `project_auto_update_runs_id_seq`
- `project_auto_update_settings_id_seq`
- `project_changelog_categories_id_seq`
- `project_changelogs_id_seq`
- `project_editor_applications_id_seq`
- `project_external_sources_id_seq`
- `project_files_id_seq`
- `project_update_events_id_seq`
- `public_routes_id_seq`
- `recipe_binding_candidates_id_seq`
- `recipe_bindings_id_seq`
- `recipe_import_binding_candidates_id_seq`
- `recipe_template_slots_id_seq`
- `report_evidence_id_seq`
- `report_reviews_id_seq`
- `reports_id_seq`
- `review_events_id_seq`
- `roles_id_seq`
- `seed_crawler_candidates_id_seq`
- `seed_crawler_runs_id_seq`
- `shop_items_id_seq`
- `shop_purchases_id_seq`
- `simple_project_gallery_images_id_seq`
- `simple_project_links_id_seq`
- `simple_project_parent_refs_id_seq`
- `simple_projects_id_seq`
- `site_changelogs_id_seq`
- `site_pages_id_seq`
- `skin_assets_id_seq`
- `sticker_packs_id_seq`
- `stickers_id_seq`
- `task_definitions_id_seq`
- `unresolved_references_id_seq`
- `unresolved_resource_references_id_seq`
- `user_activity_events_id_seq`
- `user_drafts_id_seq`
- `user_login_logs_id_seq`
- `user_registration_attempts_id_seq`
- `user_timezone_changes_id_seq`
- `users_id_seq`
- `yggdrasil_tokens_id_seq`

## 函数

- `accumulate_user_statistics`
- `aggregate_anti_abuse_event`
- `assign_mod_content_placement_identity`
- `attribute_catalog_import_revision`
- `bump_direct_user_permission_version`
- `bump_project_access_user_permission_version`
- `bump_project_acl_runtime_version`
- `bump_rbac_runtime_version`
- `bump_runtime_version`
- `bump_settings_runtime_version`
- `catalog_entity_internal_id`
- `content_route_for_comment`
- `content_target_created_at`
- `content_target_owner_id`
- `content_user_trust`
- `enforce_personal_author_claim`
- `enforce_team_author_membership`
- `enqueue_comment_heat_refresh`
- `enqueue_content_stats_refresh`
- `enqueue_metrics_from_import_resolution`
- `enqueue_metrics_from_import_snapshot`
- `enqueue_metrics_from_review_resolution`
- `enqueue_metrics_from_revision`
- `enqueue_parent_project_metrics`
- `enqueue_search_catalog_entity`
- `enqueue_search_content_localization`
- `enqueue_search_creator_binding`
- `enqueue_search_index_document`
- `enqueue_search_index_parent`
- `enqueue_search_index_root`
- `enqueue_search_servers_for_mod`
- `enqueue_search_servers_for_mod_parent`
- `ensure_catalog_public_id`
- `increment_jsonb_counter`
- `new_public_id`
- `normalized_popularity`
- `prevent_immutable_history_mutation`
- `prevent_mod_unique_id_update`
- `prevent_modpack_public_id_update`
- `prevent_simple_project_identity_update`
- `record_changelog_popularity_event`
- `record_popularity_event`
- `record_site_activity_batch`
- `refresh_comment_heat`
- `refresh_comment_heat_from_comment`
- `refresh_comment_heat_from_reaction`
- `refresh_comment_heat_from_watch`
- `refresh_content_popularity`
- `refresh_content_rating_global_stats`
- `refresh_content_route_metrics`
- `refresh_popularity_from_comment`
- `refresh_popularity_from_favorite`
- `refresh_popularity_from_rating`
- `refresh_site_current_counters`
- `refresh_site_daily_metrics`
- `register_blueprint_job_public_route`
- `register_blueprint_public_route`
- `register_blueprint_variant_public_route`
- `register_catalog_public_route`
- `register_comment_public_route`
- `register_community_post_public_route`
- `register_community_public_route`
- `register_content_subject`
- `register_creator_claim_public_route`
- `register_creator_public_route`
- `register_creator_role_public_route`
- `register_launcher_session_public_route`
- `register_minecraft_server_public_route`
- `register_mod_content_public_route`
- `register_mod_metadata_import_job_public_route`
- `register_mod_public_route`
- `register_modpack_public_route`
- `register_notification_public_route`
- `register_oss_file_public_route`
- `register_player_profile_public_route`
- `register_project_changelog_category_public_route`
- `register_project_changelog_public_route`
- `register_project_editor_application_public_route`
- `register_project_file_public_route`
- `register_simple_project_public_route`
- `register_skin_asset_public_route`
- `register_user_public_route`
- `remove_blueprint_job_public_route`
- `remove_blueprint_public_route`
- `remove_blueprint_variant_public_route`
- `remove_catalog_public_route`
- `remove_community_post_public_route`
- `remove_community_post_reference_unresolved`
- `remove_creator_claim_public_route`
- `remove_creator_public_route`
- `remove_creator_role_public_route`
- `remove_launcher_session_public_route`
- `remove_minecraft_server_mod_unresolved_reference`
- `remove_minecraft_server_public_route`
- `remove_mod_metadata_import_job_public_route`
- `remove_mod_public_route`
- `remove_mod_relationship_unresolved_references`
- `remove_modpack_mod_unresolved_reference`
- `remove_modpack_public_route`
- `remove_notification_public_route`
- `remove_oss_file_public_route`
- `remove_player_profile_public_route`
- `remove_project_changelog_category_public_route`
- `remove_project_changelog_public_route`
- `remove_project_editor_application_public_route`
- `remove_project_file_public_route`
- `remove_simple_project_parent_unresolved_reference`
- `remove_simple_project_public_route`
- `remove_skin_asset_public_route`
- `remove_user_public_route`
- `reserve_public_route_id`
- `resolve_catalog_tag_unresolved_references`
- `secure_user_auth_material_change`
- `simple_project_path`
- `sync_catalog_content_subject_locale`
- `sync_user_content_creation_fact`
- `sync_user_daily_contribution`
- `update_mod_public_route`
- `update_modpack_public_route`
- `update_simple_project_public_route`
- `validate_mod_content_section_tree`
- `validate_recipe_candidate_output_fields`
- `validate_recipe_import_candidate_output_fields`
