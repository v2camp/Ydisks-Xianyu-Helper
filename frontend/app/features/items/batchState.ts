import { STATUS_TONE_STYLES,type StatusTone } from '../../../shared/ui/tokens';

// PublishBatchSummary 是选择最近批量任务时所需的最小状态模型。
export interface PublishBatchSummary {
  // id 是批量任务标识。
  id?: string;
  // status 是批量任务状态。
  status?: string;
}

// selectActivePublishBatch 只选择仍可继续处理或等待 worker 接管的活跃任务。
export const selectActivePublishBatch = <T extends PublishBatchSummary>(batches: T[]): T | undefined =>
  batches.find(
    // 活跃任务筛选器排除已完成历史，避免覆盖新的上传流程。
    batch => batch.status === 'pending' || batch.status === 'running' || batch.status === 'canceling',
  );

// isBatchInProgress 判断批量任务是否仍需要继续轮询。
export const isBatchInProgress = (status?: string): boolean => status === 'pending' || status === 'running' || status === 'canceling';

// BatchPreviewGate 是判断预检是否可以启动任务的最小模型。
export interface BatchPreviewGate {
  // preview_id 是预检任务标识。
  preview_id?: string;
  // valid 是可发布行数。
  valid?: number;
}

// canStartBatch 判断预检结果是否包含可发布商品。
export const canStartBatch = (preview?: BatchPreviewGate | null): boolean =>
  Boolean(preview?.preview_id && Number(preview.valid || 0) > 0);

// BatchRetryGate 是判断批量任务是否可重试的最小模型。
export interface BatchRetryGate {
  // id 是批量任务标识。
  id?: string;
  // retryable 是可重试失败行数。
  retryable?: number;
  // status 是批量任务状态。
  status?: string;
}

// canRetryBatch 判断批量任务是否存在可重试失败行。
export const canRetryBatch = (detail?: BatchRetryGate | null): boolean =>
  Boolean(detail?.id && Number(detail.retryable || 0) > 0 && !isBatchInProgress(detail.status));

// isCurrentBatchRequest 判断轮询响应是否仍属于当前任务请求代次。
export const isCurrentBatchRequest = (requestGeneration: number, currentGeneration: number): boolean =>
  requestGeneration === currentGeneration;

// batchStatusText 将后端批量状态转换为中文展示文本。
export const batchStatusText = (status?: string): string => {
  switch (status) {
    case 'preview': return '待确认';
    case 'pending': return '等待中';
    case 'running': return '发布中';
    case 'canceling': return '正在安全取消';
    case 'success': return '成功';
    case 'failed': return '失败';
    case 'completed': return '已完成';
    case 'partially_failed': return '部分失败';
    case 'canceled': return '已取消';
    default: return status || '-';
  }
};

// batchStatusTone 把后端批量状态归纳为语义色调，供标签和统计卡引用同一套共享状态令牌。
const batchStatusTone = (status?: string): StatusTone => {
  switch (status) {
    // 成功与已完成都是可交付的终态结果。
    case 'success':
    case 'completed':
      return 'ready';
    // 失败与部分失败都需要用户介入处理。
    case 'partially_failed':
    case 'failed':
      return 'danger';
    // 运行中属于进行中的提示状态。
    case 'running':
      return 'info';
    // 已取消没有结论，保持中性以免误读成异常。
    case 'canceled':
      return 'neutral';
    // 预检待确认和等待中都属于尚未开始的提醒状态。
    default:
      return 'warning';
  }
};

// batchStatusClass 返回批量状态对应的标签样式，色值统一由共享状态令牌提供。
export const batchStatusClass = (status?: string): string => STATUS_TONE_STYLES[batchStatusTone(status)];
