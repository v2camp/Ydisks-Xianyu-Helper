// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import AISettings from './AISettings';
import { fetchAIModels,getSystemSettings,updateSystemSettings,verifySession } from '../api';
import type { SystemSettings } from '../api';

vi.mock('../api', /* mockSettingsApi 用可断言的替身替换 AI 设置页依赖的适配层，避免测试触网。 */ () => ({
  fetchAIModels: vi.fn(),
  getSystemSettings: vi.fn(),
  testAIConnection: vi.fn(),
  updateLoginCredentials: vi.fn(),
  updateSystemSettings: vi.fn(),
  verifySession: vi.fn(),
}));

// getSettingsMock 是系统设置读取的替身，负责让页面离开加载态。
const getSettingsMock = vi.mocked(getSystemSettings);
// verifySessionMock 是会话校验的替身，AI 页只消费其中的用户名。
const verifySessionMock = vi.mocked(verifySession);
// fetchModelsMock 是模型发现的替身，返回固定候选列表。
const fetchModelsMock = vi.mocked(fetchAIModels);
// updateSettingsMock 是 AI 配置保存的替身，用于断言总闸随载荷提交。
const updateSettingsMock = vi.mocked(updateSystemSettings);

// AI_SETTINGS_FIXTURE 是 AI 设置页渲染所需的最小系统设置快照；故意不含 ai_master_enabled，覆盖「缺省即开」。
const AI_SETTINGS_FIXTURE: SystemSettings = {
  ai_api_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
  ai_api_key: 'sk-test',
  ai_model: 'qwen-plus',
};

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

beforeEach(/* 当前回调为每个用例重置适配器替身的默认成功响应。 */ () => {
  vi.clearAllMocks();
  getSettingsMock.mockResolvedValue(AI_SETTINGS_FIXTURE);
  verifySessionMock.mockResolvedValue({ authenticated: true, username: 'admin' });
  fetchModelsMock.mockResolvedValue(['qwen-plus']);
  updateSettingsMock.mockResolvedValue({ success: true });
});

describe('AISettings 常见 AI 服务提示', /* 当前测试组验证示例列表默认折叠且不挤占表单版面。 */ () => {
  test('默认折叠，展开后列出两条示例地址', /* 当前测试验证折叠是默认态，且示例内容来自常量而非散落文案。 */ async () => {
    // container 用于定位 <details> 元素本身，折叠态只能从该元素读取。
    const { container } = render(<AISettings />);
    await screen.findByDisplayValue('qwen-plus');
    // details 是承载示例列表的折叠容器。
    const details = container.querySelector('details');
    expect(details).toBeTruthy();
    expect(details?.open).toBe(false);
    expect(details?.textContent).toContain('常见 AI 服务');
    // 两条示例都由常量渲染，折叠状态下内容仍在 DOM 中供展开时直接显示。
    expect(details?.textContent).toContain('阿里云通义千问: https://dashscope.aliyuncs.com/compatible-mode/v1');
    expect(details?.textContent).toContain('OpenAI: https://api.openai.com/v1');
    fireEvent.click(screen.getByText('常见 AI 服务'));
    expect(details?.open).toBe(true);
  });

  test('折叠不影响读取模型与测试连接入口', /* 当前测试验证折叠只作用于示例提示，模型发现与连通性测试仍在表单内。 */ async () => {
    render(<AISettings />);
    await screen.findByDisplayValue('qwen-plus');
    expect(screen.getByRole('button', { name: /读取模型/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /测试连接/ })).toBeTruthy();
  });
});

describe('AISettings AI 总闸', /* 当前测试组验证总闸缺省即开，并且改动随保存载荷提交。 */ () => {
  test('缺省未配置时总闸显示为开启且不再有原人工确认区块', /* 当前测试验证默认值与已删除的旧开关。 */ async () => {
    render(<AISettings />);
    // masterSwitch 是 AI 总闸复选框；服务端没写该键时语义就是开启。
    const masterSwitch = await screen.findByLabelText('启用 AI 自动回复') as HTMLInputElement;
    expect(masterSwitch.checked).toBe(true);
    // 人工确认模式已整体下线，页面上不应再出现该文案。
    expect(screen.queryByText(/AI 回复人工确认/)).toBeNull();
  });

  test('关闭总闸后随保存载荷提交 false', /* 当前测试验证总闸改动不会被白名单静默丢弃。 */ async () => {
    render(<AISettings />);
    // masterSwitch 是 AI 总闸复选框。
    const masterSwitch = await screen.findByLabelText('启用 AI 自动回复') as HTMLInputElement;
    fireEvent.click(masterSwitch);
    expect(masterSwitch.checked).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: '保存 AI 配置' }));
    await waitFor(/* 当前回调等待保存请求发出。 */ () => expect(updateSettingsMock).toHaveBeenCalledTimes(1));
    // payload 是本次 AI 配置实际提交的载荷。
    const payload = updateSettingsMock.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.ai_master_enabled).toBe(false);
  });
});
