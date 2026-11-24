# 反滥用内容的确定性提取

关联：REM-002 邻接回归，以及 CMP-007 补证期间发现的共享根因。该发现不增加原审计 97 项的计数。

`inspectAntiAbuseBody` 原先直接遍历反序列化后的 Go map。同一 JSON 对象的字段顺序不承载业务含义，但随机遍历会改变内容拼接顺序及 exact fingerprint，甚至改变超过 16 个可采集字段时选中的字段集合。

现按原始对象键的字典顺序递归；数组顺序保持原样。允许采集的字段名称、大小写处理、16 项总量、每字段 20,000 个原文字符上限，以及既有 `truncateRunes` 末尾省略号均不变。JSON 请求原始字节、业务字段和所有既有权限、挑战、配额、保留期及窗口不变。新采集结果用于现有 exact hash、SimHash、并发内容占用和安全事件，不新增散列算法或数据库列。

## 已有数据与部署

- 不迁移、删除或重写 `anti_abuse_content_fingerprints`、安全事件或挑战记录。旧记录只有 hash/SimHash，没有原始字段内容和遍历顺序，无法可靠重建其 canonical exact hash。不得枚举猜测字段顺序，也不得为命中率降低重复内容检查。
- 旧多字段 exact hash 可能与新提取结果不同；现有 SimHash 比较继续按原阈值和窗口参与判断，但不能保证每条旧记录都成为 exact hit。单字段及原本恰好 canonical 顺序的记录保持 exact 兼容。旧数据按原保留期自然退出窗口。
- 当前 human challenge 的验证绑定用户、会话、动作、对象、IP 和有效期，并使用已有 nonce/answer HMAC；没有内容 hash 字段。此补丁没有改变 `challenge.go` 或该绑定策略，因此既有未过期 proof 无须改签或失效。不得把这一事实宣传为新增了逐正文授权。新旧进程使用相同配置时均可验证已有 proof。
- 无数据库迁移。建议滚动部署尽快使所有写实例采用相同提取算法；混合版本期间，exact 指纹和本地/跨实例并发占用可能受到原随机顺序差异影响。挑战和现有额度仍正常执行。无需清缓存、重置状态或修改额度。
- 回滚到旧代码不改动已有数据或 nonce，但会恢复旧版随机遍历问题；不建议仅为提高历史 exact 命中率回滚。不存在需要批量修复生产数据的步骤。

## 回归

`TestAuditAntiAbuseCanonicalObjectOrderPreservesArrayOrder` 和 `TestAuditAntiAbuseCanonicalConcurrentFingerprintsAgree` 验证不同 JSON 键序、多次及并发提取的一致性，并保留不同数组元素顺序。`TestAuditAntiAbuseCanonicalCollectionKeepsExistingBudgets` 核验原字段/字符边界。

`TestAuditAntiAbuseCanonicalDistinctFieldsActualHTTPProofSingleWriteIntegration` 通过默认启用的真实 proof、普通新 verified 用户、正常经过 800ms 的表单、HTTP 和 PostgreSQL：以另一普通用户已成功提交产生的指纹自然触发 428，四个采集业务文本互不相同；正确回答保持原业务值、改变 JSON 键序后成功创建且仍待独立审核。重复 proof 由该新账号现有限额首先精确返回 429 `rate_limited` 和正 `Retry-After`，全部业务事实不变；不把这个结果冒充 duplicate 拒绝或 proof 单独验证拒绝。数据库证明 proof 已 consumed，目标只有一条业务对象和审核申请。未修改挑战门槛、TTL、配额、风险状态，未设置管理员、特殊 UA 或模拟 proof 结果。

示例命令（隔离测试数据库，数据库用例按项目合作锁串行）：

```sh
go test -race ./internal/httpapi -run '^TestAuditAntiAbuseCanonical' -count=1 -v
```

数据库场景必须显式设置 `APP_ENV=test`、`MCMODS_RUN_DB_INTEGRATION=1`、`DATABASE_URL` 和 `MCMODS_TEST_DATABASE_URL`；未设置时的 SKIP 仅证明编译，不能计为 HTTP/数据库验收。

## 评论重试与指纹

创建评论与回复共用业务请求的幂等键，优先读取正文，缺省时读取请求头。只有成功提交过的同用户键可以进入回读流程。服务端再次检查当前目标与权限，再在事务内核对键所属记录。原记录消失时返回 409。编辑与私有插眼命令各自执行正常风险检查。

发表评论或重试成功后，公开详情可用时返回完整评论。待审核时返回 `id`、`status=pending` 与 `moderation=true`。详情暂不可读时，服务器返回已提交的 ID、状态和 `detailsUnavailable=true`。客户端可刷新详情；已发布的写入无需重新提交。

SimHash 逐项累加相同的字符特征及其重复次数。已存指纹、相似度阈值和语言处理规则持续适用。
