// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { useState } from 'react';
import type { SystemSettings } from '../api';
import { MCPServerList } from './MCPServerList';

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

// MCPServerHarnessProps 描述 MCP 列表受控测试容器的输入。
interface MCPServerHarnessProps {
  /** initial 是渲染前的系统配置草稿初值。 */
  initial: SystemSettings;
}

// MCPServerHarness 是 MCP 列表的受控测试容器，保存草稿并展示序列化结果。
function MCPServerHarness({ initial }: MCPServerHarnessProps) {
  // settings 是受控的系统配置草稿状态。
  const [settings, setSettings] = useState<SystemSettings>(initial);
  return (
    <div>
      <MCPServerList settings={settings} onChange={/* patch 合并进测试草稿状态。 */ patch => setSettings(/* prev 是合并补丁前的草稿，返回合并后的新草稿。 */ prev => ({ ...prev, ...patch }))} />
      <pre data-testid="draft">{typeof settings['mcp.servers'] === 'string' ? settings['mcp.servers'] : ''}</pre>
    </div>
  );
}

// parseDraft 读取测试容器底部展示的 mcp.servers 草稿 JSON。
function parseDraft(): unknown {
  return JSON.parse(screen.getByTestId('draft').textContent || '[]');
}

describe('MCPServerList', /* 当前测试组验证 MCP 服务器列表编辑、序列化与行内校验。 */ () => {
  test('解析已有 JSON 并支持编辑、新增与删除后序列化回写', /* 当前测试验证 JSON 往返与增删改主路径。 */ () => {
    render(<MCPServerHarness initial={{ 'mcp.servers': '[{"name":"find_stuff","url":"http://127.0.0.1:59190/mcp"}]' }} />);
    // nameInput 是第一行名称输入框。
    const nameInput = screen.getByLabelText('第 1 行名称') as HTMLInputElement;
    expect(nameInput.value).toBe('find_stuff');
    fireEvent.change(nameInput, { target: { value: 'other_stuff' } });
    expect(parseDraft()).toEqual([{ name: 'other_stuff', url: 'http://127.0.0.1:59190/mcp' }]);
    fireEvent.click(screen.getByRole('button', { name: '添加服务器' }));
    expect((screen.getByLabelText('第 2 行名称') as HTMLInputElement).value).toBe('');
    fireEvent.click(screen.getByRole('button', { name: '删除第 2 行' }));
    expect(parseDraft()).toEqual([{ name: 'other_stuff', url: 'http://127.0.0.1:59190/mcp' }]);
  });

  test('名称必填且不重复、URL 须 http(s) 就地红字提示', /* 当前测试验证重复名与非法 URL 的行内校验。 */ () => {
    render(<MCPServerHarness initial={{ 'mcp.servers': '[{"name":"a","url":"http://a.example/mcp"},{"name":"a","url":"ftp://bad"},{"name":"","url":""}]' }} />);
    expect(screen.getAllByText('名称不能与本页其他服务器重复').length).toBe(2);
    expect(screen.getAllByText('地址须以 http:// 或 https:// 开头').length).toBe(1);
    expect(screen.getAllByText('名称必填').length).toBeGreaterThan(0);
    expect(screen.getAllByText('地址必填').length).toBeGreaterThan(0);
  });

  test('空状态示范 find_stuff 且未知键在编辑后原样保留', /* 当前测试验证空态文案与未知键保留。 */ () => {
    render(<MCPServerHarness initial={{ 'mcp.servers': '' }} />);
    expect(screen.getByText('名称填 find_stuff + 找书找短剧服务地址')).toBeTruthy();
    cleanup();
    render(<MCPServerHarness initial={{ 'mcp.servers': '[{"name":"find_stuff","url":"http://127.0.0.1:59190/mcp","timeout_ms":30}]' }} />);
    fireEvent.change(screen.getByLabelText('第 1 行名称'), { target: { value: 'find_stuff2' } });
    expect(parseDraft()).toEqual([{ name: 'find_stuff2', url: 'http://127.0.0.1:59190/mcp', timeout_ms: 30 }]);
  });
});
