# OSS 对象目录规范

本规范描述 Mcmods-cn 后端写入阿里云 OSS 时使用的对象键。对象键统一使用小写 ASCII
目录名；OSS 目录只用于管理和审计，权限判断始终以 PostgreSQL 中的对象归属、项目权限和
`oss_files` 记录为准。

后台配置 GET 和 PUT 在数据库读取或配置解密失败时返回 503；PUT 保留原配置，不将故障
误判为“尚未配置”。保存事务串行读取并更新同一配置，省略 Secret / SecurityToken 时继承
当前值，关闭 OSS 也不清空凭据。普通对象读取使用的兼容辅助函数仍在读取失败时返回禁用
默认值，因此无法签名或调用 OSS；这些接口尚不能完整区分配置故障和主动停用。

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
归档失败时保留原对象和数据库引用，后续发布操作可以安全重试。配额按 `uploader_id` 统计
该用户上传的所有活动对象，不能通过改用项目目录绕过用户上传额度。

## 安全和生命周期

业务记录中的 OSS URL 是存储引用，域名和路径本身不授予读取权限。公开图标、头像及背景
只有在对应 `oss_files` 活跃、扫描为 `clean` / `trusted_generated` 且属于允许的栅格图片时
才能签名。文件上传者可以查看自己的图片；公开读取还必须有有效的头像/背景、已发布的
作者、蓝图或公开皮肤绑定。资料项目图标同时核对已批准的修订与文件上传者，避免历史
任意 URL 成为私有文件的签名代理。外部 HTTP(S) 图标仍按原 URL 展示。

模组、整合包和其他资料项目创建/提交修订时，在事务中校验该图标绑定并锁住文件记录。
不合格输入返回 400；校验存储不可用返回 503。当前图标值不会单独授予权限。对缺少可信
绑定、已删除或扫描未通过的历史 OSS 图标，公开读取返回空图标，由界面显示默认图标。
合法私有下载仍由原下载接口先鉴权，再使用通用签名入口。

合法待审图标可以通过 [待审项目预览](project-reviews.md) 与 Mod 修订响应查看。服务端额外核对
目标项目、不可变 revision、snapshot 图标引用及 revision 作者和文件上传者的一致性；
只有该目标的编辑者或合格审核员可以读取，管理员身份不绕过这项绑定证明。

列表先读取并关闭业务查询结果，再批量校验本页图标；100 张不同图标的隔离测试验证只用
一次授权查询，且连接池仅有一条连接时也可完成。该证据不代表生产性能或扫描状态健康。
等待审核期间删除文件仍可能使迟到修订发布后显示默认图标，后台审核与维护任务尚未在
发布事务中全面重校验所有旧快照；不会因此获得私有文件签名。上传扫描、对象实际存在性
和生产 Bucket 策略仍需各自验证。

- 浏览器只能使用后端签发的短时、禁止覆盖的 PUT URL。16 MiB 及以上的文件默认使用
  8 MiB 分片、最多 4 路并发上传；每个分片 URL 独立签名和重试，完成或取消仍必须回到
  后端重新校验对象归属。
- 上传完成接口会根据用户、项目和上传用途重新计算目标前缀，再校验 ObjectKey。
- 文件扩展名只用于可读性；服务端仍校验允许的扩展名、内容类型、大小和 SHA-256。
- 文件包导入生成的对象使用受控并发上传，先登记待处理 artifact，再执行 OSS 写入；
  全部校验与导入成功后才激活对应记录，失败对象通过已有删除 Outbox 补偿。
- Mod、整合包和其他资料项目创建时的图标，以及 Mod 的作者头像，在业务事务前
  沿用独立镜像登记流程，避免外部 I/O 占用业务连接。登记失败沿用删除 Outbox
  补偿；已登记文件保留上传者归属，
  项目绑定仍在事务内重核活跃、扫描和所有权并锁住对应文件，数据库唯一约束
  继续处理资料 ID 的并发冲突。
- 历史对象通过数据库中的完整 ObjectKey 读取，因此旧目录对象不会因目录规范升级而失联。
- 不自动删除未登记的旧对象；清理必须以数据库引用扫描结果为依据。
- Bucket 应配置“清理未完成分片上传”生命周期规则（建议 1 天）。前端失败时会主动请求
  AbortMultipartUpload；生命周期规则用于覆盖浏览器断网、崩溃等无法送达取消请求的情况。
