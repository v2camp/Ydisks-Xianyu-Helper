package runtime

// mcp_admin_adapter.go 把管理员应用服务与进程后台任务注册表投影为 internal/mcp 的 AdminPorts。
// 适配器只做透传与时间单位换算：删除用户的运行实例收束与禁止自删由 admin 应用服务负责；
// 后台任务快照由 HTTP 服务在构造完成后提供，MCP 不感知 server 内部结构。

import (
	"context"

	adminapp "xianyu-go/internal/application/admin"
	"xianyu-go/internal/capability"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/server"
)

// mcpAdminPorts 把管理员应用服务与后台任务提供器投影为 MCP 管理员端口。
type mcpAdminPorts struct {
	// service 是管理员用户与统计应用服务。
	service *adminapp.Service
	// backgroundTasks 返回进程后台任务快照；HTTP 服务未构造完成时为空。
	backgroundTasks func() []capability.BackgroundTask
}

// 编译期断言适配器满足 MCP 管理员端口。
var _ capability.AdminPorts = (*mcpAdminPorts)(nil)

// ListUsers 透传用户摘要列表用例。
func (a *mcpAdminPorts) ListUsers(ctx context.Context) ([]adminapp.UserSummary, error) {
	return a.service.ListUsers(ctx)
}

// DeleteUser 透传删除用户用例。
func (a *mcpAdminPorts) DeleteUser(ctx context.Context, currentUserID, targetUserID int64) error {
	return a.service.DeleteUser(ctx, currentUserID, targetUserID)
}

// Stats 透传全局统计用例。
func (a *mcpAdminPorts) Stats(ctx context.Context) (adminapp.Stats, error) {
	return a.service.Stats(ctx)
}

// BackgroundTasks 返回进程后台任务快照；提供器缺失时返回空切片。
func (a *mcpAdminPorts) BackgroundTasks() []capability.BackgroundTask {
	if a.backgroundTasks == nil {
		return nil
	}
	return a.backgroundTasks()
}

// newMCPAdminPorts 构造 MCP 管理员端口；管理员服务缺失时返回 nil。
func newMCPAdminPorts(ports composition.TransportPorts, backgroundTasks func() []capability.BackgroundTask) *mcpAdminPorts {
	if ports.Admin == nil {
		return nil
	}
	return &mcpAdminPorts{service: ports.Admin, backgroundTasks: backgroundTasks}
}

// mcpBackgroundTasksFromSnapshots 把 HTTP 服务的后台任务快照转换为 MCP 传输模型。
// 时间统一转为 Unix 毫秒；零值时间保持为零，由调用方按“未知”语义处理。
func mcpBackgroundTasksFromSnapshots(snapshots []server.BackgroundTaskSnapshot) []capability.BackgroundTask {
	// tasks 是转换后的任务列表。
	tasks := make([]capability.BackgroundTask, 0, len(snapshots))
	// snapshot 是当前待转换的任务快照。
	for _, snapshot := range snapshots {
		// task 是转换中的 MCP 任务视图。
		task := capability.BackgroundTask{
			ID: snapshot.ID, Name: snapshot.Name, State: snapshot.State,
			StartedAtUnixMilli: snapshot.StartedAt.UnixMilli(),
		}
		if snapshot.FinishedAt != nil {
			task.FinishedAtUnixMilli = snapshot.FinishedAt.UnixMilli()
		}
		if snapshot.DeadlineAt != nil {
			task.DeadlineAtUnixMilli = snapshot.DeadlineAt.UnixMilli()
		}
		tasks = append(tasks, task)
	}
	return tasks
}
