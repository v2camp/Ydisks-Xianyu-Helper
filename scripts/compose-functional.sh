#!/bin/sh
# compose-functional 是执行 functional compose 栈的唯一入口（见 AGENTS.md 2.2「Docker 项目名安全」）。
#
# 它存在的唯一目的：防止 functional 服务定义落到生产 compose 项目名下。
# 背景（2026-10-10 事故）：部署根目录的 .env 设置了 COMPOSE_PROJECT_NAME=ydisks-xianyu-helper，
# 而 Compose 的项目名优先级是 -p > 环境变量 COMPOSE_PROJECT_NAME（会被 CWD 下的 .env 填充）
# > 文件里的顶层 name: > 目录名。因此在部署根目录直接执行
# `docker compose -f docker-compose.functional.yml ...` 时，functional 文件里的
# `name: ydisks-xianyu-helper-functional` 会被 .env 覆盖，compose 随即认为生产项目规格变了，
# 于是停并删除生产 postgres 容器、换成一个空卷的 functional postgres —— 生产因此下线约 31 分钟。
#
# 用法：scripts/compose-functional.sh <docker compose 参数...>
#   scripts/compose-functional.sh --dry-run run --rm go-vet   # 只打印将要执行的命令
set -eu

# production_project 是生产 compose 项目名；任何情况下都不允许本脚本落到该项目。
production_project="ydisks-xianyu-helper"
# functional_project 是 functional 栈的固定项目名；可用环境变量覆盖，但必须显式且不得等于生产名。
functional_project="${COMPOSE_FUNCTIONAL_PROJECT:-ydisks-xianyu-helper-functional}"

# script_dir、repo_root 分别定位脚本目录与仓库根，保证从任意 worktree 执行都指向同一份 compose 文件。
script_dir="$(cd "$(dirname "$0")" && pwd)"
repo_root="$(cd "$script_dir/.." && pwd)"
# compose_file 是 functional 栈的唯一 compose 定义。
compose_file="$repo_root/docker-compose.functional.yml"

# fail 打印中文原因并终止，避免误执行后才发现项目名不对。
fail() {
  printf '[compose-functional] 拒绝执行：%s\n' "$1" >&2
  exit 1
}

if [ "$functional_project" = "$production_project" ]; then
  fail "项目名不得等于生产项目 $production_project（当前 COMPOSE_FUNCTIONAL_PROJECT=$functional_project）"
fi

# 部署根目录特征：当前目录存在 .env 且其中设置了 COMPOSE_PROJECT_NAME。
# 命中即说明 CWD 会把生产项目名注入 compose，必须先切到 worktree 目录再执行。
if [ -f .env ] && grep -q '^[[:space:]]*COMPOSE_PROJECT_NAME[[:space:]]*=' .env; then
  fail "当前目录是部署工作区（.env 设置了 COMPOSE_PROJECT_NAME）；请在 .worktree/<任务名> 目录下执行"
fi

[ -f "$compose_file" ] || fail "找不到 $compose_file"

# dry_run 只回显最终命令，便于在不触达 Docker 的情况下核对项目名与文件路径。
if [ "${1:-}" = "--dry-run" ]; then
  shift
  printf 'docker compose -p %s -f %s %s\n' "$functional_project" "$compose_file" "$*"
  exit 0
fi

# 执行前打印生产容器快照；执行后应逐条核对状态与创建时间未变，变了即为事故信号。
printf '[compose-functional] project=%s file=%s\n' "$functional_project" "$compose_file"
docker ps --filter "label=com.docker.compose.project=$production_project" \
  --format '[compose-functional] 生产容器: {{.Names}} {{.Status}} 创建于 {{.CreatedAt}}' || true

# exec 保证信号直达 compose 进程，避免调用方「终止任务」只杀掉外层 shell 而 compose 继续执行。
exec docker compose -p "$functional_project" -f "$compose_file" "$@"
