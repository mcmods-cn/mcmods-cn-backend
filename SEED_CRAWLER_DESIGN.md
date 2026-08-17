# 早期数据填充爬虫

## 边界

该编排器仅处理 Modrinth 的 Mod、插件、光影和材质包，内部类型为 `mod/plugin/shader_pack/resource_pack`。它调用现有 Modrinth API 客户端、来源解析、字段映射、导入任务和草稿/审核流程，不抓 HTML，也不复制导入器。

所有导入任务、草稿、项目创建、内容修订和外部来源绑定都以系统服务账号 `autobot`（`autobot@mcmods.cn`）作为业务发起者。管理员手动点击运行时仍在 `requested_by` 保留操作者，`actor_id` 则固定记录实际执行修改的 `autobot`，两者不会混用。该账号没有可用的交互式密码，通过普通登录接口无法登录。

## 候选与质量

每种启用类型先请求 `/search` 的候选总量，再用加密随机数选择合法 offset，按下载量索引读取随机页，避免每次都是第一条。外部唯一键是 Modrinth project ID，`seed_crawler_candidates.external_project_id` 唯一并保留已处理状态。

默认下载门槛为严格 `downloads > 100`，可在后台调整。候选还必须能被现有导入器正常读取，拥有正文和有效 Minecraft 兼容信息；站内已有外部 ID 会标记为 `existing` 而非重复导入。

## 翻译隔离

草稿携带 `importOrigin=seed_crawler_import`。只有这一来源会按站内语言注册表创建 8 语言翻译任务；用户手工 Modrinth、CurseForge/GitHub 或管理员普通导入不会触发自动翻译。每个 `(candidate, locale)` 独立记录状态和 token 用量，失败只重试对应语言。启用自动提交后，提交仍经过正常项目创建、修订和权限链，但 `autobot` 的 `project.no-review/content.no-review` 权限会使其直接批准；代码不会伪造管理员 Claims，也不会把任务静默归到任意活跃用户。

## 可靠性与成本

PostgreSQL 的 run/candidate/translation 表是事实来源。调度使用 advisory lock，Worker 使用租约、`FOR UPDATE SKIP LOCKED`、指数退避和最多 5 次尝试；NATS 不是唯一任务记录。后台可启停（停用即暂停）、试运行、手动运行，并配置类型、批量、每日限额、间隔、并发、下载门槛、AI 日预算和是否自动提交审核。权限为 `seed_crawler.view/configure/run`，每次配置与运行均审计。

`autobot` 通过现有变量权限系统获得 1000% 的全局限流额度和 1000% 的 `review.submit` 专项额度。该上限仍由反滥用服务统一限制，不存在 Worker 私有的绕过分支。
