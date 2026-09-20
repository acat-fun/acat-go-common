# acat-go-common

ACAT 后端 Go 公共基础库。

> 归属：`lib/backend/acat-go-common`，独立 git 仓库；module path `47.108.230.93/acat-fun/acat-go-common`。
> 目标：抽取跨服务稳定的通用能力，业务领域（Entity / Mapper / Controller / 业务枚举）**禁止**进入本库。

## 包结构

| 包 | 职责 | 备注 |
| --- | --- | --- |
| `result` | 统一响应 `Result{code,message,data}` 与分页 `PageData{total,headNodeTotal,pageIndex,pageSize,list}` | 平台统一响应信封 |
| `apperr` | HTTP 状态码语义错误（未登录 401 / 无权限 403 / 404 / 409 / 503 / 422 / 400 / 500） | 异常兜底语义 |
| `config` | 环境变量 + YAML 配置加载，支持 `${ENV:default}` 占位符 | — |
| `logging` | `log/slog` JSON 日志与 `trace_id` 上下文 | — |
| `db` | MySQL 连接池（`database/sql` + go-sql-driver） | — |
| `redisx` | Redis 客户端（go-redis v9） | — |
| `satoken` | **Sa-Token 兼容层**：token 生成、Redis key 拼接、Account-Session 读写、登录态与权限校验 | 语义对齐 Sa-Token starter + jackson 序列化约定 |
| `middleware` | 认证中间件、Trace 中间件、统一异常/恢复、CORS | 会话拦截 + 请求计时语义 |
| `server` | HTTP 服务装配、健康探针、优雅停机 | — |
| `health` | 存活/就绪探针与依赖健康检查 | — |
| `permission` | 领域权限判定框架（`/health` 等白名单 + 注解式权限校验语义） | 权限注解 + 权限数据源语义 |
| `objectstore` | MinIO/S3 对象存储（SigV4 签名） | — |
| `mysqlx` | MySQL 写样板：空值转换（`nullString`/`nullInt`/`nullTime`）、约束冲突识别（`IsIntegrityViolation`）、字段级 insert/update 样板 | 含审计字段自动填充语义 |
| `mongox` | **MySQL+Mongo 跨存储一致性 outbox**（2026-09-19 遗留事项 2）：`EventStore`（业务事务内入队 `t_read_mongo_outbox`）+ `Replayer`（后台幂等重放，单条失败隔离、10 次上限）+ `DocApplier`（服务注入应用逻辑） | 跨存储补偿机制，无对应 starter |

## Sa-Token 兼容边界（重要）

`satoken` 包与既有登录服务的会话契约对齐（Sa-Token v1.44.0 语义）：

- token 名默认 `satoken`，可用 `SaTokenConfig.TokenName` 覆盖（ACAT 管理端实际使用 `acat-admin-token`，取值来自部署配置而非仓库）。
- loginType 默认 `login`，可用 `SaTokenConfig.LoginType` 覆盖。
- Redis key（`StpLogic.splicingKey*`）：
  - token → loginId：`<tokenName>:<loginType>:token:<tokenValue>`
  - Account-Session：`<tokenName>:<loginType>:session:<loginId>`
  - Token-Session：`<tokenName>:<loginType>:token-session:<tokenValue>`
  - 最后活跃时间：`<tokenName>:<loginType>:last-active:<tokenValue>`
- token 值生成遵循 `token-style` 配置（`uuid` / `simple-uuid` / `random-32` / `random-64` / `random-128` / `tik`），默认 `uuid`（32 位无连字符）。
- Session JSON 采用 `sa-token-jackson` 的 Jackson 序列化约定：启用 default typing、`@class` 以属性形式嵌入（`NON_FINAL`），`dataMap` 内的集合带 `java.util.ArrayList` 等类型标记。

**未在真实 Redis 上验证**：当前环境无法连到既有登录服务与共享 Redis，Jackson 多态 `@class` 的完整嵌套形态（尤其 `dataMap` 中 `List<String>` 的具体标记位置）需与登录服务会话交叉验证。验证方法与判定标准见根任务文档；在交叉验证通过前，Go 服务不得直接接管 Admin 认证路由。

## 质量门禁

```bash
go vet ./...
go test -race ./...
```

## 依赖策略

- 公共库只依赖公开模块（MySQL 驱动、go-redis、x/crypto、yaml）。
- 服务侧以**版本化依赖**引用本库（`go.mod` 中 pin 到具体提交），**不再使用 `replace`**；`vendor/` 与 `go.sum` 不提交（项目规范 2026-09-19）。
- 解析路径：Gitea `47.108.230.93` 只有 HTTP → `GOINSECURE` 接受 `?go-get=1` 元数据 → `git url.insteadOf` 把 `http://47.108.230.93/` 改写为 `ssh://git@47.108.230.93/`（实际传输走 SSH）。**不要用 `git.acat.fun` 作 module path**（该域名无 HTTPS，Go 元数据探测必然失败）。
- devops 流水线由 Runner 注入 git 凭据（容器内 `/gitcred`，`GIT_SSH_COMMAND` + `GIT_CONFIG_GLOBAL`）与 `GOPROXY=https://goproxy.cn,direct`；本机一次性配置见工作区 `docs/setup-go-env.sh`。
- 本库变更后：各服务执行 `go mod tidy` 并按需更新 pin（不再需要 `go mod vendor`）。
