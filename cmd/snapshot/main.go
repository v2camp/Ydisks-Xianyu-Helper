// snapshot 从真实主号 SQLite 生成测试环境可用的脱敏快照 SQLite。
//
// 与 dbseed 同一安全纪律：源库全程只读，Cookie/Token、平台账号标识、买家卖家
// 身份与卡密内容全部替换为 fixture 值，快照账号一律停用，日志不打印真实凭证。
// 用法示例：
//
//	./snapshot -source /path/xianyu_data.db -target /tmp/snapshot.db
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
)

// snapshotOptions 配置快照生成过程中的 fixture 登录账号。
type snapshotOptions struct {
	Username string // Username 是快照库新建的登录用户名，真实用户一律不复制。
	Password string // Password 是快照库新建的登录密码，用于本地测试登录。
}

// main 装配命令行参数并编排脱敏快照流程。
func main() {
	// sourcePath 是真实主号 SQLite 文件路径，全程只读打开。
	sourcePath := flag.String("source", "data/xianyu_data.db", "真实主号 SQLite 文件路径")
	// targetPath 是脱敏快照 SQLite 输出路径。
	targetPath := flag.String("target", "", "脱敏快照 SQLite 输出路径")
	// force 允许覆盖已存在的目标文件，默认拒绝避免混入旧数据。
	force := flag.Bool("force", false, "允许覆盖已存在的目标文件")
	// username 是快照库新建的登录用户名。
	username := flag.String("user", "snapshot_user", "快照库登录用户名")
	// password 是快照库新建的登录密码。
	password := flag.String("password", "snapshot_password", "快照库登录密码")
	flag.Parse()
	if strings.TrimSpace(*targetPath) == "" {
		fmt.Fprintln(os.Stderr, "必须提供 -target 脱敏快照输出路径")
		os.Exit(2)
	}

	// ctx 与 cancel 限定快照全过程的总超时，防止源库挂起或迁移卡死。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// source 是只读打开的真实主号数据库连接，快照绝不写入源库。
	source, err := sql.Open("sqlite", "file:"+*sourcePath+"?mode=ro&_pragma=busy_timeout(10000)")
	if err != nil {
		fatalf("打开源 SQLite: %v", err)
	}
	defer source.Close()
	// err 表示源库连通性检查结果，失败时无法读取主号数据。
	if err := source.PingContext(ctx); err != nil {
		fatalf("连接源 SQLite: %v", err)
	}

	// 目标文件必须不存在，或显式 -force 覆盖，避免残留旧快照污染新快照。
	if _, statErr := os.Stat(*targetPath); statErr == nil {
		if !*force {
			fatalf("目标文件已存在：%s（如需覆盖请加 -force）", *targetPath)
		}
		// err 表示删除旧目标文件的结果，失败时终止避免残留旧数据。
		if err := os.Remove(*targetPath); err != nil {
			fatalf("删除旧目标文件: %v", err)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		fatalf("检查目标文件: %v", statErr)
	}

	// target 是执行全部迁移后的全新快照数据库，schema 与当前应用版本保持一致。
	target, _, err := db.Open(ctx, *targetPath)
	if err != nil {
		fatalf("创建目标快照数据库: %v", err)
	}
	defer target.Close()

	// result 保存各业务表复制行数，供完成摘要输出。
	result, err := snapshotFromSQLite(ctx, source, target, snapshotOptions{
		Username: strings.TrimSpace(*username),
		Password: *password,
	})
	if err != nil {
		fatalf("生成脱敏快照: %v", err)
	}
	fmt.Printf("脱敏快照完成：cookies=%d items=%d orders=%d cards=%d sessions=%d messages=%d\n",
		result.Cookies, result.Items, result.Orders, result.Cards, result.Sessions, result.Messages)
}

// fatalf 输出错误脱敏后的失败信息并退出；错误参数经 logsafe 清理，防止连接信息泄漏。
func fatalf(format string, args ...any) {
	// safeArgs 保存经过错误脱敏的命令行错误参数。
	safeArgs := append([]any(nil), args...)
	// index 表示当前格式化参数下标；value 表示待检查的原始参数。
	for index, value := range safeArgs {
		// errValue 表示当前参数是否为错误对象，是则替换为脱敏文本。
		if errValue, ok := value.(error); ok {
			safeArgs[index] = logsafe.Error(errValue)
		}
	}
	fmt.Fprintf(os.Stderr, format+"\n", safeArgs...)
	os.Exit(1)
}
