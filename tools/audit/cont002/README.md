# CONT-002 真实页面邻接样本

此工具仅在本次任务的 PostgreSQL 容器与仍存活的隔离游戏夹具中增加两条**合成的人工权威配方**。它核验进程、OWNER、容器标签、schema 所有者及注释和原始游戏包 SHA256，在事务内重复检查所有权。原始游戏阶段必须先封存；人工样本与原游戏验收分别记录。不能将本工具视为人工创作、独立审核或真实游戏原样导出证据。

以新建夹具的实际 runtime 路径运行（旧03已过期，明确拒绝）：

```sh
python3 tools/audit/cont002/seed_owned_neighbor.py \
  --runtime /workspace/audit-fixes-20261024/NEW-OWNED-FIXTURE/runtime.json \
  --namespace auditbrowser04 --apply
```

不带 `--apply` 只输出经所有权核验的 SQL 和原始快照，后续执行须换独立输出 namespace。`--mixed-type` 默认 `minecraft:crafting`，要求该类型已有真实导入快照，不能用伪造导入行代替。全新 namespace 的唯一约束失败会回滚，不更新已有人工、导入或共享对象；只允许增加数据及递增本测试库的目录版本。工具对 12 张表保存前后快照，验证每一原始行仍完全相同。

两种样本的输出均应为数量 `3`、概率 `0.5`，空输入槽 `ingredient_present=false`；人工类型总数 `1`，混合类型原数量加 `1`。结果 `seed-result.json` 包含公开配方和类型 ID。页面应分别验证列表、详情、分页、空槽、数量 `3`、`50%`，以及刷新与历史导航；浏览器未执行时永远记录 `NOT_RUN`。

旧03阶段的实际 API/DB 已通过，但浏览器发现混合配方输出没有绘制数量 badge，见外部 `live-journey-fd25ce3-03/cont002-browser-neighbor/browser-first.json`、HTML 与截图。原失败不因 API 200 或后续修复而覆盖；使用新前端提交和新隔离样本重新验收。
