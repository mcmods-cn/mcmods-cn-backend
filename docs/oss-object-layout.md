# OSS 对象目录规范

本规范描述 Mcmods-cn 后端写入阿里云 OSS 时使用的对象键。对象键统一使用小写 ASCII
目录名；OSS 目录只用于管理和审计，权限判断始终以 PostgreSQL 中的对象归属、项目权限和
`oss_files` 记录为准。

## 根目录

默认根目录为 `mcmods`，可由后台 OSS 设置中的 `prefix` 调整。根目录下只保留两个业务入口：

```text
mcmods/
  project/
  user/
```

## 项目文件

普通项目使用全站公开 ID，而不是可修改的站内短名：

```text
mcmods/project/<project-kind>/<project-public-id>/
```

`project-kind` 当前包括：

```text
mods plugins modpacks maps resourcepacks shaders datapacks blueprints skins authors teams catalogs
```

模组等资料型项目的目录：

```text
mcmods/project/mods/<project-public-id>/
  icons/
    project/original/
    <resource-kind>/<size>/<import-revision-id>/<resource-public-id>.png
  files/
    releases/
    text/<content-public-id>/
    imports/<importer>/
  recipe-gui/
  models/
  assets/
```

- `icons` 使用 `32`、`128`、`256` 等实际尺寸目录。
- 图标增加不可变的导入修订目录。相同全局资源在不同模组资料版本中可以拥有不同贴图，
  旧修订也不会被新导入覆盖。
- 可解析到资源身份的图标以资源的 9 位公开 ID 命名。
- `files/releases` 存放站内发布文件；文件在数据库中仍有独立的 9 位发布 ID。
- `files/text/<content-public-id>` 存放项目简介或具体资料正文中的附件。不能用项目 ID
  代替具体资料 ID，否则无法单独清理某个资料的附件。
- `files/imports` 是短期导入工作区。导入完成并持久化后，任务会删除导入包。
- `models` 保存模型渲染所需的媒体与纹理；规范化属性和可检索技术数据仍保存于数据库。
- `catalogs/_shared/recipe-gui` 用于不隶属于单一模组的全站配方模板。

## 蓝图

```text
mcmods/project/blueprints/<blueprint-public-id>/
  files/
    releases/
      original/
      normalized/
      <converted-format>/
    text/<blueprint-public-id>/cover/
```

蓝图上传时由后端先生成公开 ID，再签发该目录的直传请求。转换任务只向同一蓝图目录写入。

## 皮肤

皮肤资料正文附件使用：

```text
mcmods/project/skins/<skin-public-id>/files/text/<skin-public-id>/
```

经过校验的 Minecraft 皮肤和披风纹理使用内容寻址的共享区：

```text
mcmods/project/skins/_shared/files/releases/textures/<sha256>.png
```

这是有意保留的内部例外。Yggdrasil 纹理协议使用内容哈希，相同纹理只保存一份可以避免重复
存储，并保证纹理 URL 与内容不可变。`skin_assets` 仍以各自的 9 位公开 ID 区分皮肤资料。

## 用户文件

```text
mcmods/user/<numeric-user-id>/files/
  avatars/
  profile/
  messages/
  comments/
  playground/
  applications/
  skins/
  mod-gallery/
  recipe-gui/
  imports/
  authors/
  misc/
```

尚未获得项目公开 ID 的新项目附件先进入用户暂存目录。项目审核通过并发布后，后端使用
OSS 服务端复制将已绑定的画廊文件归档到项目目录，再原子更新 `oss_files` 的对象键。该过程
使用固定工作线程、有界任务队列和单项目去重，不占用 HTTP 请求，也不会无界创建 goroutine。
归档目标文件名使用 `<gallery-public-id>-<file-public-id>.<extension>`，包含不可变的文件身份。同一画廊替换文件后会使用不同目标对象，迟到复制不会覆盖较新文件。数据库更新仍核对原 object key；旧对象按数据库完整 ObjectKey 读取。

归档失败时保留原对象和数据库引用，后续发布操作可以安全重试。配额按 `uploader_id` 统计
该用户上传的所有活动对象，不能通过改用项目目录绕过用户上传额度。

## 安全和生命周期

- 浏览器只能使用后端签发的短时、禁止覆盖的 PUT URL。16 MiB 及以上的文件默认使用
  8 MiB 分片、最多 4 路并发上传；每个分片 URL 独立签名和重试，完成或取消仍必须回到
  后端重新校验对象归属。
- 上传完成接口会根据用户、项目和上传用途重新计算目标前缀，再校验 ObjectKey。
- 文件扩展名只用于可读性；服务端仍校验允许的扩展名、内容类型、大小和 SHA-256。
- 导入生成的对象使用受控并发上传；数据库记录只在 OSS 写入成功后提交。
- 历史对象通过数据库中的完整 ObjectKey 读取，因此旧目录对象不会因目录规范升级而失联。
- 不自动删除未登记的旧对象；清理必须以数据库引用扫描结果为依据。
- Bucket 应配置“清理未完成分片上传”生命周期规则（建议 1 天）。前端失败时会主动请求
  AbortMultipartUpload；生命周期规则用于覆盖浏览器断网、崩溃等无法送达取消请求的情况。
