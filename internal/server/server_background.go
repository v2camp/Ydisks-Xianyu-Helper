package server

import (
	"context"
)

// WaitForBackground 等待 Server 自有的 HTTP 后台任务退出；应用 worker 由生命周期协调器负责。
func (s *Server) WaitForBackground() {
	if s == nil {
		return
	}
	_ = s.waitForBackgroundContext(context.Background())
}

// closedSignal 封装closedSignal业务协调。
func closedSignal() chan struct{} {
	// done 用于本次流程后续判断的done
	done := make(chan struct{})
	close(done)
	return done
}

// startBackgroundTaskContext 登记并启动带显式 Context 的 Server 后台任务。
// 返回值是可供管理端查询的任务 ID；任务完成、取消或超时后会保留有限历史。
func (s *Server) startBackgroundTaskContext(name string, ctx context.Context, task func()) string {
	return s.startBackgroundTaskResult(name, ctx, func() error {
		if task != nil {
			task()
		}
		return nil
	})
}

// startBackgroundTaskResult 登记并启动可返回错误的 Server 后台任务。
// 任务错误会进入任务注册表；调用方仍需自行处理敏感错误日志和取消语义。
func (s *Server) startBackgroundTaskResult(name string, ctx context.Context, task func() error) string {
	// taskID、complete 记录任务状态并提供一次性收束回调。
	taskID, complete := s.taskRegistryForServer().start(name, ctx)
	s.beginBackgroundTask()
	// #nosec G118 -- 任务由调用方提供的 Server 生命周期控制。
	go func() {
		defer s.finishBackgroundTask()
		// taskErr 保存后台任务函数返回的可观测错误。
		var taskErr error
		defer func() { complete(taskErr) }()
		if task == nil {
			if s.Logger != nil {
				s.Logger.Warn("跳过空后台任务", "task", name)
			}
			return
		}
		taskErr = task()
	}()
	return taskID
}

// beginBackgroundTask 登记一个由 Server 负责等待的后台任务，并刷新零到一任务转换信号。
func (s *Server) beginBackgroundTask() {
	if s == nil {
		return
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.backgroundCount == 0 {
		s.backgroundDone = make(chan struct{})
	}
	s.backgroundCount++
}

// finishBackgroundTask 标记一个后台任务退出，并在计数归零时关闭完成信号。
func (s *Server) finishBackgroundTask() {
	if s == nil {
		return
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.backgroundCount <= 0 {
		return
	}
	s.backgroundCount--
	if s.backgroundCount == 0 && s.backgroundDone != nil {
		close(s.backgroundDone)
	}
}

// beginWorker 为仍需由 Server 等待的通用后台任务提供兼容测试入口；业务 worker 生命周期由应用协调器拥有。
func (s *Server) beginWorker() func() {
	if s == nil {
		return func() {}
	}
	s.beginBackgroundTask()
	return func() {
		s.finishBackgroundTask()
	}
}

// waitForBackgroundContext 等待已登记后台任务退出；超时只结束当前等待，不创建游离等待 goroutine。
func (s *Server) waitForBackgroundContext(ctx context.Context) bool {
	if s == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.backgroundMu.Lock()
	// done 是当前后台任务批次归零时关闭的完成信号。
	done := s.backgroundDone
	if done == nil {
		done = closedSignal()
		s.backgroundDone = done
	}
	s.backgroundMu.Unlock()
	return waitForSignal(ctx, done)
}

// taskRegistryForServer 返回 Server 的后台任务注册表；零值 Server 也会安全惰性初始化。
func (s *Server) taskRegistryForServer() *taskRegistry {
	if s == nil {
		return newTaskRegistry()
	}
	s.taskRegistryMu.Lock()
	defer s.taskRegistryMu.Unlock()
	if s.taskRegistry == nil {
		s.taskRegistry = newTaskRegistry()
	}
	return s.taskRegistry
}

// waitForSignal 在关闭上下文取消或目标信号到达时返回，避免无界阻塞。
func waitForSignal(ctx context.Context, signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	case <-ctx.Done():
		return false
	}
}
