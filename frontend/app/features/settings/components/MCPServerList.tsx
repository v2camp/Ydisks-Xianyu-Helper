// MCPServerList 编辑 MCP 服务器列表：每行「名称 + 服务地址」，序列化到
// settings['mcp.servers'] 的 JSON 字符串（[{"name":"...","url":"..."}]）。
// 行内校验名称必填且页内不重复、URL 须为 http(s)；非法行就地红框红字提示。

import React from 'react';
import { Server } from 'lucide-react';
import type { SystemSettings } from '../api';
import { EditableList } from './EditableList';

// MCPServer 是 MCP 服务器一行的已知字段与可保留的扩展字段。
export interface MCPServer {
  /** name 是引擎侧识别服务器的名称，例如 find_stuff。 */
  name: string;
  /** url 是 MCP 服务的 http(s) 绝对地址。 */
  url: string;
  /** 未知扩展键在编辑与回写时原样保留。 */
  [key: string]: unknown;
}

// MCPServerListProps 描述 MCP 服务器列表编辑区需要的状态与回调。
export interface MCPServerListProps {
  /** settings 是当前系统配置草稿。 */
  settings: SystemSettings;
  /** onChange 更新系统配置草稿中的 mcp.servers 键。 */
  onChange: (patch: Partial<SystemSettings>) => void;
}

// isHttpUrl 判断字符串是否为合法的 http 或 https 绝对地址。
function isHttpUrl(value: string): boolean {
  try {
    // parsed 是解析后的 URL 对象；相对路径或非法字符串会抛错。
    const parsed = new URL(value);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

// normalizeServerRow 把解析出的行数据归一为已知字段字符串，未知键原样保留。
function normalizeServerRow(raw: unknown): MCPServer {
  // source 是待归一的原始行对象；非对象回退空对象。
  const source = raw && typeof raw === 'object' && !Array.isArray(raw) ? raw as Record<string, unknown> : {};
  return {
    ...source,
    name: typeof source.name === 'string' ? source.name : '',
    url: typeof source.url === 'string' ? source.url : '',
  };
}

// parseServerArray 把 mcp.servers 的 JSON 字符串解析为行数组；非法或非数组时返回空数组。
function parseServerArray(raw: string | undefined): MCPServer[] {
  if (!raw || raw.trim() === '') return [];
  try {
    // value 是解析后的原始 JSON 值。
    const value = JSON.parse(raw);
    if (!Array.isArray(value)) return [];
    return value.map(normalizeServerRow);
  } catch {
    return [];
  }
}

// createEmptyServer 构造新增 MCP 行的空初始数据。
function createEmptyServer(): MCPServer {
  return { name: '', url: '' };
}

// MCPServerList 渲染 MCP 服务器列表编辑区，替换原 JSON 占位输入。
export const MCPServerList: React.FC<MCPServerListProps> = ({ settings, onChange }) => {
  // raw 是当前 mcp.servers 配置原文，缺省空串。
  const raw = typeof settings['mcp.servers'] === 'string' ? settings['mcp.servers'] : '';
  // servers 是解析后的服务器行列表。
  const servers = parseServerArray(raw);

  // emitServers 把行列表序列化回 mcp.servers 配置字符串并写回草稿。
  const emitServers = (rows: MCPServer[]) => {
    onChange({ 'mcp.servers': JSON.stringify(rows, null, 2) });
  };

  // validateServerRow 返回单行服务器的行内校验错误列表，含名称必填/不重复与 URL 合法性。
  const validateServerRow = (row: MCPServer, index: number): string[] => {
    // errors 收集当前行的校验错误文案。
    const errors: string[] = [];
    // name 是去掉首尾空白后的名称，用于必填与重复判断。
    const name = row.name.trim();
    if (name === '') {
      errors.push('名称必填');
    } else if (servers.some(/* other 是参与重复比对的其他服务器行。 */ (other, otherIndex) => otherIndex !== index && other.name.trim() === name)) {
      errors.push('名称不能与本页其他服务器重复');
    }
    // url 是去掉首尾空白后的地址，用于必填与协议判断。
    const url = row.url.trim();
    if (url === '') {
      errors.push('地址必填');
    } else if (!isHttpUrl(url)) {
      errors.push('地址须以 http:// 或 https:// 开头');
    }
    return errors;
  };

  return (
    <section className="space-y-4">
      <h3 className="text-lg font-extrabold text-gray-800 flex items-center gap-2">
        <div className="p-1.5 rounded-lg bg-emerald-500 text-white">
          <Server className="w-4 h-4" />
        </div>
        MCP 服务器列表
      </h3>
      <div className="ios-card rounded-xl p-6 bg-white space-y-4">
        <p className="text-xs text-gray-500">
          引擎目前消费 name 为 find_stuff 的条目；未配置时回落环境变量 FIND_STUFF_MCP_URL。
        </p>
        <EditableList
          rows={servers}
          onChange={emitServers}
          createRow={createEmptyServer}
          getRowKey={/* rowKeyProvider 用行索引拼出稳定的列表 React key。 */ (_row, index) => `mcp-server-${index}`}
          validateRow={validateServerRow}
          addButtonText="添加服务器"
          emptyText="名称填 find_stuff + 找书找短剧服务地址"
          renderRow={/* renderRow 渲染 MCP 名称与地址编辑控件。 */ (row, index, update) => (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              <input
                value={row.name}
                onChange={/* 输入变化更新当前行的服务器名称。 */ event => update({ name: event.target.value })}
                placeholder={index === 0 ? 'find_stuff' : '名称'}
                aria-label={`第 ${index + 1} 行名称`}
                className="w-full ios-input px-3 py-2 rounded-lg text-sm"
              />
              <input
                value={row.url}
                onChange={/* 输入变化更新当前行的服务地址。 */ event => update({ url: event.target.value })}
                placeholder={index === 0 ? 'http://find-stuff:59190/mcp' : 'http(s)://…'}
                aria-label={`第 ${index + 1} 行地址`}
                className="w-full ios-input px-3 py-2 rounded-lg text-sm font-mono"
              />
            </div>
          )}
        />
      </div>
    </section>
  );
};

export default MCPServerList;
