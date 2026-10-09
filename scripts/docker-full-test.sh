#!/bin/sh
set -eu

# script_dir 定位本目录，使脚本从任意 worktree 调用都走同一份功能栈入口。
script_dir="$(cd "$(dirname "$0")" && pwd)"
# compose 统一委托给 compose-functional.sh：它强制 -p 项目名并拒绝在部署根目录执行。
compose="sh $script_dir/compose-functional.sh"

$compose up -d --build postgres
$compose build seed-sqlite frontend-test go-vet go-lint go-test browser-integration-test webui-e2e-test
$compose run --rm frontend-test
$compose run --rm go-vet
$compose run --rm go-lint
$compose run --rm go-test
$compose run --rm browser-integration-test
$compose run --rm webui-e2e-test
$compose run --rm dbverify-sqlite
$compose run --rm dbverify-postgres
$compose rm -sf sqlite-seed-copy seed-sqlite seed-postgres
$compose up --build sqlite-seed-copy
$compose up --build seed-sqlite seed-postgres
$compose up -d --build app-sqlite app-postgres
$compose run --rm --no-deps functional-test

./scripts/docker-persistence-test.sh
