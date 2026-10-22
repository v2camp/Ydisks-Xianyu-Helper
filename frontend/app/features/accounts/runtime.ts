import { STATUS_TONE_STYLES,type StatusTone } from '../../../shared/ui/tokens';
import type { AccountDetail } from './api';
import type { AccountRuntimeStatus } from './api';
import type { AccountRuntimePresentation } from './types';

// isOlderStatus 判断传入的运行时快照是否早于账号当前已展示的快照。
export const isOlderStatus = (currentUpdatedAt?: string, incomingUpdatedAt?: string): boolean => {
  if (!currentUpdatedAt || !incomingUpdatedAt) return false;
  // currentTime 是账号当前已展示快照的时间戳。
  const currentTime = Date.parse(currentUpdatedAt);
  // incomingTime 是本次轮询返回快照的时间戳。
  const incomingTime = Date.parse(incomingUpdatedAt);
  return Number.isFinite(currentTime) && Number.isFinite(incomingTime) && incomingTime < currentTime;
};

// mergeAccountRuntimeStatuses 将最新运行时快照合并到账号列表，并拒绝过期响应。
export const mergeAccountRuntimeStatuses = (
  accounts: AccountDetail[],
  statuses: Record<string, AccountRuntimeStatus>,
): AccountDetail[] => accounts.map(
  // account 是待合并运行时快照的账号对象。
  account => {
  // status 是服务端按账号 ID 返回的运行时状态。
  const status = statuses[account.id];
  if (!status || isOlderStatus(account.runtime_updated_at, status.updated_at)) return account;
  return {
    ...account,
    runtime_state: status.state,
    runtime_message: status.message || '',
    runtime_connected: status.connected,
    runtime_updated_at: status.updated_at,
  };
  },
);

// RUNTIME_DOT_STYLES 把同一套状态语义映射到头像旁状态点的实心圆点：徽标的「浅底 + 深字」不适合 6px 的点状指示，
// 因此按同族取 500 档实心底色，保证圆点与徽标同义同族；中性态用浅灰，避免已停用账号抢视觉焦点。
const RUNTIME_DOT_STYLES: Record<StatusTone, string> = {
  ready: 'bg-success-500',
  info: 'bg-brand-500',
  warning: 'bg-warning-500',
  danger: 'bg-danger-500',
  neutral: 'bg-gray-300',
};

// runtimePresentation 用同一档语义色派生徽标与状态点，避免两处各写一套色值而漂移。
const runtimePresentation = (label: string, tone: StatusTone): AccountRuntimePresentation => ({
  label,
  badge: STATUS_TONE_STYLES[tone],
  dot: RUNTIME_DOT_STYLES[tone],
});

// accountRuntimePresentation 将后端运行状态转换为列表徽标和状态点样式；具体色值由语义色档决定。
export const accountRuntimePresentation = (account: AccountDetail): AccountRuntimePresentation => {
  if (!account.enabled || account.runtime_state === 'disabled') {
    return runtimePresentation('已停用', 'neutral');
  }
  switch (account.runtime_state) {
    // 在线可正常收发消息，是本模块唯一的「可执行」终态。
    case 'online':
      return runtimePresentation('在线', 'ready');
    // 启动与连接中属于过程态，只需提示「正在进行」，不应读作告警。
    case 'starting':
    case 'connecting':
      return runtimePresentation('连接中', 'info');
    // 重连中意味着链路已断但仍在自愈，需要用户留意。
    case 'reconnecting':
      return runtimePresentation('重连中', 'warning');
    // 登录失效必须重新扫码，属于需要用户处置的终态。
    case 'auth_expired':
      return runtimePresentation('登录已失效', 'danger');
    // 平台风控验证同样需要人工介入，但可通过人脸识别自愈，因此按警示而非错误处理。
    case 'verification_required':
      return runtimePresentation('需要验证', 'warning');
    // 同账号在别处登录导致的状态冲突，需用户处置，与运行异常同档。
    case 'runtime_conflict':
      return runtimePresentation('运行状态冲突', 'danger');
    case 'error':
    case 'stopped':
      return runtimePresentation('运行异常', 'danger');
    // 其余未知状态保持中性，避免把「尚未探明」显示成异常。
    default:
      return runtimePresentation('状态检测中', 'neutral');
  }
};
