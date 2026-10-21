import React from 'react';

// PageHeaderProps 描述页面级页头的图标、文案与右侧动作槽。
export interface PageHeaderProps {
  /** icon 是页头左侧的图标组件，用于一眼标识页面所属功能域。 */
  icon: React.ElementType;
  /** title 是页面主标题，使用页面级字号与字重。 */
  title: string;
  /** subtitle 是标题下方的一句话说明，缺省时不渲染该行。 */
  subtitle?: string;
  /** actions 是右侧动作槽，通常承载刷新、跳转等页级操作。 */
  actions?: React.ReactNode;
}

// PageHeader 渲染页面级页头：图标块 + 标题 + 副标题 + 右侧动作槽。
export const PageHeader: React.FC<PageHeaderProps> = ({ icon: Icon, title, subtitle, actions }) => (
  <div className="flex items-center justify-between gap-4">
    <div className="flex items-center gap-4">
      <div className="w-12 h-12 bg-gray-100 rounded-2xl flex items-center justify-center">
        <Icon className="w-6 h-6 text-gray-600" />
      </div>
      <div>
        <h2 className="text-3xl font-extrabold text-ink">{title}</h2>
        {subtitle && <p className="text-muted mt-1 text-sm font-medium">{subtitle}</p>}
      </div>
    </div>
    {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
  </div>
);

export default PageHeader;
