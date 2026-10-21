import React from 'react';
import { SECTION_TONE_STYLES, type SectionTone } from './tokens';

// SectionTone 仍从本文件导出，保证既有调用方的导入路径不变；真实定义在 tokens.ts。
export type { SectionTone };

// SectionHeaderProps 描述区块标题的图标、文案与右侧动作槽。
export interface SectionHeaderProps {
  /** icon 是区块图标组件，与 tone 一起构成区块的视觉标识。 */
  icon: React.ElementType;
  /** title 是区块标题文案。 */
  title: string;
  /** description 是标题下方的一句话解释，缺省时只展示标题行。 */
  description?: string;
  /** tone 是语义色，缺省为 brand（AI 能力域）。 */
  tone?: SectionTone;
  /** actions 是标题行右侧的动作槽，用于放置该区块的局部操作。 */
  actions?: React.ReactNode;
}

// SectionHeader 渲染区块标题：语义色图标块 + 标题 + 可选说明与右侧动作槽。
// 图标块底色由 tokens.ts 的 SECTION_TONE_STYLES 提供，组件内不再持有色值映射。
export const SectionHeader: React.FC<SectionHeaderProps> = ({
  icon: Icon, title, description, tone = 'brand', actions,
}) => (
  <div className="space-y-1">
    <div className="flex items-center justify-between gap-3">
      <div className="flex items-center gap-2">
        <div className={`p-1.5 rounded-lg text-white ${SECTION_TONE_STYLES[tone]}`}>
          <Icon className="w-4 h-4" />
        </div>
        <h3 className="text-lg font-extrabold text-ink">{title}</h3>
      </div>
      {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
    </div>
    {description && <p className="text-xs leading-5 text-muted">{description}</p>}
  </div>
);

export default SectionHeader;
