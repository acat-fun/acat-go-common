# acat-go-common

ACAT 后端 Go 公共基础库（Java 渐进迁移 Go 方案的阶段 1 产物）。

> 归属：`lib/backend/acat-go-common`，独立 git 仓库；module path `gitea.acat.fun/acat-fun/acat-go-common`。
> 目标：抽取跨服务稳定的通用能力，业务领域（Entity / Mapper / Controller / 业务枚举）**禁止**进入本库。

## 包结构

| 包 | 职责 | 对应 Java 侧 |
| --- | --- | --- |
| `result` | 统一响应 `Result{code,message,data}` 与分页 `PageData{total,headNodeTotal,pageIndex,pageSize,list}` | `fun.acat.common.result.Result`、`PageData` |
| `apperr` | HTTP 状态码语义错误（未登录 401 / 无权限 403 / 404 / 409 / 503 / 422 / 400 / 500） | `GlobalExceptionHandler`（`lib/backend/acat-svc-common`） |
| `config` | 环境变量 + YAML 配置加载，支持 `${ENV:default}` 占位符 | `bootstrap.yml` / Nacos 配置 |
| `logging` | `log/slog` JSON 日志与 `trace_id` 上下文 | `logback-spring.xml` + Trace 过滤器 |
| `db` | MySQL 连接池（`database/sql` + go-sql-driver） | MyBatis-Plus / HikariCP |
| `redisx` | Redis 客户端（go-redis v9） | `spring.data.redis` |
| `satoken` | **Sa-Token 兼容层**：token 生成、Redis key 拼接、Account-Session 读写、登录态与权限校验 | `sa-token-spring-boot3-starter` + `sa-token-redis-jackson` |
| `middleware` | 认证中间件、Trace 中间件、统一异常/恢复、CORS | `SaInterceptor` + `RequestTimingGlobalFilter` |
| `server` | HTTP 服务装配、健康探针、优雅停机 | Spring Boot Web 容器 |
| `health` | 存活/就绪探针与依赖健康检查 | `actuator/health` |
| `permission` | 领域权限判定框架（`/health` 等白名单 + `@SaCheckPermission` 语义） | `SaInterceptor` + `StpInterfaceImpl` |
| `objectstore` | MinIO/S3 对象存储（SigV4 签名） | MinIO Java SDK |
| `mysqlx` | MySQL 写样板：空值转换（`nullString`/`nullInt`/`nullTime`）、约束冲突识别（`IsIntegrityViolation`）、MP 风格 insert/update 样板 | MyBatis-Plus `MyBaseMapper` + `MyMetaObjectHandler` |
| `mongox` | **MySQL+Mongo 跨存储一致性 outbox**（2026-09-19 遗留事项 2）：`EventStore`（业务事务内入队 `t_read_mongo_outbox`）+ `Replayer`（后台幂等重放，单条失败隔离、10 次上限）+ `DocApplier`（服务注入应用逻辑） | 无 Java 对应（Java 侧无补偿机制，Go 补齐） |

## Sa-Token 兼容边界（重要）

`satoken` 包按 Sa-Token **v1.44.0** 源码复刻下列契约：

- token 名默认 `satoken`，可用 `SaTokenConfig.TokenName` 覆盖（ACAT 管理端实际使用 `acat-admin-token`，取值来自部署配置而非仓库）。
- loginType 默认 `login`，可用 `SaTokenConfig.LoginType` 覆盖。
- Redis key（`StpLogic.splicingKey*`）：
  - token → loginId：`<tokenName>:<loginType>:token:<tokenValue>`
  - Account-Session：`<tokenName>:<loginType>:session:<loginId>`
  - Token-Session：`<tokenName>:<loginType>:token-session:<tokenValue>`
  - 最后活跃时间：`<tokenName>:<loginType>:last-active:<tokenValue>`
- token 值生成遵循 `token-style` 配置（`uuid` / `simple-uuid` / `random-32` / `random-64` / `random-128` / `tik`），默认 `uuid`（32 位无连字符）。
- Session JSON 采用 `sa-token-jackson` 的 Jackson 序列化约定：启用 default typing、`@class` 以属性形式嵌入（`NON_FINAL`），`dataMap` 内的集合带 `java.util.ArrayList` 等类型标记。

**未在真实 Redis 上验证**：本库当前环境无法连到 UAT 的 Java 服务与 Redis，Jackson 多态 `@class` 的完整嵌套形态（尤其 `dataMap` 中 `List<String>` 的具体标记位置）需在 UAT 用 Java 写入、Go 读取做交叉验证。验证方法与判定标准见 `docs/task/2026-09-13-Java迁移阶段0-认证与权限机制.md`；在交叉验证通过前，Go 服务不得直接接管 Admin 认证路由。

## 质量门禁

```bash
go vet ./...
go test -race ./...
```

## 依赖策略

- 公共库只依赖公开模块（MySQL 驱动、go-redis、x/crypto、yaml）。
- 服务侧通过 `replace` 或私有 module 代理引用本库；devops 流水线需为 Go 阶段提供 `GOPROXY` 与私有仓凭据（见根 `docs/注意事项.md`）。
