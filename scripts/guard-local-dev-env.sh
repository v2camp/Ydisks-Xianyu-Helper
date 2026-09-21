#!/usr/bin/env bash
# 用途：本机开发/测试环境防护检查（只读），防止误触达真实主号数据。
# 检查项：1) TEST_XIANYU_LIVE 未被意外开启为 "1"；
#         2) DATABASE_URL 未误指向 data/ 目录下的真实主号 SQLite 数据文件；
#         3) data/ 与 browser_data/ 目录未残留真实凭证特征文件（Cookie/登录数据库、含 token 的日志）。
# 行为：全部通过返回 0；任一违反打印告警并返回非零。全程只读，不写文件、不依赖 docker。
# 约定：不打印任何 Cookie/Token 明文，凭证相关内容只报「存在/不存在」。
# 用法：bash scripts/guard-local-dev-env.sh
set -uo pipefail

# 脚本所在目录（绝对路径），用于定位仓库根
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# 仓库根目录：本脚本位于 scripts/ 下，其上一级即仓库根
REPO_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)

# 违规总数，最终退出码由它决定（0 通过，非 0 违规）
violations=0

# 记录一条违规并累计违规数
# 参数 $1：违规原因描述
report_violation() {
    printf '[FAIL] %s\n' "$1" >&2
    violations=$((violations + 1))
}

# ---- 检查一：真实平台触达开关 ----
# TEST_XIANYU_LIVE：真实闲鱼平台集成测试的显式 opt-in 开关，取 "1" 才生效
# （参考 internal/xianyu/mtop/account_tasks_live_integration_test.go）；本机环境禁止误开。
if [ "${TEST_XIANYU_LIVE:-}" = "1" ]; then
    report_violation "TEST_XIANYU_LIVE 被设置为 1：会触发真实闲鱼平台触达，本机开发/测试环境禁止开启"
else
    printf '[OK] TEST_XIANYU_LIVE 未开启\n'
fi

# ---- 检查二：DATABASE_URL 不得指向真实主号 SQLite ----
# 真实主号数据文件固定为 data/ 目录下的 xianyu_data.db；常见误配置形如
# sqlite://./data/xianyu_data.db、file:data/xianyu_data.db 或裸路径 data/xianyu_data.db。
# 违规时只报环境变量名与判定结论，不打印 URL 值本身（避免泄露 URL 中可能含有的口令）。
db_url="${DATABASE_URL:-}"
if [ -n "$db_url" ]; then
    # 归一化为本地文件路径：剥掉 sqlite:// 与 file: 前缀、查询参数与片段
    local_path="$db_url"
    case "$local_path" in
        sqlite://*) local_path="${local_path#sqlite://}" ;;
        file:*) local_path="${local_path#file:}" ;;
    esac
    # 去掉 ? 之后的查询参数与 # 之后的片段
    local_path="${local_path%%\?*}"
    local_path="${local_path%%#*}"
    # 违规特征：路径含 data/ 目录段且以数据库后缀结尾，或直接引用 xianyu_data.db
    case "$local_path" in
        *"/data/"*.db|*"/data/"*.sqlite|*"/data/"*.sqlite3|data/*.db|data/*.sqlite|data/*.sqlite3|*xianyu_data.db*)
            report_violation "DATABASE_URL 指向 data/ 目录下的 SQLite 文件，疑似真实主号数据；请改用内存库或临时测试库（URL 值已脱敏不打印）"
            ;;
        *)
            printf '[OK] DATABASE_URL 未指向 data/ 目录下的 SQLite 主号数据\n'
            ;;
    esac
else
    printf '[OK] DATABASE_URL 未设置\n'
fi

# ---- 检查三：data/ 与 browser_data/ 凭证特征 ----
# 目录不存在视为通过；存在但无凭证特征文件只提示，不判违规。

# data/ 目录：真实主号 SQLite 数据与运行日志的默认落盘位置（gitignore 覆盖）
data_dir="$REPO_ROOT/data"
if [ -d "$data_dir" ]; then
    printf '[INFO] 检测到 data/ 目录，检查凭证特征…\n'
    # 数据库文件计数：存在 *.db/*.sqlite/*.sqlite3 即视为可能保存主号数据
    data_db="$(find "$data_dir" -maxdepth 2 -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \) 2>/dev/null | wc -l | tr -d ' ')"
    if [ "$data_db" -gt 0 ]; then
        report_violation "data/ 目录下存在 ${data_db} 个 SQLite 数据库文件，可能保存真实主号数据"
    fi
    # 若系统装有 sqlite3，进一步只读探测各库是否含 cookies 相关表（只查表名，不读数据）
    if command -v sqlite3 >/dev/null 2>&1 && [ "$data_db" -gt 0 ]; then
        # cookie 表命中数：跨全部数据库累计；只读模式打开，失败即跳过
        cookie_tables="$(find "$data_dir" -maxdepth 2 -type f \( -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3' \) \
            -exec sqlite3 -readonly {} "SELECT count(*) FROM sqlite_master WHERE type='table' AND name LIKE '%cook%';" \; \
            2>/dev/null | awk '{s+=$1} END {print s+0}')"
        if [ "$cookie_tables" -gt 0 ]; then
            report_violation "data/ 下的 SQLite 数据库存在 ${cookie_tables} 个 cookies 相关表，确认保存账号凭证"
        fi
    fi
    # 日志凭证特征：data/ 下 *.log 中含 cookie/token 特征的行数（只计数，不输出内容）
    log_files="$(find "$data_dir" -maxdepth 2 -type f -name '*.log' 2>/dev/null)"
    log_hits=0
    if [ -n "$log_files" ]; then
        while IFS= read -r log_file; do
            # 单条日志的凭证特征行数；无匹配时 grep 输出 0 并返回 1，|| true 兜底
            hits=$(grep -c -i -E '_tb_token_|cook[ie]|token=' "$log_file" 2>/dev/null || true)
            hits=${hits:-0}
            log_hits=$((log_hits + hits))
        done <<< "$log_files"
    fi
    if [ "$log_hits" -gt 0 ]; then
        report_violation "data/ 下的日志文件含 ${log_hits} 行凭证特征文本（cookie/token），可能存在真实凭证泄漏"
    fi
    if [ "$data_db" -eq 0 ] && [ "$log_hits" -eq 0 ]; then
        printf '[OK] data/ 目录未发现凭证特征文件\n'
    fi
else
    printf '[OK] data/ 目录不存在\n'
fi

# browser_data/ 目录：浏览器持久化 profile 的落盘位置（gitignore 覆盖）
browser_dir="$REPO_ROOT/browser_data"
if [ -d "$browser_dir" ]; then
    printf '[INFO] 检测到 browser_data/ 目录，检查凭证特征…\n'
    # 浏览器凭证库文件计数：Chromium 持久化 profile 的 Cookie 与登录数据数据库
    cred_files="$(find "$browser_dir" -type f \( -name 'Cookies' -o -name 'Login Data' \) 2>/dev/null | wc -l | tr -d ' ')"
    if [ "$cred_files" -gt 0 ]; then
        report_violation "browser_data/ 下存在 ${cred_files} 个浏览器 Cookie/登录数据文件，可能含真实主号登录态"
    fi
    # 用户 profile 目录计数：仅提示归属风险，不单独判违规
    profiles="$(find "$browser_dir" -maxdepth 1 -type d -name 'user_*' 2>/dev/null | wc -l | tr -d ' ')"
    if [ "$profiles" -gt 0 ]; then
        printf '[WARN] browser_data/ 下存在 %s 个用户 profile 目录，请确认其不含主号登录态\n' "$profiles"
    fi
    if [ "$cred_files" -eq 0 ] && [ "$profiles" -eq 0 ]; then
        printf '[OK] browser_data/ 目录未发现凭证特征文件\n'
    fi
else
    printf '[OK] browser_data/ 目录不存在\n'
fi

# ---- 汇总退出 ----
# 任一违规即返回非零，阻止本机开发/测试链路继续
if [ "$violations" -gt 0 ]; then
    printf '[FAIL] 环境防护检查未通过，共 %s 项违规；请先整改后再启动本机开发/测试链路\n' "$violations" >&2
    exit 1
fi
printf '[OK] 环境防护检查全部通过：本机未配置触达真实主号数据的开关与数据源\n'
exit 0
