package db

import (
	"strings"
	"testing"
)

// --- db.go: parseDBURL ---

// TestParseDBURL 覆盖各 scheme 与向后兼容路径。
func TestParseDBURL(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name     string
		url      string
		driver   driverName
		dialect  Dialect
		dsnHas   string // dsn 应包含的子串
		wantErr  bool
		errMatch string
	}{
		{name: "sqlite path (no scheme)", url: "/tmp/x.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:/tmp/x.db"},
		{name: "sqlite scheme", url: "sqlite://rel/path.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:rel/path.db"},
		{name: "sqlite3 scheme", url: "sqlite3://x.db", driver: driverSQLite, dialect: DialectSQLite, dsnHas: "file:x.db"},
		{name: "postgres url scheme", url: "postgres://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgres://u:p@h:5432/db"},
		{name: "postgres url without user", url: "postgres://localhost:5432/db?sslmode=disable", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgres://localhost:5432/db?sslmode=disable"},
		{name: "pgx alias", url: "pgx://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "pgx://u:p@h:5432/db"},
		{name: "postgres kv dsn", url: "postgres://host=localhost port=5432", driver: driverPgx, dialect: DialectPostgres, dsnHas: "host=localhost"},
		{name: "postgresql alias", url: "postgresql://u:p@h:5432/db", driver: driverPgx, dialect: DialectPostgres, dsnHas: "postgresql://u:p@h:5432/db"},
		{name: "empty url", url: "", wantErr: true, errMatch: "为空"},
		{name: "whitespace url", url: "   ", wantErr: true, errMatch: "为空"},
		{name: "unknown scheme", url: "redis://h:6379", wantErr: true, errMatch: "不支持"},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// driver、dialect、dsn、err 用于本次流程后续判断的driver、dialect、dsn、err
			driver, dialect, dsn, err := parseDBURL(c.url)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望报错, got driver=%s dsn=%s", driver, dsn)
				}
				if c.errMatch != "" && !strings.Contains(err.Error(), c.errMatch) {
					t.Fatalf("错误信息 %q 不含 %q", err.Error(), c.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDBURL(%q): %v", c.url, err)
			}
			if driver != c.driver {
				t.Errorf("driver=%s want %s", driver, c.driver)
			}
			if dialect != c.dialect {
				t.Errorf("dialect=%s want %s", dialect, c.dialect)
			}
			if c.dsnHas != "" && !strings.Contains(dsn, c.dsnHas) {
				t.Errorf("dsn=%q 不含 %q", dsn, c.dsnHas)
			}
		})
	}
}

// TestParseDBURLForcesUTCSessionTimezone 验证 PostgreSQL 由结构化连接配置注入 UTC。
func TestParseDBURLForcesUTCSessionTimezone(t *testing.T) {
	// postgresDSNValue 保存 PostgreSQL URL，解析阶段保留原始 DSN，避免破坏带引号或编码的认证参数。
	_, _, postgresDSNValue, err := parseDBURL("postgres://u:p@h:5432/db?sslmode=disable&timezone=Asia%2FShanghai")
	if err != nil || !strings.Contains(postgresDSNValue, "timezone=Asia%2FShanghai") {
		t.Fatalf("PostgreSQL DSN 被错误改写: dsn=%q err=%v", postgresDSNValue, err)
	}
	// config、configErr 保存 pgx 结构化解析结果及错误。
	config, configErr := parsePgxConfig(`postgres://u:p%20word@h:5432/db?timezone=Asia%2FShanghai`)
	if configErr != nil || config.RuntimeParams["timezone"] != "UTC" || config.Password != "p word" {
		t.Fatalf("PostgreSQL 结构化会话配置错误: config=%+v err=%v", config, configErr)
	}
}

// --- pgx_compat.go: rewriteQuestionPlaceholders ---

// TestRewriteQuestionPlaceholders ? → $N，跳过引号内字面量。
func TestRewriteQuestionPlaceholders(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no placeholder", in: "SELECT 1", want: "SELECT 1"},
		{name: "single", in: "SELECT * FROM t WHERE id=?", want: "SELECT * FROM t WHERE id=$1"},
		{name: "multi", in: "INSERT INTO t (a,b,c) VALUES (?,?,?)", want: "INSERT INTO t (a,b,c) VALUES ($1,$2,$3)"},
		{name: "single-quoted string with ?", in: "SELECT '?' AS q WHERE id=?", want: "SELECT '?' AS q WHERE id=$1"},
		{name: "escaped single quote", in: "SELECT 'it''s a ?' WHERE id=?", want: "SELECT 'it''s a ?' WHERE id=$1"},
		{name: "double-quoted identifier with ?", in: `SELECT "col?" FROM t WHERE id=?`, want: `SELECT "col?" FROM t WHERE id=$1`},
		{name: "empty", in: "", want: ""},
	}
	// c 表示当前遍历过程中的c
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// got 用于本次流程后续判断的got
			got := rewriteQuestionPlaceholders(c.in)
			if got != c.want {
				t.Errorf("rewriteQuestionPlaceholders(%q)=%q want %q", c.in, got, c.want)
			}
		})
	}
}
