import React from 'react';

// StatusBadgeTone 是徽章的语义色：ready 已就绪、missing 未配置、loading 读取中、warning 合法默认（如回落内置）。
export type StatusBadgeTone = 'ready' | 'missing' | 'loading' | 'warning';

// StatusBadgeProps 描述徽章的语义色、文案与测试标识。
export interface StatusBadgeProps {
  /** tone 是语义色，决定徽章的底色与文字强弱。 */
  tone: StatusBadgeTone;
  /** children 是徽章文案，由调用方按当前状态传入具体措辞。 */
  children: React.ReactNode;
  /** testId 是稳定的测试标识，供页面级用例定位该徽章。 */
  testId?: string;
}

// STATUS_TONE_CLASSES 把语义色映射到徽章类名：就绪用成功色，回落内置用琥珀弱化，读取与未配置保持中性。
const STATUS_TONE_CLASSES: Record<StatusBadgeTone, string> = {
  ready: 'bg-success-100 text-success-700',
  missing: 'bg-gray-100 text-gray-500',
  loading: 'bg-gray-100 text-gray-500',
  warning: 'bg-warning-100 text-warning-700',
};

// StatusBadge 渲染只读状态徽章，统一连接器卡与 Agent 中心的状态呈现。
export const StatusBadge: React.FC<StatusBadgeProps> = ({ tone, children, testId }) => (
  <span
    data-testid={testId}
    className={`inline-flex items-center text-[11px] font-bold px-2 py-0.5 rounded-full whitespace-nowrap ${STATUS_TONE_CLASSES[tone]}`}
  >
    {children}
  </span>
);

export default StatusBadge;
