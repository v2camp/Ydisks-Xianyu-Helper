# 多数据库实测回归运行手册

本文说明如何在不伪造外部数据库证据的前提下，重复执行 SQLite 和 PostgreSQL
双方言回归。`make test-multidb` 只有在 `TEST_POSTGRES_URL` 存在时才会启动严格门禁；
URL 缺失会在命令入口直接失败。

## CI 实测

`.github/workflows/ci.yml` 的 `multidb` job 使用 GitHub Actions service containers 启动：

- PostgreSQL 17，映射到 runner 的 `127.0.0.1:5432`；
- 服务通过健康检查后，执行 `make test-multidb`。

该 job 的环境变量只包含临时测试凭证，不得替换为生产连接串。测试会为每个数据库目标创建
独立的 `xytest_*` 数据库，迁移和回归结束后删除；测试失败时仍应检查服务日志和残留数据库。

## 本地可重复执行

仓库已有 `docker-compose.functional.yml` 的 PostgreSQL 服务。先启动数据库并等待健康检查：

```bash
docker compose -f docker-compose.functional.yml up -d postgres
```

然后从仓库根目录执行严格的双方言回归：

```bash
TEST_POSTGRES_URL='postgres://xianyu:xianyu_postgres_password@127.0.0.1:15432/xianyu?sslmode=disable&timezone=UTC' \
make test-multidb
```

回归完成后可以停止并清理本地测试服务及其数据卷：

```bash
docker compose -f docker-compose.functional.yml down -v
```

## 证据解释

- `make test-multidb` 的成功输出才代表本次 SQLite、PostgreSQL 测试目标都实际执行；
- 仅运行 `go test ./internal/db` 或只看到 `SQLite` 子测试通过，不得宣称两库实测完成；
- 未提供外部 URL 时，普通 `go test ./internal/db` 可以保留 SQLite 开发反馈，但必须在报告中明确
  标注 PostgreSQL 未执行；
- 外部实例、真实账号、生产凭证和平台网络不属于本地回归证据，必须单独记录环境与结果。
