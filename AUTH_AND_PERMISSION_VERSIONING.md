# 认证与权限版本

## 四种版本

| 版本 | 权威存储 | 变化来源 | Session 影响 |
| --- | --- | --- | --- |
| `auth_version` | `users.auth_version` | 密码修改/重置、封禁、停用、强制退出等安全事件 | 撤销旧 Session |
| `permission_version` | `users.permission_version` | 用户角色、直接权限、编辑员 assignment、个人作者认领的生效/撤销 | Session 有效，用户权限缓存换 Key |
| `rbac_version` | `runtime_versions('rbac')` | 权限节点、角色模板、角色权限、优先级和拒绝规则变化 | 全站权限缓存换 Key |
| `project_acl` | `runtime_versions('project_acl')` | 团队成员、项目作者/团队关系和关系是否授予权限的变化 | 全站项目 ACL 缓存换 Key |

待审核或驳回且从未生效的作者认领/编辑员申请不会无意义增加用户权限版本。创建、提交或导入项目不改变任何权限版本。

## Session 缓存

Session Key 使用原始 Session ID 的不可逆 SHA-256 指纹：

```text
session:{fingerprint}
```

缓存只保存 Session ID 对应用户、公开 ID、`auth_version`、真实到期时间和结构版本。每次仍核对共享用户认证版本指针；缓存损坏会删除缓存并回源 PostgreSQL，不会当作已认证。

## RBAC 缓存

实际 Key 为：

```text
authz:v{rbacVersion}:acl{projectACLVersion}:user:{userId}:p{permissionVersion}
```

版本指针分别为 `versions:rbac`、`versions:project-acl` 和 `authz:user-version:{userId}`。旧版本 Key 自然过期，不使用 `SCAN + DEL`。权限解析合并普通角色、管理员手工直接权限，以及 `effective_project_access` 派生的编辑员/开发者变量角色。

## 变化语义

- 编辑员通过/撤销、作者认领通过/撤销、管理员手工授予/撤销：递增相关用户 `permission_version`。
- 团队成员或项目作者/团队关系变化可能影响多人：递增 `project_acl`，所有实例下一次请求使用新 Key。
- 角色模板或角色权限定义变化：递增 `rbac_version`。
- 只有安全凭据或账号安全状态变化才递增 `auth_version`。

权限变化事务提交后，应用把数据库权威版本写入 Redis 共享指针。Redis 不可用时版本和权限回源 PostgreSQL；无法确认高风险权限时失败关闭。

权限节点的创建或更新与 `permission_audit_logs` 在同一事务中提交。权限组、用户权限等事务内变更也显式检查审计写入；审计编码或存储失败返回 500 并回滚业务变更，不留下没有对应审计的授权事实。事务已成功提交后，版本指针刷新失败仍返回既有 503 和 `committed=true`，不能把该响应当作事务已经回滚。

## 前端同步

响应暴露 `X-MCMods-Permission-Version` 与 `X-MCMods-RBAC-Version`。统一请求层发现版本变化后刷新 `/api/v1/auth/me`，无需重新登录。

可选认证区分“没有 Session”和“携带了失效 Session”。后者即使返回公开内容，也同时发送：

```text
X-MCMods-Auth-State: invalid
```

前端统一清除内存和本地认证状态；必须登录的接口仍返回 401。

## 启动种子与后台管理

默认角色及其初始允许权限在同一事务中创建。角色已存在后，其名称、状态和允许权限由管理员维护；重启不会恢复已撤销的默认授权，也不会给已有角色自动追加未来新增的种子授权。如需调整，使用现有后台权限管理入口。

`banned` 的内置通配拒绝仍在每次启动时保持为拒绝，保留封禁失败关闭边界。默认用户仍只在确认为首次空库初始化时创建，不重置已有密码或用户授权。
