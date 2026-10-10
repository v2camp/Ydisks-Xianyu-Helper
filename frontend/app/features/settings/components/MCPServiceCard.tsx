import { Check,Copy,KeyRound,Loader2,PlugZap,RefreshCw,ShieldAlert,Trash2 } from 'lucide-react';
import React,{ useCallback,useState } from 'react';
import type { MCPServiceStatus } from '../models';
import { MCP_AUDIT_CATEGORY_LABELS,buildMCPHarnessConfig,formatMCPUnixSeconds } from '../state';
import { useMCPService } from '../mcpHooks';

/** MCPServiceCard 让管理员启停对外开放的 MCP 服务、管理访问令牌并查看最近调用审计。 */
export const MCPServiceCard: React.FC = () => {
  // service 是 MCP 服务卡片的 Hook 状态与动作集合。
  const service = useMCPService();
  // copied 保存最近一次复制动作的按钮标识；空串表示无反馈。
  const [copied, setCopied] = useState('');

  /** 复制文本到剪贴板并给出按钮反馈；剪贴板不可用时保留原文供手工选择。 */
  const copyText = useCallback(/* 当前回调封装复制到剪贴板的降级处理。 */ async (key: string, text: string) => {
    try {
      if (typeof navigator !== 'undefined' && navigator.clipboard) await navigator.clipboard.writeText(text);
      setCopied(key);
    } catch {
      setCopied('');
    }
  }, []);

  // status 是当前 MCP 服务状态；未加载成功时为 null。
  const status: MCPServiceStatus | null = service.status;
  // enabled 表示 MCP 服务当前是否已启用。
  const enabled = status?.enabled === true;
  // allowNonLoopback 表示是否已放行非本机来源。
  const allowNonLoopback = status?.allowNonLoopback === true;
  // harnessConfig 是接入地址对应的 Harness 配置示例文本。
  const harnessConfig = buildMCPHarnessConfig(status?.endpoint || '', service.token);
  // currentPage 是审计表当前展示的页码。
  const currentPage = service.audit?.page || 1;
  // totalPages 是审计表总页数。
  const totalPages = service.audit?.totalPages || 0;

  return (
    <div className="ios-card rounded-xl p-6 bg-white space-y-5" data-testid="mcp-service-card">
      <div className="flex items-start gap-2">
        <div className="p-1.5 rounded-lg bg-indigo-500 text-white mt-0.5">
          <PlugZap className="w-4 h-4" />
        </div>
        <div>
          <h4 className="text-sm font-bold text-gray-800">MCP 服务</h4>
          <p className="text-xs text-gray-500 mt-0.5">把本服务的能力以 MCP 协议开放给本机 Harness；默认仅本机可访问，需要令牌鉴权。</p>
        </div>
      </div>

      {service.statusError && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-red-100 bg-red-50 px-4 py-3 text-sm text-red-700">
          <span>{service.statusError}</span>
          <button type="button" className="font-bold underline" onClick={/* 当前回调重新读取 MCP 服务状态。 */ () => void service.reloadStatus()}>重新加载</button>
        </div>
      )}

      {!status && service.statusLoading && (
        <div className="flex items-center gap-2 text-sm text-gray-500">
          <Loader2 className="w-4 h-4 animate-spin" />
          正在读取 MCP 服务状态...
        </div>
      )}

      {status && (
        <>
          <label className="flex items-start gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 cursor-pointer" htmlFor="mcp-service-enabled">
            <input
              id="mcp-service-enabled"
              type="checkbox"
              className="mt-1"
              checked={enabled}
              disabled={service.saving}
              onChange={/* 当前回调切换 MCP 服务启用开关并立即保存。 */ event => void service.submitSettings({ enabled: event.target.checked, allowNonLoopback })}
            />
            <span>
              <span className="block text-sm font-bold text-gray-900">启用 MCP 服务</span>
              <span className="mt-1 block text-xs leading-5 text-gray-600">关闭时 /mcp 端点对外表现为不存在；启用并生成令牌后 Harness 才能接入。</span>
            </span>
          </label>

          <label className="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 p-4 cursor-pointer" htmlFor="mcp-allow-non-loopback">
            <input
              id="mcp-allow-non-loopback"
              type="checkbox"
              className="mt-1"
              checked={allowNonLoopback}
              disabled={service.saving}
              onChange={/* 当前回调切换非本机访问策略并立即保存。 */ event => void service.submitSettings({ enabled, allowNonLoopback: event.target.checked })}
            />
            <span>
              <span className="flex items-center gap-1 text-sm font-bold text-amber-900">
                <ShieldAlert className="w-4 h-4" />
                允许非本机访问
              </span>
              <span className="mt-1 block text-xs leading-5 text-amber-800">默认关闭。开启后局域网内其他机器只要拿到令牌即可调用全部管理能力，请仅在可信网络中使用。</span>
            </span>
          </label>

          <div className="space-y-2">
            <label className="block text-sm font-bold text-gray-800">接入地址</label>
            <div className="flex items-center gap-2">
              <input
                type="text"
                readOnly
                value={enabled ? status.endpoint : '未启用'}
                className="w-full ios-input px-4 py-3 rounded-xl text-sm font-mono"
              />
              <button
                type="button"
                disabled={!enabled}
                onClick={/* 当前回调复制 MCP 接入地址。 */ () => void copyText('endpoint', status.endpoint)}
                className="px-3 py-3 rounded-xl bg-gray-100 hover:bg-gray-200 text-gray-700 disabled:opacity-40 flex items-center gap-1 text-xs font-bold"
                title="复制接入地址"
              >
                {copied === 'endpoint' ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                复制
              </button>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="block text-sm font-bold text-gray-800">Harness 配置示例</label>
              <button
                type="button"
                disabled={!enabled}
                onClick={/* 当前回调复制 Harness 配置示例。 */ () => void copyText('harness', harnessConfig)}
                className="px-3 py-1.5 rounded-lg bg-gray-100 hover:bg-gray-200 text-gray-700 disabled:opacity-40 flex items-center gap-1 text-xs font-bold"
                title="复制配置示例"
              >
                {copied === 'harness' ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                复制
              </button>
            </div>
            <pre className="rounded-xl bg-gray-900 text-gray-100 text-xs p-4 overflow-x-auto">{harnessConfig}</pre>
          </div>

          <div className="space-y-3">
            <label className="block text-sm font-bold text-gray-800">访问令牌</label>
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                disabled={service.tokenLoading}
                onClick={/* 当前回调生成或轮换持久化令牌。 */ () => void service.generateToken()}
                className="ios-btn-primary px-4 py-2.5 rounded-xl text-sm font-bold flex items-center gap-2 disabled:opacity-40"
              >
                {service.tokenLoading ? <Loader2 className="w-4 h-4 animate-spin" /> : <KeyRound className="w-4 h-4" />}
                {status.hasToken ? '轮换令牌' : '生成令牌'}
              </button>
              <button
                type="button"
                disabled={service.tokenLoading || !status.hasToken}
                onClick={/* 当前回调吊销全部持久化令牌。 */ () => void service.revokeToken()}
                className="px-4 py-2.5 rounded-xl bg-red-50 hover:bg-red-100 text-red-700 text-sm font-bold flex items-center gap-2 disabled:opacity-40"
              >
                <Trash2 className="w-4 h-4" />
                吊销令牌
              </button>
              {status.hasToken && (
                <span className="flex items-center gap-1 text-xs text-gray-500">
                  <RefreshCw className="w-3 h-3" />
                  创建于 {formatMCPUnixSeconds(status.tokenCreatedAt)}，最近使用 {formatMCPUnixSeconds(status.tokenLastUsedAt)}
                </span>
              )}
            </div>

            {service.token && (
              <div className="rounded-xl border border-green-200 bg-green-50 p-4 space-y-2">
                <p className="text-xs font-bold text-green-800">令牌只在此处显示一次，刷新或关闭页面后无法再查看，请立即复制保存。</p>
                <div className="flex items-center gap-2">
                  <input type="text" readOnly data-testid="mcp-token-plaintext" value={service.token} className="w-full ios-input px-3 py-2 rounded-lg text-xs font-mono" />
                  <button
                    type="button"
                    onClick={/* 当前回调复制一次性令牌明文。 */ () => void copyText('token', service.token)}
                    className="px-3 py-2 rounded-lg bg-green-600 hover:bg-green-700 text-white text-xs font-bold flex items-center gap-1"
                    title="复制令牌"
                  >
                    {copied === 'token' ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                    复制
                  </button>
                  <button
                    type="button"
                    onClick={/* 当前回调清除页面上保留的令牌明文。 */ service.dismissToken}
                    className="px-3 py-2 rounded-lg bg-white text-green-800 text-xs font-bold"
                  >
                    我已保存
                  </button>
                </div>
              </div>
            )}
          </div>

          {service.message && (
            <div className={`rounded-xl px-4 py-3 text-sm font-medium ${service.message.type === 'success' ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'}`}>
              {service.message.text}
            </div>
          )}

          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <label className="block text-sm font-bold text-gray-800">最近调用记录</label>
              <button
                type="button"
                onClick={/* 当前回调重新加载当前页审计。 */ () => service.loadAudit(currentPage)}
                className="px-3 py-1.5 rounded-lg bg-gray-100 hover:bg-gray-200 text-gray-700 text-xs font-bold flex items-center gap-1"
              >
                <RefreshCw className="w-3 h-3" />
                刷新
              </button>
            </div>

            {service.auditError && <p className="text-xs text-red-600">{service.auditError}</p>}

            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead>
                  <tr className="text-left text-gray-500">
                    <th className="py-2 pr-3 font-bold">时间</th>
                    <th className="py-2 pr-3 font-bold">类别</th>
                    <th className="py-2 pr-3 font-bold">名称</th>
                    <th className="py-2 pr-3 font-bold">账号</th>
                    <th className="py-2 pr-3 font-bold">结果</th>
                    <th className="py-2 font-bold">耗时</th>
                  </tr>
                </thead>
                <tbody>
                  {service.audit?.records.map(/* 当前回调渲染单条审计记录。 */ record => (
                    <tr key={record.id} className="border-t border-gray-100 text-gray-700">
                      <td className="py-2 pr-3 whitespace-nowrap">{formatMCPUnixSeconds(record.createdAt)}</td>
                      <td className="py-2 pr-3">{MCP_AUDIT_CATEGORY_LABELS[record.category] || record.category}</td>
                      <td className="py-2 pr-3 font-mono">{record.name}</td>
                      <td className="py-2 pr-3 font-mono">{record.cookieId || '—'}</td>
                      <td className={`py-2 pr-3 font-bold ${record.success ? 'text-green-600' : 'text-red-600'}`}>
                        {record.success ? '成功' : record.errorClass || '失败'}
                      </td>
                      <td className="py-2">{record.durationMs} ms</td>
                    </tr>
                  ))}
                  {service.audit && service.audit.records.length === 0 && (
                    <tr>
                      <td colSpan={6} className="py-4 text-center text-gray-400">
                        {service.auditLoading ? '加载中...' : '暂无调用记录'}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            <div className="flex items-center justify-between text-xs text-gray-500">
              <span>共 {service.audit?.total ?? 0} 条</span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={currentPage <= 1 || service.auditLoading}
                  onClick={/* 当前回调加载上一页审计。 */ () => service.loadAudit(currentPage - 1)}
                  className="px-3 py-1.5 rounded-lg bg-gray-100 hover:bg-gray-200 disabled:opacity-40 font-bold"
                >
                  上一页
                </button>
                <span>第 {currentPage} / {Math.max(totalPages, 1)} 页</span>
                <button
                  type="button"
                  disabled={totalPages === 0 || currentPage >= totalPages || service.auditLoading}
                  onClick={/* 当前回调加载下一页审计。 */ () => service.loadAudit(currentPage + 1)}
                  className="px-3 py-1.5 rounded-lg bg-gray-100 hover:bg-gray-200 disabled:opacity-40 font-bold"
                >
                  下一页
                </button>
              </div>
            </div>
          </div>
        </>
      )}
    </div>
  );
};
