// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import AgentCenter from './AgentCenter';
import { getAgentSupportSettings,getMCPAudit,getMCPServiceStatus,getSystemSettings,updateAgentSupportSettings } from '../api';
import type { AgentSupportSettings,MCPAuditPage,MCPServiceStatus,SystemSettings } from '../models';

vi.mock('../api', /* mockSettingsApi 用可断言的替身替换设置适配层，避免测试触网；MCP 卡片、客服 Agent 与依赖读取共用同一适配层。 */ () => ({
  getAgentSupportSettings: vi.fn(),
  updateAgentSupportSettings: vi.fn(),
  getMCPServiceStatus: vi.fn(),
  getMCPAudit: vi.fn(),
  getSystemSettings: vi.fn(),
  updateMCPServiceSettings: vi.fn(),
  generateMCPToken: vi.fn(),
  revokeMCPToken: vi.fn(),
}));

// agentSupportSettingsMock 是租户级客服 Agent 配置读取的替身。
const agentSupportSettingsMock = vi.mocked(getAgentSupportSettings);
// agentSupportSettingsUpdateMock 是租户级客服 Agent 配置保存的替身。
const agentSupportSettingsUpdateMock = vi.mocked(updateAgentSupportSettings);
// mcpServiceStatusMock 是 MCP 服务现状读取的替身。
const mcpServiceStatusMock = vi.mocked(getMCPServiceStatus);
// mcpAuditMock 是 MCP 调用审计分页读取的替身，MCP 卡片挂载时会立即拉取首页。
const mcpAuditMock = vi.mocked(getMCPAudit);
// systemSettingsMock 是依赖状态读取的替身，页面据此推导三类 Agent 的前置依赖。
const systemSettingsMock = vi.mocked(getSystemSettings);

// READY_SETTINGS 是四条依赖全部就绪的系统设置快照：密钥已配置、模型非空、接管范围非空、至少一个 MCP 服务、QQ 凭据齐全。
const READY_SETTINGS: SystemSettings = {
  ai_api_key_configured: true,
  ai_model: 'gpt-4o-mini',
  ai_scope_config: '{"intents":["price"]}',
  'mcp.servers': '[{"name":"find_stuff","url":"http://127.0.0.1:59190/mcp"}]',
  'qqbot.app_id': '102012345',
  'qqbot.app_secret_configured': true,
};

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

// EMPTY_MCP_AUDIT 是尚无调用记录时的审计首页，用于让 MCP 卡片渲染空态。
const EMPTY_MCP_AUDIT: MCPAuditPage = { records: [], total: 0, page: 1, pageSize: 20, totalPages: 0 };

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
  mcpAuditMock.mockResolvedValue(EMPTY_MCP_AUDIT);
  systemSettingsMock.mockResolvedValue(READY_SETTINGS);
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

  test('管理端只由 MCP 服务卡展示启用状态与接入地址', /* 当前测试验证摘要卡已并入 MCP 服务卡，同一事实不会在两处渲染。 */ async () => {
    mcpServiceStatusMock.mockResolvedValue({ ...DISABLED_MCP_STATUS, enabled: true, endpoint: 'http://127.0.0.1:59190/mcp' });
    render(<AgentCenter />);
    // card 是唯一的 MCP 服务卡，启用状态、接入地址与 Harness 配置示例都在其中。
    const card = await screen.findByTestId('mcp-service-card');
    expect(card.textContent).toContain('启用 MCP 服务');
    // 接入地址以只读输入框的值呈现，启用时展示真实地址。
    expect(await screen.findByDisplayValue('http://127.0.0.1:59190/mcp')).toBeTruthy();
    // 重复摘要卡已删除：它的标题与说明文案都不应再出现在页面上。
    expect(screen.queryByText('外部 Harness 接入（MCP）')).toBeNull();
    expect(screen.queryByText(/没有档位可配/)).toBeNull();
    // 区块标题与说明保留，入口语义不变。
    expect(screen.getByText('对外 MCP 服务（入站）')).toBeTruthy();
  });

  test('页头刷新同时重读租户配置与前置依赖', /* 当前测试验证刷新不会只刷配置而让依赖状态停在旧值。 */ async () => {
    systemSettingsMock.mockResolvedValueOnce({ ...READY_SETTINGS, ai_api_key_configured: false });
    render(<AgentCenter />);
    expect((await screen.findByTestId('dependency-ai-model')).textContent).toContain('未配置');
    // settingsCalls 记录挂载后租户配置的请求次数，用于确认刷新确实重发了一次。
    const settingsCalls = agentSupportSettingsMock.mock.calls.length;
    // dependencyCalls 记录挂载后依赖状态的请求次数，用于确认刷新没有漏掉依赖读取。
    const dependencyCalls = systemSettingsMock.mock.calls.length;
    systemSettingsMock.mockResolvedValue(READY_SETTINGS);
    // 页头刷新是页面第一个「刷新」按钮，MCP 卡片内的审计刷新在它之后渲染。
    fireEvent.click(screen.getAllByRole('button', { name: '刷新' })[0]);
    expect((await screen.findByTestId('dependency-ai-model')).textContent).toContain('已配置');
    expect(agentSupportSettingsMock.mock.calls.length).toBeGreaterThan(settingsCalls);
    expect(systemSettingsMock.mock.calls.length).toBeGreaterThan(dependencyCalls);
  });

  test('刷新请求未完成时页头按钮保持禁用', /* 当前测试验证任一读取进行中都不允许叠加下一次刷新。 */ async () => {
    // resolveSettings 由测试控制租户配置请求何时返回，用于观察请求进行中的按钮状态。
    let resolveSettings: (value: AgentSupportSettings) => void = () => undefined;
    render(<AgentCenter />);
    await screen.findByTestId('dependency-ai-model');
    agentSupportSettingsMock.mockReturnValueOnce(new Promise<AgentSupportSettings>(/* 挂起的租户配置请求。 */ resolve => { resolveSettings = resolve; }));
    // refreshButton 是页头的整体刷新入口。
    const refreshButton = screen.getAllByRole('button', { name: '刷新' })[0] as HTMLButtonElement;
    fireEvent.click(refreshButton);
    await waitFor(/* 当前回调等待按钮进入禁用态。 */ () => expect(refreshButton.disabled).toBe(true));
    resolveSettings({ enabled: false, preset: 'readonly', presets: [] });
    await waitFor(/* 当前回调等待请求结束、按钮恢复可用。 */ () => expect(refreshButton.disabled).toBe(false));
  });

  test('运营 Agent 只给只读指引并指明配置位置', /* 当前测试验证配置项留在 QQ 连接器卡片这一归属约定。 */ async () => {
    render(<AgentCenter />);
    await screen.findByLabelText('启用客服 Agent');
    expect(screen.getByText(/QQ 机器人连接器卡片/)).toBeTruthy();
    expect(screen.getByText(/本页不对该 Agent 提供配置项/)).toBeTruthy();
  });

  test('四类前置依赖就绪时逐行显示已配置', /* 当前测试验证依赖状态来自系统设置快照而不是前端猜测。 */ async () => {
    render(<AgentCenter />);
    // 三个依赖行的可见文案都由同一次系统设置读取推导，逐行断言避免「一处就绪就当全部就绪」。
    for (const /* testId 是当前待断言的依赖行测试标识。 */ testId of ['dependency-ai-model', 'dependency-ai-scope', 'dependency-ai-tools', 'dependency-qq-connector']) {
      // row 是当前待断言的依赖行。
      const row = await screen.findByTestId(testId);
      expect(row.textContent).toContain('已配置');
    }
  });

  test('接管范围留空时显示回落内置而不是未配置', /* 当前测试验证留空是合法默认值，不会误报为缺配置。 */ async () => {
    systemSettingsMock.mockResolvedValue({ ...READY_SETTINGS, ai_scope_config: '   ' });
    render(<AgentCenter />);
    // scopeRow 是接管范围依赖行，空白配置应呈现回落态。
    const scopeRow = await screen.findByTestId('dependency-ai-scope');
    expect(scopeRow.textContent).toContain('未配置 · 回落内置');
  });

  test('依赖读取失败时显示错误并可重试', /* 当前测试验证失败信息落在页面上且可恢复，不只写进控制台。 */ async () => {
    systemSettingsMock.mockRejectedValueOnce(new Error('读取系统设置失败'));
    render(<AgentCenter />);
    expect(await screen.findByText('读取系统设置失败')).toBeTruthy();
    // retryButton 是依赖区的重试入口，点击后应重新发起读取并渲染依赖行。
    const retryButton = screen.getByRole('button', { name: '重新读取' });
    systemSettingsMock.mockResolvedValue(READY_SETTINGS);
    fireEvent.click(retryButton);
    expect((await screen.findByTestId('dependency-ai-model')).textContent).toContain('已配置');
  });
});
