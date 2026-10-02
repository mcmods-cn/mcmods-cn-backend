# 评论楼层设计

## 范围

楼层只属于顶级评论，按已解析的评论对象隔离。范围键为：

```text
target_type + target_id + coalesce(target_version_id, 0)
```

因此模组资料版本等拥有独立评论对象的页面分别从 1 开始；回复和回复的回复均为 `floor_number = NULL`。

## 原子分配

创建顶级评论的同一事务执行：

```sql
INSERT INTO comment_floor_counters(..., last_floor)
VALUES (..., 1)
ON CONFLICT (...) DO UPDATE
SET last_floor = comment_floor_counters.last_floor + 1
RETURNING last_floor;
```

返回值直接写入新评论。唯一部分索引覆盖对象范围和楼层号，提供第二层并发保护。不同对象锁定不同计数器行，不产生全站楼层锁。

## 不可变规则

- 删除、软删除、审核隐藏、驳回、封禁隐藏均不重编号。
- 置顶和热度排序只改变显示顺序，不改变楼层。
- 回复不更新计数器。
- 不可见楼层定位返回“不可见/不存在”，绝不复用为另一条评论。

## 开发库与已有数据

当前 generation 168 按 `DEVELOPMENT_SCHEMA_RESET.md` 的开发期重置策略安装空库结构，没有历史评论楼层的前向回填迁移。不能把该流程用于需要保留数据的数据库。

若将来为已有数据引入前向升级，可按对象分区，以 `created_at, id` 稳定排序为尚无楼层的顶级评论分配 `row_number()`，再以最大楼层初始化计数器。这是待实现的迁移方案，需要核对既有楼层、分批边界、并发写入和重复值后才能执行，不能视为当前已支持或已验证的恢复能力。

## API 与前端定位

评论响应顶级返回 `floorNumber`，回复返回 `null`。定位 API：

```text
GET /api/v1/comment-targets/{targetType}/{targetKey}/comments/floors/{floor}
```

接口复用对象解析、拉黑规则和可见性校验，返回评论 ID 及所在分页位置。前端优先定位已加载 DOM，否则请求定位数据、加载目标页、写入 `#floor-N`、滚动并短暂高亮。

## “数字+楼”解析

`MarkdownRenderer` 中的 remark 插件只遍历 AST 普通文本节点，识别 1–9 位正整数加“楼”。父节点为 link、linkReference、code 或 inlineCode 时跳过；原始 Markdown 不被改写，避免嵌套链接、URL 破坏和 HTML 字符串替换型 XSS。
