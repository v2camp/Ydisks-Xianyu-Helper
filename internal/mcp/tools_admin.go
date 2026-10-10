// tools_admin.go 注册管理员全局域工具：全局统计、用户列表、删除用户与进程后台任务总览。
//
// 安全与语义红线：
//   - 全局视图同样脱敏：用户摘要不含密码与凭证字段，跨用户账号枚举沿用账号域既有的
//     account_list_all（Task 4）与通知域的 notification_uncertain_list_all（Task 10）；
//   - 删除用户是不可逆高危操作，必须显式 confirm，并完全复用 AdminService（运行实例
//     收束 + 禁止删除当前管理员自身）；
//   - 后台任务总览只返回任务标识、名称、状态与时间，不含任务参数与错误正文。

package mcp

import (
	"context"

	"xianyu-go/internal/capability"
)

// adminUserDTO 是管理员用户列表的非敏感视图；不含密码哈希与任何凭证。
type adminUserDTO struct {
	// UserID 是用户标识。
	UserID int64 `json:"user_id"`
	// Username 是登录名。
	Username string `json:"username"`
	// Email 是联系邮箱。
	Email string `json:"email,omitempty"`
	// IsActive 表示用户是否启用。
	IsActive bool `json:"is_active"`
	// IsAdmin 表示用户是否拥有管理员权限。
	IsAdmin bool `json:"is_admin"`
	// AccountCount 是用户拥有的账号数量。
	AccountCount int `json:"account_count"`
	// CreatedAt 是用户创建时间文本。
	CreatedAt string `json:"created_at,omitempty"`
}

// adminUserListResult 是管理员用户列表返回。
type adminUserListResult struct {
	// Total 是用户数量。
	Total int `json:"total"`
	// Users 是用户摘要列表。
	Users []adminUserDTO `json:"users"`
}

// adminStatsDTO 是管理员全局统计视图。
type adminStatsDTO struct {
	// TotalUsers 是用户总数。
	TotalUsers int64 `json:"total_users"`
	// TotalAccounts 是账号总数。
	TotalAccounts int64 `json:"total_accounts"`
	// ActiveAccounts 是启用账号总数。
	ActiveAccounts int64 `json:"active_accounts"`
	// TotalCardGroups 是卡券组总数。
	TotalCardGroups int64 `json:"total_card_groups"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是未删除订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// adminUserDeleteResult 是删除用户的确认返回。
type adminUserDeleteResult struct {
	// UserID 是被删除的用户标识。
	UserID int64 `json:"user_id"`
	// Deleted 固定为 true，表示删除已在服务端生效。
	Deleted bool `json:"deleted"`
}

// adminBackgroundTaskDTO 是进程后台任务的非敏感视图。
type adminBackgroundTaskDTO struct {
	// TaskID 是进程内任务标识。
	TaskID string `json:"task_id"`
	// Name 是任务业务名称。
	Name string `json:"name"`
	// State 是任务生命周期状态。
	State string `json:"state"`
	// StartedAt 是任务开始执行的 Unix 毫秒时间戳。
	StartedAt int64 `json:"started_at"`
	// FinishedAt 是任务结束的 Unix 毫秒时间戳；仍运行时为零。
	FinishedAt int64 `json:"finished_at,omitempty"`
	// DeadlineAt 是任务截止时间的 Unix 毫秒时间戳；无截止时间时为零。
	DeadlineAt int64 `json:"deadline_at,omitempty"`
}

// adminBackgroundTaskListResult 是进程后台任务总览返回。
type adminBackgroundTaskListResult struct {
	// Total 是返回的任务数量。
	Total int `json:"total"`
	// Running 是其中仍在运行的任务数量。
	Running int `json:"running"`
	// Tasks 是任务快照列表。
	Tasks []adminBackgroundTaskDTO `json:"tasks"`
	// Note 提示任务总览只覆盖当前进程。
	Note string `json:"note"`
}

// RegisterAdminTools 注册管理员全局域全部工具。
func (e *Endpoint) RegisterAdminTools(p capability.AdminPorts) {
	if p == nil {
		return
	}
	e.registerAdminReadTools(p)
	e.registerAdminMutationTools(p)
}

// registerAdminReadTools 注册全局统计、用户列表与后台任务总览工具。
func (e *Endpoint) registerAdminReadTools(p capability.AdminPorts) {
	e.RegisterTools(
		ToolDef{
			Name:        "admin_stats",
			Description: "读取管理员全局统计：用户数、账号数与启用数、卡券组数、关键词规则数、订单数。只读。",
			Handler: func(ctx context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				// stats、statErr 是全局聚合计数。
				stats, statErr := p.Stats(ctx)
				if statErr != nil {
					return nil, statErr
				}
				return adminStatsDTO{
					TotalUsers: stats.TotalUsers, TotalAccounts: stats.TotalCookies,
					ActiveAccounts: stats.ActiveCookies, TotalCardGroups: stats.TotalCards,
					TotalKeywords: stats.TotalKeywords, TotalOrders: stats.TotalOrders,
				}, nil
			},
		},
		ToolDef{
			Name: "admin_user_list",
			Description: "列出全部用户摘要（用户名、邮箱、启用与管理员标记、账号数量）。" +
				"只读，不返回密码、Cookie 或令牌字段。",
			Handler: func(ctx context.Context, _ *CallIdentity, _ Arguments) (any, error) {
				// rows、listErr 是用户摘要列表。
				rows, listErr := p.ListUsers(ctx)
				if listErr != nil {
					return nil, listErr
				}
				// users 是用户视图列表。
				users := make([]adminUserDTO, 0, len(rows))
				// row 是当前待映射的用户摘要。
				for _, row := range rows {
					users = append(users, adminUserDTO{
						UserID: row.ID, Username: row.Username, Email: row.Email,
						IsActive: row.IsActive, IsAdmin: row.IsAdmin,
						AccountCount: row.CookieCount, CreatedAt: row.CreatedAt,
					})
				}
				return adminUserListResult{Total: len(users), Users: users}, nil
			},
		},
		ToolDef{
			Name: "admin_background_tasks",
			Description: "查看当前进程的后台任务总览（任务名、状态与起止时间），用于排查卡住或失败的运维任务。" +
				"只覆盖当前进程，不含任务参数与错误正文。只读。",
			Args: []ArgSpec{{Name: "limit", Type: ArgInteger, Description: "最多返回条数，默认 50，最大 200。"}},
			Handler: func(ctx context.Context, _ *CallIdentity, args Arguments) (any, error) {
				_ = ctx
				// limit 是归一后的返回条数上限。
				limit := clampLimit(args.OptionalInt("limit", 50), 50, maxPageSize)
				// snapshots 是进程后台任务快照。
				snapshots := p.BackgroundTasks()
				// tasks 是任务视图列表。
				tasks := make([]adminBackgroundTaskDTO, 0, len(snapshots))
				// running 是仍在运行的任务数量。
				running := 0
				// snapshot 是当前待映射的任务快照。
				for _, snapshot := range snapshots {
					if len(tasks) >= limit {
						break
					}
					if snapshot.State == "running" {
						running++
					}
					tasks = append(tasks, adminBackgroundTaskDTO{
						TaskID: snapshot.ID, Name: snapshot.Name, State: snapshot.State,
						StartedAt: snapshot.StartedAtUnixMilli, FinishedAt: snapshot.FinishedAtUnixMilli,
						DeadlineAt: snapshot.DeadlineAtUnixMilli,
					})
				}
				return adminBackgroundTaskListResult{
					Total: len(tasks), Running: running, Tasks: tasks,
					Note: "该总览只覆盖当前进程；服务重启后历史任务不再可见。",
				}, nil
			},
		},
	)
}

// registerAdminMutationTools 注册删除用户工具。
func (e *Endpoint) registerAdminMutationTools(p capability.AdminPorts) {
	e.RegisterTools(
		ToolDef{
			Name: "admin_user_delete",
			Description: "删除指定用户及其关联资源（不可逆，会先停止该用户全部账号运行实例）。" +
				"不能删除当前管理员自身；运行实例停止失败时会中止删除。必须显式 confirm=true。",
			Destructive: true,
			Args:        []ArgSpec{{Name: "user_id", Type: ArgInteger, Required: true, Description: "要删除的用户标识。"}},
			Handler: func(ctx context.Context, identity *CallIdentity, args Arguments) (any, error) {
				// id 是目标用户标识。
				id, err := args.Int("user_id")
				if err != nil {
					return nil, err
				}
				// deleteErr 是删除用例返回的错误；自删拒绝与运行收束失败由应用服务给出。
				if deleteErr := p.DeleteUser(ctx, identity.UserID, int64(id)); deleteErr != nil {
					return nil, deleteErr
				}
				return adminUserDeleteResult{UserID: int64(id), Deleted: true}, nil
			},
		},
	)
}
