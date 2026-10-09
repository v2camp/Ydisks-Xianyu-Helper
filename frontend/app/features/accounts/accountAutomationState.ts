import type { AccountDetail,AccountTaskSettings } from './api';
import type { AccountTaskType } from './accountAutomationTypes';

/** 每日定时下架时间的默认值，与后端默认值保持一致。 */
export const ACCOUNT_DELIST_TIME_DEFAULT = '09:00';

/** 根据账号详情创建任务设置初始草稿。 */
export const buildAccountTaskDefaults = (account: AccountDetail): AccountTaskSettings => ({
  account_id: account.id,
  auto_rate_enabled: account.auto_rate_enabled === true,
  rate_content: account.rate_content || '不错的买家，交易愉快',
  auto_polish_enabled: account.auto_polish_enabled === true,
  polish_time: account.polish_time || '03:00',
  auto_delist_enabled: account.auto_delist_enabled === true,
  delist_time: account.delist_time || ACCOUNT_DELIST_TIME_DEFAULT,
  delist_item_ids: account.delist_item_ids ?? [],
  last_rate_scan_at: account.last_rate_scan_at,
  last_polish_date: account.last_polish_date,
  last_polish_at: account.last_polish_at,
  last_delist_date: account.last_delist_date,
  last_delist_at: account.last_delist_at,
});

/** toggleDelistItem 在下架白名单中就地增删商品标识，保持其余顺序不变。 */
export const toggleDelistItem = (itemIDs: string[], itemID: string): string[] => (
  itemIDs.includes(itemID) ? itemIDs.filter(current => current !== itemID) : [...itemIDs, itemID]
);

/** 判断账号任务响应是否仍属于当前编辑账号和请求代次。 */
export const isCurrentAccountTaskRequest = (currentSequence: number, requestSequence: number, signal: AbortSignal): boolean => (
  currentSequence === requestSequence && !signal.aborted
);

/** 统一提取账号任务请求错误文本。 */
export const accountTaskErrorMessage = (error: unknown, fallback: string): string => error instanceof Error ? error.message : fallback;

/** 判断错误是否来自请求主动取消。 */
export const isAccountTaskAbortError = (error: unknown): boolean => error instanceof Error && error.message === '请求已取消';

/** 判断账号任务动作是否允许重复提交。 */
export const canStartAccountTask = (saving: boolean, running: '' | AccountTaskType): boolean => !saving && running === '';
