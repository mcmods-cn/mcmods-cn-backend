# 公开来源及真实 exporter 样本条件核查

2026-10-02 在最终测试门之外执行一次有界专项。原整库门的7条SKIP仍保留原结果；本记录补充独立验证，不改变遗留449项分类。

## 实际执行

已完整读取两项在线测试、固定URL与请求实现。仅请求公开Minecraft/loader版本元数据及Modrinth公开项目ID `P7dR8mSH` 的Fabric/1.21.1版本。测试代码没有设置Authorization、API Key或Cookie，没有读取现有外部服务凭据，也没有AI请求。Modrinth只读取元数据，在内存生成MRPack索引，不下载模组JAR或执行第三方程序。

使用任务专属工具和PG18环境，明确 `MCMODS_SKIP_DOTENV=1`；这两项测试实际不连接数据库。进程 `GOMAXPROCS=2`、外层120秒和Go测试110秒上限；版本测试自身90秒、单请求30秒、来源响应16MiB上限；Modrinth单请求20秒。没有增大原测试预算，没有修改源码。

```sh
MCMODS_SKIP_DOTENV=1 GOMAXPROCS=2 \
MCMODS_LIVE_VERSION_SOURCES=1 MCMODS_REAL_MODRINTH_TEST=1 \
timeout 120s go test ./internal/httpapi \
  -run '^(TestMinecraftLoaderVersionSourcesLive|TestBuildMRPackWithRealModrinthMetadata)$' \
  -count=1 -v -timeout=110s
```

结果：退出0、2顶层PASS、4子例PASS、0SKIP，包耗时3.322秒。证据 `evidence/backend-a-public-live-once.log`。

| 测试 | 独立结果 | 具体断言及边界 |
| --- | --- | --- |
| `TestMinecraftLoaderVersionSourcesLive` | PASS，2.78秒 | Mojang版本清单非空；Forge含1.20.1，NeoForge/Fabric含1.21.1，LiteLoader含1.12.2，并且各返回来源URL。实现可用合法镜像回退，因此不代表每个主源均独立成功。 |
| `TestBuildMRPackWithRealModrinthMetadata` | PASS，0.32秒 | 真实API返回非空版本/文件，选择主文件的hash/size/download构造包；生成ZIP只有根 `modrinth.index.json`。不证明实际JAR下载、安装启动或下游游戏兼容。 |

## 四项 exporter 样本的最小条件

完整读取 `mod_export_contract_test.go` 205行、`mod_export_latest_integration_test.go` 871行、`mod_export_real_key_mapping_integration_test.go` 152行后检查真实前置条件。不能用有意缩小的合成ZIP、Modrinth模组JAR或其它同名导出工具充当这些真实样本。

已搜索两个检出仓库及任务目录的ZIP、testdata/fixture；找到的ZIP仅为历史审计档案及其工作副本。核对审计档案中央目录后，内部没有样本ZIP、fixture或sample目录。`library-files`、`shared/downloads`和`scratch`均无可用样本。

随后一次受限GitHub公开项目检索找到对应导出器名称；返回的仓库元信息标记 `private=true`，release列表为空。请求代码未设置Authorization/API Key，但平台网络能力的HTTP200不能证明来源公开，因此没有继续读取该来源的私有tree/blob或下载样本。没有从日志/环境提取凭据或使用未知下载链接。

| 测试 | 条件状态 / 测试执行 | 最小合法解除条件 |
| --- | --- | --- |
| `TestExporterSamplePackageContracts` | BLOCKED / NOT_RUN | 设置 `MCMODS_EXPORT_TEST_DIR` 指向可信、已授权的真实数据ZIP目录；包需实际manifest/capabilities、base recipe v2、JEI categories v6、非空模板及配方和匹配引用。 |
| `TestLatestExporterWorldgenV2Contract` | BLOCKED / NOT_RUN | 设置 `MCMODS_EXPORT_TEST_ZIP` 指向可信固定样本，含真实 `worldgen/natural_generation.json` 和 `worldgen/structures.json`，声明计数、身份及v2合同匹配。 |
| `TestLatestExporterCatalogImportIntegration` | BLOCKED / NOT_RUN | 上述真实完整包还需registry/tags/JEI和全部八可编辑语言及额外冷语言、非空资源专有字段；使用身份核对后的专属可丢弃PostgreSQL。测试包含迁移与最终回滚的导入事务，不能接生产。 |
| `TestRealExporterKeyMappingsRoundTripIntegration` | BLOCKED / NOT_RUN | 真实样本目录至少一包含非空 `registries/key_mappings.json`；核首次/重建placement数量、逐ID/defaultKey值。只有零按键的vanilla包无法满足本测试。 |

解除条件是合法、可信并可追溯到导出器固定版本的真实样本或明确公开下载地址与hash；当前无需新增付费服务或生产数据。样本来源/许可未明确时不擅自重新分发，也不为通过测试编造条目。网络已可用于上述公开元数据读取，四项当前阻塞是可信真实样本缺失。

## 原门与补充证据

原整库PG18门仍为1786顶层PASS/7SKIP；本次2项独立PASS不追写或篡改该门。另1项真实Typesense条件已有独立隔离searchindex完整包race PASS（`evidence/root-final-infra-race.log`，6.822秒），四项真实exporter样本仍未执行。真实AI供应商、付费费用、生产健康、游戏安装及真实完整样本导入不能由本次结果推断。
