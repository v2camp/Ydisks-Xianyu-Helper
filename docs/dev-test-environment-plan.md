# 本机开发/测试环境治理方案

> 版本：1.0
> 日期：2026-09-21
> 状态：方案讨论稿
> 关联：`docs/test-separation-plan.md`（测试分离方案）、`docs/agent-evaluation-plan.md`（Agent 评测方案）

## 一、背景与目标

应用已迁移到另一台机器部署，本机从「部署工作区」调整为「开发、测试环境」。

- 本机不再用主号登录、不再承担生产运行职责。
- Docker 中的服务已暂停，方案确定后再整理启用。
- 目标：本机一切运行（开发、测试、评测）都不触达真实主号，不影响账号稳定。

## 二、现状盘点：不依赖主号的测试能力已基本齐备

六阶段重构后，工程测试基建本身就不依赖真实账号：

| 测试层 | 数据源 | 是否需真实账号 |
|---|---|---|
| Go 单测/集成测试 | SQLite 内存库 + httptest 替身 + golden 录制 | 否 |
| 功能测试栈（docker-compose.functional.yml） | `dbseed` 脱敏种子 + `docker_fixture`/`docker_admin` | 否 |
| 浏览器测试（internal/browser） | 本地 Chromium + 本地页面/server（滑块冻结规范同源，如 CDP fallback、webui-e2e 冒烟） | 否 |
| Web UI 冒烟（webui-e2e-test） | 本地 server + 临时 SQLite | 否 |
| 前端测试 | vitest + tsc | 否 |
| 真实平台测试 | 真实闲鱼页面 + 真实账号 | **是（显式 opt-in）** |

关键证据：
- `cmd/dbseed/main.go` 注释明确「不会复制真实 Cookie、买家 ID 或卡密内容」，功能测试栈全链路用脱敏数据。
- 真实平台调用是显式 opt-in：`TEST_XIANYU_LIVE=1` 才运行，见 `internal/xianyu/mtop/account_tasks_live_integration_test.go`。
- 浏览器本地页面测试：`internal/browser/token_captcha_diagnostic_test.go`「避免访问真实平台或使用真实账号」。

## 三、治理原则

1. **主号不登录本机**：本机任何服务、测试、浏览器 profile 都不得使用主号真实 Cookie。
2. **真实平台触达必须显式开关**：所有可能调用真实闲鱼接口的测试，必须以环境变量显式开启（沿用 `TEST_XIANYU_LIVE` 模式）。
3. **回放数据一律脱敏**：主号数据只能以脱敏快照形式存在于本机测试环境（方案见测试分离文档）。
4. **默认门禁零账号**：`make check`、functional、webui-e2e 等默认门禁不得依赖任何真实账号。

## 四、本机环境整理

- 确认 `data/`、`browser_data/` 不保存主号真实 Cookie；如需保留作回放源，走脱敏快照工具后落库。
- Docker 服务恢复时直接复用现有 `docker-compose.functional.yml` 功能测试栈。种子链路为：`sqlite-seed-copy` 读本机 `./data/xianyu_data.db` → `dbseed` 脱敏（不复制真实 Cookie/买家 ID/卡密）→ 写入目标库，全程无需主号。
- `TEST_XIANYU_LIVE` 保持默认关闭；本机所有开发、测试、评测任务在未显式开启该开关时不得触达真实平台。

## 五、配套纪律（写入后续执行）

- 禁止在本机以主号运行 server 或浏览器登录流程。
- 新增任何「可能触达真实平台」的测试入口，必须挂显式开关，并在文档中登记。
- 覆盖率声明中「真实账号/外部平台」例外项，本机一律不执行、只登记。

## 六、与其余两篇方案的关系

本方案是另外两篇的前提：
- 测试分离方案：平台功能测试零账号 + UI 自动化用测试小号/大号，衔接资产为主号脱敏快照。
- Agent 评测方案：评测集来自脱敏快照与真实 IM 分类，零账号可运行。

## 七、落地状态与检查命令

本方案已落地为一条可执行的防护检查脚本：`scripts/guard-local-dev-env.sh`。

### 脚本用途

- 只读检查本机开发/测试环境的三类风险，任一违反即打印告警并返回非零退出码：
  1. `TEST_XIANYU_LIVE` 被意外设置为 `1`（真实闲鱼平台触达开关）；
  2. `DATABASE_URL` 误指向本机 `data/` 目录下的真实主号 SQLite 数据文件（如 `data/xianyu_data.db`）；
  3. `data/` 与 `browser_data/` 目录残留真实凭证特征文件（SQLite 数据、浏览器 Cookie/登录数据库、含 cookie/token 特征的日志）。
- 脚本为 bash 兼容、只读、不依赖 docker；不打印任何 Cookie/Token 明文，凭证相关内容只报「存在/不存在」。

### 检查命令

```bash
bash scripts/guard-local-dev-env.sh        # 期望退出码 0
TEST_XIANYU_LIVE=1 bash scripts/guard-local-dev-env.sh   # 期望告警并返回非零
```

### 审计结论（2026-09-21，主工作区）

- `data/`、`browser_data/` 均不存在，无真实凭证特征文件，真实主号数据不在本机运行链路中。
- `internal/server/data/` 仅含空的 `uploads` 目录，无凭证特征。
- `backup/` 目录保留历史备份（`xianyu-backup.sql`、`app_data.tar.gz`、`browser_data.tar.gz`）：SQL 备份含 cookies/account 表，浏览器备份含持久化 profile（`user_*` 目录）。属 gitignore 离线备份，不作为本机运行数据源；遗留待脱敏快照工具处理或离线清理（衔接方案第四节与测试分离方案）。
