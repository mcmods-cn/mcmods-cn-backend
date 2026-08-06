# 双 ID 架构

Mcmods-cn 的业务对象使用“数字内部 ID + 随机公开 ID”：

- PostgreSQL 主键和表间关联使用 `bigint`；
- REST/JSON、URL、前端状态、审核快照和用户可见日志使用 9 位 `public_id`；
- `public_id` 全站唯一，继续兼容 `mcmods.cn/<public_id>` 的短链设计；
- `public_id` 只能由 PostgreSQL 的 `new_public_id()` 或服务端 CSPRNG 生成，不能再由名称、Minecraft ID、时间戳或内容哈希推导；
- 外部公开 ID 不是权限凭据，服务端解析后仍必须执行对象级权限检查。

Schema generation 60 直接建立这套结构。开发数据不迁移；升级时必须重建开发数据库。

## 核心表约定

```sql
id        bigint primary key
public_id text not null unique default new_public_id()
```

内部外键必须引用数字主键，例如：

```sql
comments.author_id bigint references users(id)
comments.parent_id bigint references comments(id)
project_files.project_internal_id bigint
```

API 不返回这些数字主键。响应中的 `id`、`userId`、`parentId`、`fileId`、`revisionId` 等字段均为对应对象的公开 ID。

## 全局公开 ID 注册表

`public_id_registry` 负责跨对象类型保留公开 ID，`public_routes` 保存：

```text
numeric route id -> public_id -> entity_type + internal_id + canonical_path
```

`public_routes.id` 是全站公共路由的数字内部主键，`public_id` 只是唯一的对外字符标识。各业务表通过插入/删除触发器维护映射。服务端使用 `public_routes` 或对象表的唯一索引把公开 ID 解析为数字主键，不允许前端提交内部主键。

## 边界规则

1. HTTP 路径和 JSON DTO 只接受、返回公开 ID。
2. JWT 对外主体使用用户公开 ID；鉴权中间件解析为数字用户 ID 后再查询权限。
3. 审核修订快照只保存公开 ID。批准发布时，在同一事务内重新解析数字主键。
4. NATS 的纯后端消息可以携带数字内部 ID；一旦任务 ID 返回浏览器，则必须同时拥有并返回公开 ID。
5. Redis 的公开对象缓存键使用公开 ID；缓存值可包含仅供服务端使用的数字 ID。
6. 高频用户操作记录只保存数字用户/对象关联、枚举动作、Markdown 新增字节数和时间，不保存公开 ID 快照或 JSON metadata；后台查询时按数字路由关联公开 ID。
7. 游标分页可在服务端使用递增数字主键排序，游标必须封装为不透明值，不能直接暴露数字 ID。

## 不属于业务公开 ID 的字符串

以下字符串是协议或 Minecraft 技术身份，不转换为随机公开 ID：

- `minecraft:stone`、Tag ID、配方类型 ID、Mod ID；
- Modrinth、CurseForge、GitHub 等外部平台项目/文件 ID；
- OAuth 提供方用户 ID；
- 导入包内部的资源路径、哈希、源条目 ID 和确定性快照键；
- NATS event ID、trace ID、幂等 token。

这些字段不能充当站内业务表之间的通用外键。可独立访问、编辑、审核或授权的站内对象仍必须拥有数字主键和随机公开 ID。

导入溯源层中的确定性 `snapshot`/`source` 键是例外：它们只在隔离的
`*_import_*` 技术表中标识同一份源观察，不能作为 URL、API 对象 ID 或业务表
外键。规范化资源、Tag、配方及其可编辑内容仍全部使用数字内部 ID。

## 当前覆盖对象

- 用户、模组、模组资料版本/模板/分类；
- 全局资源、Tag、配方类型、配方模板、配方；
- 蓝图、蓝图变体与处理任务；
- 皮肤、披风、皮肤库角色；
- 作者/团队、作者角色与认领申请；
- 评论、评论订阅、收藏夹与收藏条目；
- 私信会话、消息、通知；
- OSS 文件、站内项目发行文件；
- 审核修订、变更请求、审核事件、用户行为；
- AI 任务以及模组元数据导入任务。

新增业务对象时，必须同时完成数字主键、公开 ID、全局注册、API DTO 和权限解析，不能只生成一个字符串主键。
