import { Loader2 } from 'lucide-react';
import React from 'react';

// AsyncSectionProps 描述区块级异步容器的三态输入与内容插槽。
export interface AsyncSectionProps {
  /** loading 表示该区块数据仍在读取，为 true 时优先展示加载态，避免先闪出空表单。 */
  loading?: boolean;
  /** error 是读取失败的用户可见文案，非空时展示错误态并隐藏内容。 */
  error?: string;
  /** onRetry 是错误态「重试」按钮的点击回调，缺省时只展示错误文案而不给无效入口。 */
  onRetry?: () => void;
  /** retryLabel 是重试按钮文案，缺省「重试」，供不同区块贴合自身语境的措辞。 */
  retryLabel?: string;
  /** children 是数据就绪后渲染的区块内容。 */
  children: React.ReactNode;
  /** className 是附加在容器上的类名，供调用方微调间距。 */
  className?: string;
}

// AsyncSection 只负责加载、错误与内容三种状态的互斥展示，不持有任何请求逻辑。
export const AsyncSection: React.FC<AsyncSectionProps> = ({
  loading = false, error, onRetry, retryLabel = '重试', children, className,
}) => {
  if (loading) {
    return (
      <div className={`flex items-center gap-2 p-6 text-sm text-muted ${className ?? ''}`} role="status">
        <Loader2 className="w-4 h-4 animate-spin" />
        加载中...
      </div>
    );
  }

  if (error) {
    return (
      <div
        className={`flex items-center justify-between gap-3 rounded-xl border border-danger-100 bg-danger-50 px-4 py-3 text-sm text-danger-700 ${className ?? ''}`}
        role="alert"
      >
        <span>{error}</span>
        {onRetry && (
          <button
            type="button"
            onClick={onRetry}
            className="font-bold underline shrink-0"
          >
            {retryLabel}
          </button>
        )}
      </div>
    );
  }

  return <div className={className}>{children}</div>;
};

export default AsyncSection;
