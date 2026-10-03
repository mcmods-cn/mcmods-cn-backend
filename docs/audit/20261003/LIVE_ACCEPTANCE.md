# 当前 Go 与 Next dev 的真实前后端联通验收

使用 [live_acceptance.py](../../../tools/testing/live_acceptance.py) 编译当前 Go，并将
当前前端 app/lib/public 与运行配置复制到独立 Next dev 目录。浏览器执行前端现有
`browser-tests/live-backend.mts`，登录、收藏和退出的项目 API 均访问真实 Go 与
完整 PostgreSQL 子库，没有替换项目 API 响应。此项与生产构建的受控 API 浏览器
夹具验收分开；Next dev 通过不能证明所有 production 页面完整联通。

## 环境与复现

要求 Linux、Python 3.10+（pidfd 支持）、Docker、项目对应 Go/Node/npm 与 Playwright
Chromium。安装与服务创建约束见[隔离环境说明](../../testing-isolated-environment.md)。
本次实际 Go 1.26.6、Node 24.19、Python 3.12.14；复用匹配锁文件的依赖和浏览器，
没有为该测试购买服务或使用真实供应商凭据。

以下命令在后端根目录运行。路径变量由执行者填写，均不得指向生产环境或现有用户
目录。`task_state` 必须是本工具新建或明确归属的隔离服务记录，不能用普通 DSN
替代；`live_scope` 必须不存在，位于两个仓库和 state 目录之外。

```sh
python3 -B tools/testing/isolated_environment.py --state "$task_state" up

# 新建明确可丢弃的前端副本，只复制安装输入。
mkdir "$live_copy"
cp "$frontend_repo/package.json" "$frontend_repo/package-lock.json" "$live_copy/"
npm ci --prefix "$live_copy"

GOMAXPROCS=2 python3 -B tools/testing/live_acceptance.py \
  --state "$task_state" --frontend "$frontend_repo" \
  --scope "$live_scope" --frontend-copy "$live_copy" \
  --adopt-prepared-copy all

python3 -B -m unittest discover -s tools/testing -p live_acceptance_test.py -v
```

已有本任务专属服务可复用其 state，跳过 `up`。已有匹配依赖且具正确所有者标记的
独立前端副本可复用，后续无需重复 `--adopt-prepared-copy`。不要把源仓库自身作为
副本，也不要覆盖其他任务的副本。工具不会删除未知文件；遇到 stale source、dotenv、
符号链接、锁文件/所有者不匹配会失败，需检查后选择新的专属目录。

`all` 创建随机子库、完整初始化/seed、编译、启动、执行浏览器并清理。也支持同组
参数的 `prepare`、`run`、`cleanup`，用于分阶段排查。`run` 始终重新核对持久化环境，
源码变化则重新编译；运行结束再次核对源码指纹，漂移不得算作当前版本通过。

## 安全边界、停止与恢复

父服务须有精确 owner 标签与回环映射；数据库、Redis、NATS 地址在任何启动前与
这些实际映射交叉核对。子库名、两份数据库 URL、nonce 命名空间和环境文件指纹
也重新验证，不能仅凭名字包含 test 认定可删。完整 schema 的 reset 仅作用于本次
新建的子库，不修改共享母库 schema。

生成随机合成登录密码、JWT/加密/HMAC 密钥，仅写入 0600 私有文件；目录为 0700。
清空外部供应商、SMTP 和付费入口配置，禁 dotenv，合成 schema 不含 AI/OSS 配置，
crawler 默认关闭。在 Go 启动前从当前源码提取 Minecraft 同步 advisory key，在子库
持有该租约；真实应用记录跳过竞争同步，从而本用例不会触发版本源外呼。
Next 与 Go 都只监听 127.0.0.1；浏览器只阻止外部媒体请求，项目 API 不受拦截。

OCT03-A-003 修复三个测试工具误用窗口：父缓存/队列与持久化子目标启动前校验；
已退出 npm/Docker leader 的存活后代仍按 session、nonce、starttime 和 pidfd 清理；
每次启动前原子替换 RUNNING/stopped=false 记录，最终停止凭据绑定本次 run ID。
旧成功凭据不能授权新运行清理或掩盖中断。独立交叉 probe 保留原 RED 和最终 GREEN；
12 个工具回归覆盖错误主机/owner/query override、私有环境、副本、子目标/命名空间、
旧凭据以及真实“leader 已退出、60 秒子进程仍存活”的停止行为。

正常结束向精确进程发送 TERM，必要时 KILL，再确认没有属于本次的存活成员。
失败或强杀使停止无法确认时，`cleanup` 拒绝删除资源；不得把旧结果改写成 true
绕过它。检查该私有 scope 的进程、nonce 与身份并明确停止后，重新执行相应诊断。
本工具不声称机器强杀后会在后台自动恢复。

仅清理本次 `mcmods:live_<nonce>:` Redis key、精确 subject 的独立 NATS stream 与
已记录随机 PostgreSQL 子库，确认各自不存在；不执行全局 flush，不移除父服务。
临时 Go namespace cleanup 文件写在私有 scope 内，finally 删除，不被 `go test ./...`
发现。完成全部使用后，父环境创建者可按精确 state 执行 `isolated_environment.py down`；
其他任务仍使用父服务时不运行此命令。副本/证据保留以便诊断，不递归删除未知目录。

## 实际验收与限制

本次真实用户旅程验证：匿名 me=401；登录=200 与 HttpOnly cookie；收藏创建=201，
刷新后仍存在；中文/英文切换、390px 布局；取消删除保留收藏；确认删除=204，刷新
后消失；退出=200、旧 cookie me=401，再刷新出现登录提示；浏览器 pageerror 为 0。

最终 726 行 runner 的实际运行为 1 PASS、0 SKIP、4.519 秒；
工具 SHA256 为 `387d6fcfee36e770f142fe7817ced1ce837e50226cf65a902d47e1c2a7d6e898`。
实际核对 1,210 个 Go 源码文件，复制 606 个前端运行源码/配置文件；
Go 与前端运行后漂移均为空。
该轮新编译二进制 SHA256 为
`b056cdc978c9871b507e390f0645e6fed7897184a0de0290c897dd3b00411c9b`，
包含最终 187 行数据库 helper 的严格等价函数限定优化及新增测试快照；
helper SHA256 为 `8c052e63dca070443bd754de320b4afe74eeecbd37dee6774066bece24a4f395`。
没有复用早期旧二进制。
旅程后活跃 session、该用例收藏、AI task、provider usage 和 crawler run 均为 0；
停止所属进程，确认子库不存在、43 个所属 Redis key 已清空、所属 NATS stream 已删除。
第一轮加入最后 run ID 保护前也实际通过 1/1、4.237 秒，单独保留其版本证据。
中间版本为 158 行 schema 预检 helper、1/1、4.091 秒、44 个所属 Redis key；
该轮也实际执行最后 run ID 保护，但不包含后来的 helper 优化与新增测试。
最终轮再次新建 nonce 子库、编译和绑定本次 run ID，不将前轮通过冒充最终版本重跑。

本项不覆盖全部角色/模块、生产数据库健康、真实邮件/OSS/AI 语义或计费、长期故障、
浏览器之外的部署代理配置。不会因此将历史所有 partial 状态升级为通过。私有原始
命令、退出码、源码/二进制指纹和清理结果保留在任务证据中；秘密环境文件不提交。
