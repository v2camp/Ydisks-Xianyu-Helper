// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import AgentCenter from './AgentCenter';
import { getAgentSupportSettings,updateAgentSupportSettings } from '../agentSupportApi';
import { getMCPServiceStatus } from '../api';
import type { MCPServiceStatus } from '../models';

vi.mock('../agentSupportApi', /* mockAgentSupportApi 用可断言的替身替换客服 Agent 适配层，避免测试触网。 */ () => ({
  getAgentSupportSettings: vi.fn(),
  updateAgentSupportSettings: vi.fn(),
}));

vi.mock('../api', /* mockSettingsApi 用替身替换页面读取的 MCP 状态接口，避免连带加载整份设置适配层。 */ () => ({
  getMCPServiceStatus: vi.fn(),
}));

// agentSupportSettingsMock 是租户级客服 Agent 配置读取的替身。
const agentSupportSettingsMock = vi.mocked(getAgentSupportSettings);
// agentSupportSettingsUpdateMock 是租户级客服 Agent 配置保存的替身。
const agentSupportSettingsUpdateMock = vi.mocked(updateAgentSupportSettings);
// mcpServiceStatusMock 是 MCP 服务现状读取的替身。
const mcpServiceStatusMock = vi.mocked(getMCPServiceStatus);

// DISABLED_MCP_STATUS 是未启用 MCP 服务时页面读取到的最小状态。
const DISABLED_MCP_STATUS: MCPServiceStatus = {
  enabled: false,
  allowNonLoopback: false,
  hasToken: false,
  tokenCreatedAt: 0,
  tokenLastUsedAt: 0,
  hasPreviousToken: false,
  previousTokenExpiresAt: 0,
  endpoint: '',
};

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

beforeEach(/* 当前回调为每个用例重置适配器替身的默认成功响应。 */ () => {
  agentSupportSettingsMock.mockResolvedValue({
    enabled: false,
    preset: 'readonly',
    presets: [
      { value: 'readonly', label: '只读', description: '只查询与答复，不产生对外动作' },
      { value: 'standard', label: '标准', description: '在只读之上允许生成报价等对外承诺' },
      { value: 'advanced', label: '高级', description: '在标准之上允许可逆的数据写入' },
    ],
  });
  agentSupportSettingsUpdateMock.mockResolvedValue();
  mcpServiceStatusMock.mockResolvedValue(DISABLED_MCP_STATUS);
});

describe('AgentCenter', /* 当前测试组验证三类 Agent 的入口归拢与租户级客服 Agent 授权往返。 */ () => {
  test('档位名单来自服务端下发而不写死在前端', /* 当前测试验证内核新增档位后界面自动跟随。 */ async () => {
    agentSupportSettingsMock.mockResolvedValue({
      enabled: true,
      preset: 'experimental',
      presets: [
        { value: 'readonly', label: '只读', description: '只查询与答复' },
        { value: 'experimental', label: '实验档', description: '内核新增的第四档' },
      ],
    });
    render(<AgentCenter />);
    // presetButton 是内核新增档位对应的按钮；前端没有写死名单，该按钮必须能渲染出来并标记为已选。
    const presetButton = await screen.findByRole('button', { name: /实验档/ });
    expect(presetButton.getAttribute('aria-pressed')).toBe('true');
  });

  test('保存提交开关与档位的全量载荷', /* 当前测试验证保存语义是替换而不是字段级部分更新。 */ async () => {
    render(<AgentCenter />);
    // enabledSwitch 是租户默认开关，初始跟随服务端返回的关闭状态。
    const enabledSwitch = await screen.findByLabelText('启用客服 Agent');
    expect(enabledSwitch.getAttribute('aria-checked')).toBe('false');
    fireEvent.click(enabledSwitch);
    fireEvent.click(screen.getByRole('button', { name: /高级/ }));
    fireEvent.click(screen.getByRole('button', { name: /保存/ }));
    await waitFor(/* 当前回调等待保存请求携带完整的开关与档位载荷。 */ () => expect(agentSupportSettingsUpdateMock).toHaveBeenCalledWith({ enabled: true, preset: 'advanced' }));
    expect(await screen.findByText('客服 Agent 配置已保存')).toBeTruthy();
  });

  test('保存失败时保留草稿并给出错误提示', /* 当前测试验证失败不会静默丢弃管理员的选择。 */ async () => {
    agentSupportSettingsUpdateMock.mockRejectedValue(new Error('档位取值非法'));
    render(<AgentCenter />);
    await screen.findByLabelText('启用客服 Agent');
    fireEvent.click(screen.getByRole('button', { name: /保存/ }));
    expect(await screen.findByText('档位取值非法')).toBeTruthy();
    expect((screen.getByLabelText('启用客服 Agent') as HTMLElement).getAttribute('aria-checked')).toBe('false');
  });

  test('服务端未下发档位名单时给出可解释的提示', /* 当前测试验证空名单不会退化成静默不可用。 */ async () => {
    agentSupportSettingsMock.mockResolvedValue({ enabled: false, preset: '', presets: [] });
    render(<AgentCenter />);
    expect(await screen.findByText(/服务端未下发档位名单/)).toBeTruthy();
  });

  test('租户配置读取失败时给出错误与重试入口', /* 当前测试验证失败路径仍可恢复。 */ async () => {
    agentSupportSettingsMock.mockRejectedValue(new Error('读取客服 Agent 配置失败'));
    render(<AgentCenter />);
    expect(await screen.findByText('读取客服 Agent 配置失败')).toBeTruthy();
    expect(screen.getByRole('button', { name: '重新加载' })).toBeTruthy();
  });

  test('管理端展示 Harness 启用状态与接入地址', /* 当前测试验证外部 Harness 的现状是只读展示而非本页配置项。 */ async () => {
    mcpServiceStatusMock.mockResolvedValue({ ...DISABLED_MCP_STATUS, enabled: true, endpoint: 'http://127.0.0.1:59190/mcp' });
    render(<AgentCenter />);
    expect(await screen.findByText('已启用')).toBeTruthy();
    expect(screen.getByText(/http:\/\/127\.0\.0\.1:59190\/mcp/)).toBeTruthy();
    expect(screen.getByText(/没有档位可配/)).toBeTruthy();
  });

  test('运营 Agent 只给只读指引并指明配置位置', /* 当前测试验证配置项留在 QQ 连接器卡片这一归属约定。 */ async () => {
    render(<AgentCenter />);
    await screen.findByLabelText('启用客服 Agent');
    expect(screen.getByText(/QQ 机器人连接器卡片/)).toBeTruthy();
    expect(screen.getByText(/本页不对该 Agent 提供配置项/)).toBeTruthy();
  });
});
