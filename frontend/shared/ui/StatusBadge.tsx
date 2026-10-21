import React from 'react';
import { STATUS_BADGE_STYLES, type StatusBadgeTone } from './tokens';

// StatusBadgeTone 仍从本文件导出，保证既有调用方的导入路径不变；真实定义在 tokens.ts。
export type { StatusBadgeTone };

// StatusBadgeProps 描述徽章的语义色、文案与测试标识。
export interface StatusBadgeProps {
  /** tone 是语义色，决定徽章的底色与文字强弱。 */
  tone: StatusBadgeTone;
  /** children 是徽章文案，由调用方按当前状态传入具体措辞。 */
  children: React.ReactNode;
  /** testId 是稳定的测试标识，供页面级用例定位该徽章。 */
  testId?: string;
}

// StatusBadge 渲染只读状态徽章，统一连接器卡与 Agent 中心的状态呈现。
// 语义色类串由 tokens.ts 的 STATUS_BADGE_STYLES 提供，组件内不再持有色值映射。
export const StatusBadge: React.FC<StatusBadgeProps> = ({ tone, children, testId }) => (
  <span
    data-testid={testId}
    className={`inline-flex items-center text-[11px] font-bold px-2 py-0.5 rounded-full whitespace-nowrap ${STATUS_BADGE_STYLES[tone]}`}
  >
    {children}
  </span>
);

export default StatusBadge;
