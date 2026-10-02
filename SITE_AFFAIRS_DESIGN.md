# 站务板块设计

主导航最后一项为“站务”，包含关于本站、更新日志、小黑屋和固定外部链接 `https://wiki.mcmods.cn`。外部 Wiki 在新窗口打开，并带 `noopener noreferrer external` 与可见的外链标识；桌面和移动导航使用同一导航定义。

## 关于本站

`site_pages` 保证 `about` 只有一个当前页面，`site_page_translations` 以 `(page_id, locale)` 唯一。空库种子发布站内语言注册表中的 8 种语言。公开接口优先当前语言，再按站内回退顺序返回，并明确返回实际 locale；前端在回退时提示。后台可选择语言、编辑安全 Markdown、预览、保存草稿和发布，权限为 `site_affairs.about.manage`。

默认 Markdown 正文使用真实换行分隔标题和段落。重复启动补充缺失语言，并只修正精确匹配旧默认正文中字面 `\n` 的未编辑种子：要求版本为 1、操作者为空、仍已发布且标题匹配原默认文案。修正后递增版本，重复启动不会再修改。已编辑、草稿或不匹配默认内容的行保持不变，管理员可在现有编辑页核对并修正。

## 更新日志

`site_changelogs` 保存共同日期与发布状态，`site_changelog_translations` 保存同一事件的多语言标题和正文。列表按日期和 ID 稳定倒序，详情安全渲染 Markdown。后台支持日期、当日快捷按钮、语言、标题、正文、预览和发布，权限为 `site_affairs.changelog.manage`。

## 索引与抓取

关于本站、更新日志和小黑屋静态入口进入 sitemap；后台、API 与登录相关页面仍由 robots 规则排除。小黑屋隐私边界详见 `BAN_AND_BLACKROOM_DESIGN.md`。
