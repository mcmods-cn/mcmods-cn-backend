# JEI 化学资源引用

支持的生产者为 gas、infuse_type、pigment、slurry 分别输出独立的 Mekanism ingredient 类型。规范资源身份对应 `mekanism.gas`、`mekanism.infusion`、`mekanism.pigment`、`mekanism.slurry`，但四者在 `resource_kinds` 中共同属于分类 `chemical`。

分类分组不能代替引用身份。导入资源和资料版本回退查询现在为这四种已注册类型核对具体 `kind_code`，避免把请求 `gas` 与数据库分类 `chemical` 直接比较后丢失合法候选。现有 family 查询行为保留；具体化学类型匹配优先于分组回退，未知类型不会因这项修改获得通用 chemical 匹配。生产者的 `mekanism_infuse_type`、`infuse_type` 别名归一到已有 infusion 类型。

可见性、来源修订、Mod 状态及当前权限条件保持原有公共实现。源 JSON 中的身份、链接和图标声明仍由服务端重新解析；这项修改不会信任客户端提供的公开 ID 或图片 URL。

回归使用合成的合法 v7 类别、模板、四种配方和原生产者结构的 ingredient 索引，经实际 HTTP 上传、业务 Worker、独立审核及匿名 renderer API。四种资源故意使用同一 canonical ID，断言返回四个独立的公开身份、各自的图标和数量，资料版本回退不会跨类型，物品请求不会匹配化学资源。测试是实际 PostgreSQL/HTTP，不能代称真实游戏加载 Mekanism 或 NATS 传输。

修复是读取时的类型匹配，不更改既有资源 ID、family 或源快照，无 schema/数据迁移。部署新版后既有合法图标可重新解析；先前已返回的缓存表示按原缓存策略失效，不删除共享图片或重建账本。
