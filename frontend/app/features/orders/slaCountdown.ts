// slaCountdown 提供发货 SLA 倒计时的纯展示计算；不依赖 React 与网络。

/** 发货倒计时单元格的展示档位。 */
export type SlaCountdownTone = 'muted' | 'normal' | 'warning' | 'danger';

/** 发货倒计时单元格的展示状态。 */
export interface SlaCountdownView {
  /** text 是主文案；未启用时为破折号。 */
  text: string;
  /** tone 决定文案颜色档位。 */
  tone: SlaCountdownTone;
  /** badge 是附加警示标签；无警示时省略。 */
  badge?: '将超时' | '已超时';
}

/** formatSlaDuration 将非负毫秒时长格式化为「X 小时 Y 分」或「X 分 Y 秒」。 */
export const formatSlaDuration = (ms: number): string => {
  // totalSeconds 是向下取整后的整秒时长。
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  // hours 是整小时数。
  const hours = Math.floor(totalSeconds / 3600);
  // minutes 是不足一小时部分的整分钟数。
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  // seconds 是不足一分钟的整秒数。
  const seconds = totalSeconds % 60;
  if (ms >= 3_600_000) return `${hours} 小时 ${minutes} 分`;
  return `${minutes} 分 ${seconds} 秒`;
};

/** slaCountdownView 计算订单在给定时刻的发货倒计时展示状态。 */
export const slaCountdownView = (
  /** deadline 是 sla_deadline 截止时刻 ISO 文本，空值表示未启用。 */
  deadline: string | null | undefined,
  /** slaMinutes 是生效的 SLA 分钟数，用于计算剩余 20% 预警阈值。 */
  slaMinutes: number | undefined,
  /** nowMs 是本地计算时刻的 Unix 毫秒。 */
  nowMs: number,
): SlaCountdownView => {
  if (!deadline) return { text: '—', tone: 'muted' };
  // deadlineMs 是解析后的截止时刻 Unix 毫秒；解析失败按未启用展示。
  const deadlineMs = Date.parse(deadline);
  if (!Number.isFinite(deadlineMs)) return { text: '—', tone: 'muted' };
  // remainingMs 是剩余毫秒数，负值表示已超时。
  const remainingMs = deadlineMs - nowMs;
  if (remainingMs <= 0) {
    return { text: `已超时 ${formatSlaDuration(-remainingMs)}`, tone: 'danger', badge: '已超时' };
  }
  // totalMs 是 SLA 窗口总时长；缺失或 0 时不启用 20% 预警阈值。
  const totalMs = (slaMinutes && slaMinutes > 0 ? slaMinutes : 0) * 60_000;
  // urgent 表示剩余时间已不足窗口的 20%。
  const urgent = totalMs > 0 && remainingMs < totalMs * 0.2;
  return urgent
    ? { text: formatSlaDuration(remainingMs), tone: 'warning', badge: '将超时' }
    : { text: formatSlaDuration(remainingMs), tone: 'normal' };
};
