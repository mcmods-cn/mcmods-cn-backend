# 真实配方目录页面先决与只读验收

`TestAuditLiveRecipeCatalogFixture` 仅在 `MCMODS_RUN_LIVE_RECIPE_CATALOG=1`
时启动。使用全新绝对 `MCMODS_AUDIT_RECIPE_CATALOG_DIR`，在正常迁移的任务隔离
schema 内通过 HTTP 上传原样真实059b游戏包、创建任务、直接调用实际业务 Worker、
独立批准，再通过普通人工提交/独立批准准备三组页面数据：人工1、导入2、混合1+1。
混合组的导入是明确标注的合成v7契约金样，不冒充另一次真实游戏导出。保留默认审核
3/60秒预算；只在建表/清理时持有任务PG互斥锁，页面观察期不占锁。

该夹具的退出码不证明浏览器或真实NATS成功。运行时文件记录实际三个源码提交和
样本SHA；认证只写独立0600文件。原样游戏包固定SHA和生产者提交均不改写。页面
运行者另记录真实Next/Chromium/请求与可视断言；完成后只写本目录的STOP进行正常
清理。不要在已有封存输出目录重跑或覆盖原失败。

活跃夹具可运行只读观察器，输出目录必须不存在：

```sh
python3 tools/audit/recipe-catalog/diagnose.py \
  /workspace/audit-fixes-20261024/NEW/runtime.json \
  /workspace/audit-fixes-20261024/NEW/terminal-readonly
```

工具核验实际进程UID/命令、OWNER、任务容器标签、数据库schema所有者/注释，只对该
隔离schema执行READ ONLY/REPEATABLE READ查询。匿名HTTP读取真实目录的limit1分页
与render数据，验证不重漏、数量3/概率.5、空输入，以及六条普通人工提交和独立批准。
不联网访问第三方，不读取认证文件，不打印预签名地址，不提供生产apply。

2026-11-01已执行的具名层次与首次观察器错误、原生浏览器报告，见外部主台账中的
CONT-002/CONT-003/GR-010分项。GR-010的真实原样游戏item与合成fluid/gas层分开；
本夹具不把尚未执行的格式或发布环境标为通过。

`TestAuditLiveRecipeGeometryFixture` 是独立的三类合成 v7 配方夹具，使用
`MCMODS_RUN_LIVE_RECIPE_GEOMETRY=1` 和全新的 `MCMODS_AUDIT_RECIPE_GEOMETRY_DIR`。
需要真实图片时还须设置 `MCMODS_AUDIT_GEOMETRY_WITH_ASSETS=1`；只有坐标的样本不能
代替图片绘制验收。可选 `MCMODS_AUDIT_GEOMETRY_VERIFY_WITHDRAWAL=1` 在正常 STOP 后
通过普通版本归档申请和独立审核验证四张图片撤公开及目录缓存失效。

对正在服务的带图片夹具可执行以下只读命令（需要 Pillow）：

```sh
python3 tools/audit/recipe-catalog/diagnose-geometry-canonical.py \
  /workspace/audit-fixes-20261024/NEW/runtime.json \
  /workspace/audit-fixes-20261024/NEW/terminal-canonical
```

该命令继承基础观察器的全部所有权保护，另以数据库快照核对 item、fluid、gas 的
公开身份、具体种类、来源修订、资料版本和图标引用，并检查四张实际公开 PNG 的尺寸、
全部 RGBA 像素及文本元数据清理。输出不包含临时重定向凭据。这些检查不能替代浏览器
的实际绘制、触摸、键盘或缩放断言，也不证明真实游戏化学资源导出或 NATS 传输成功。
