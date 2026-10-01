# 插件等项目关联的可见性和故障行为

`/api/v1/content-projects/{projectType}` 覆盖 plugin/map/resource_pack/shader_pack/datapack/addon。父关联通过 `simple_project_parent_refs` 的 `(target_type,target_id)` 外键指向 `public_routes`；这个存在性约束本身不表示父项目已经发布。

读取附属项目时，mod/modpack/其他simple project父元数据仅在父review_status为approved或父submitted_by等于实际访问者时返回。未解析的外部raw_identifier保留；其他未发布父关系从响应中省略，不返回其名称、slug、icon或public ID。parent筛选应用相同边界，避免通过结果数量探测隐藏父项目。编辑模式不能无条件旁路所有父审核。新增绑定已存在父public ID时也要求批准或该父属于当前actor；不会删除数据库内已有关系或清理真实内容。

带访问者差异的登录目录/详情/历史响应以及编辑响应使用 `Cache-Control: private, no-store`。前端字段格式与URL不变；后端先部署可见性修复即可，不需要schema迁移。

图库读取和对象跳转同时要求oss_files仍active、scan_status为clean或trusted_generated，以及受支持的raster MIME。pending/rejected、SVG或其他不可信内容不展示，直达图库URL返回404。只检查上传时信任不能代替每次公开读取的检查。

多批关联读取每批都检查Rows.Err，数据库流错误使整次请求失败，不以部分语言/作者/链接/图库作为成功响应。实际不存在或无可见记录仍404，数据库故障返回脱敏500；分配slug只有已存在冲突才尝试下一个候选，数据库错误立即停止，不连续重试1000次。

隔离验证使用随机、创建时所有权marker确认的PostgreSQL数据库；空库迁移两次由现有helper验证，不读取生产资料。测试通过不证明生产容量、备份恢复、实际CDN策略或全部审核旅程正常。

seed crawler派生创建在网络资源准备之后、真正项目INSERT之前于同一事务锁定并核对当前run-token租约；失去租约返回409且不创建项目。普通请求没有crawler上下文，沿既有创建权限/审核行为。此处只补调用共享fence，不宣称外部上传可随数据库事务回滚；旧worker停机/整体crawler升级顺序见SEED_CRAWLER_DESIGN.md。
