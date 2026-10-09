#!/bin/sh
set -eu

# script_dir 定位本目录，使脚本从任意 worktree 调用都走同一份功能栈入口。
script_dir="$(cd "$(dirname "$0")" && pwd)"
# compose 统一委托给 compose-functional.sh：它强制 -p 项目名并拒绝在部署根目录执行。
compose="sh $script_dir/compose-functional.sh"

$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SHOW server_version" | grep -q '^17\.'
$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SHOW timezone" | grep -Eq '^UTC$|^Etc/UTC$'
$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='automation_runs' AND column_name IN ('action_cursor','action_started')" | grep -qx '2'
$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT value FROM cookies WHERE id='docker-fixture-account'" | grep -q '^enc:v1:'

postgres_before="$($compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT (SELECT COUNT(*) FROM users WHERE username='docker_fixture') || ':' || (SELECT COUNT(*) FROM cards c JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture') || ':' || (SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture')")"

sqlite_admin_cookies=/tmp/sqlite-persistence-admin.cookies
curl -fsS -c "$sqlite_admin_cookies" -H 'Content-Type: application/json' \
  -d '{"username":"docker_admin","password":"docker_admin_password"}' \
  http://127.0.0.1:28080/login | grep -q '"success":true'
curl -fsS -b "$sqlite_admin_cookies" -H 'Content-Type: application/json' -X PUT \
  -d '{"theme_color":"sqlite-container-recreated"}' \
  http://127.0.0.1:28080/system-settings | grep -q '"success":true'

$compose exec -T postgres psql -U xianyu -d xianyu -c "INSERT INTO system_settings (\"key\",value,description) VALUES ('docker_persistence_probe','postgres-container-recreated','persistence test') ON CONFLICT (\"key\") DO UPDATE SET value=EXCLUDED.value"
$compose exec -T app-postgres touch /app/browser_data/.docker-persistence-probe
$compose exec -T app-sqlite touch /app/browser_data/.docker-persistence-probe

# 删除并重建容器而不是简单 restart；命名卷不会被删除，且这里不重新运行 seed 服务。
$compose stop app-sqlite app-postgres postgres
$compose rm -f app-sqlite app-postgres postgres
$compose up -d --wait postgres
$compose up -d --wait --no-deps app-sqlite app-postgres

postgres_after="$($compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT (SELECT COUNT(*) FROM users WHERE username='docker_fixture') || ':' || (SELECT COUNT(*) FROM cards c JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture') || ':' || (SELECT COUNT(*) FROM orders o JOIN cookies c ON c.id=o.cookie_id JOIN users u ON u.id=c.user_id WHERE u.username='docker_fixture')")"

test "${postgres_before%%:*}" = "1"
test "${postgres_before#*:}" != "0:0"
test "$postgres_after" = "$postgres_before"
$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT value FROM system_settings WHERE \"key\"='docker_persistence_probe'" | grep -qx 'postgres-container-recreated'
$compose exec -T app-postgres test -f /app/browser_data/.docker-persistence-probe
$compose exec -T app-sqlite test -f /app/browser_data/.docker-persistence-probe
$compose exec -T postgres psql -U xianyu -d xianyu -Atc "SELECT value FROM cookies WHERE id='docker-fixture-account'" | grep -q '^enc:v1:'
rm -f "$sqlite_admin_cookies"
curl -fsS -c "$sqlite_admin_cookies" -H 'Content-Type: application/json' \
  -d '{"username":"docker_admin","password":"docker_admin_password"}' \
  http://127.0.0.1:28080/login | grep -q '"success":true'
curl -fsS -b "$sqlite_admin_cookies" http://127.0.0.1:28080/system-settings | grep -q '"theme_color":"sqlite-container-recreated"'

$compose run --rm --no-deps functional-test
$compose exec -T app-postgres rm -f /app/browser_data/.docker-persistence-probe
$compose exec -T app-sqlite rm -f /app/browser_data/.docker-persistence-probe
$compose exec -T postgres psql -U xianyu -d xianyu -c "DELETE FROM system_settings WHERE \"key\"='docker_persistence_probe'"
printf 'Persistent volumes verified: postgres=%s\n' "$postgres_after"
