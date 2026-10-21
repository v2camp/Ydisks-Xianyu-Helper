import React from 'react';

// PageShellProps 描述页面外壳需要承载的内容与可选的布局微调类名。
export interface PageShellProps {
  /** children 是页面主体内容，由各页面按业务区块自行组织。 */
  children: React.ReactNode;
  /** className 是调用方追加的外层类名，仅用于个别页面的局部布局微调。 */
  className?: string;
}

// PageShell 是配置类页面统一的外壳：约束内容宽度与区块间距，并预留页级保存条的底部避让空间。
export const PageShell: React.FC<PageShellProps> = ({ children, className }) => (
  <div className={`mx-auto max-w-6xl space-y-8 animate-fade-in pb-24 ${className ?? ''}`}>
    {children}
  </div>
);

export default PageShell;
