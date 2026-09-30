import React, { useEffect, useState } from 'react';
import { slaCountdownView } from '../slaCountdown';

// SlaCountdownCellProps 描述订单表格发货倒计时单元格的输入。
interface SlaCountdownCellProps {
  /** slaDeadline 是付款时间加 SLA 的截止时刻 ISO 文本，空值表示未启用。 */
  slaDeadline?: string | null;
  /** slaMinutes 是生效的 SLA 分钟数，用于剩余 20% 预警阈值。 */
  slaMinutes?: number;
}

/** SlaCountdownCell 渲染发货倒计时，并每 30 秒本地重算剩余时间。 */
export const SlaCountdownCell: React.FC<SlaCountdownCellProps> = ({ slaDeadline, slaMinutes }) => {
  // nowMs 保存本地重算基准时刻的 Unix 毫秒，初始取挂载时刻。
  const [nowMs, setNowMs] = useState(/* initialNow 是组件挂载时刻的 Unix 毫秒。 */ () => Date.now());

  useEffect(
    /* timerEffect 每 30 秒推进一次本地时钟并驱动倒计时重渲染。 */ () => {
    // timer 是 30 秒间隔的本地重算定时器。
    const timer = window.setInterval(
      /* tick 刷新本地时钟到当前时刻。 */ () => setNowMs(Date.now()),
      30_000,
    );
    return /* cleanup 在组件卸载时清理重算定时器。 */ () => window.clearInterval(timer);
    },
    [],
  );

  // view 是按当前时刻计算的倒计时展示状态。
  const view = slaCountdownView(slaDeadline, slaMinutes, nowMs);
  // toneStyles 保存各展示档位的文本与徽标样式。
  const toneStyles = {
    muted: 'text-gray-400',
    normal: 'text-gray-800 font-semibold',
    warning: 'text-amber-600 font-bold',
    danger: 'text-red-600 font-bold',
  } as const;

  return (
    <span className={`text-sm whitespace-nowrap ${toneStyles[view.tone]}`} data-testid="sla-countdown">
      {view.text}
      {view.badge && (
        <span className={`ml-1.5 inline-block rounded-full px-2 py-0.5 text-[10px] font-bold ${view.tone === 'danger' ? 'bg-red-100 text-red-700' : 'bg-amber-100 text-amber-700'}`}>
          {view.badge}
        </span>
      )}
    </span>
  );
};
