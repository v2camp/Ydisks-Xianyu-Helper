// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { ConnectorsState } from '../types';
import type { UserIntegrationsState } from '../../../../shared/async/useUserIntegrations';
import Connectors from './Connectors';

// useConnectorsMock 是页面测试使用的通知 Hook 替身，让页面只依赖受控的状态输入。
const { useConnectorsMock,useUserIntegrationsMock } = vi.hoisted(/* hoistedHookMock 在模块求值前创建两个 Hook 替身。 */ () => ({
  useConnectorsMock: vi.fn(),
  useUserIntegrationsMock: vi.fn(),
}));

vi.mock('../hooks', /* useConnectorsModuleMock 用替身替换真实请求逻辑。 */ () => ({
  useConnectors: /* useConnectorsStub 返回当前替身实现。 */ () => useConnectorsMock(),
}));

vi.mock('../../../../shared/async/useUserIntegrations', /* userIntegrationsModuleMock 用替身替换用户集成请求逻辑。 */ () => ({
  useUserIntegrations: /* userIntegrationsStub 返回当前替身实现。 */ () => useUserIntegrationsMock(),
}));

afterEach(/* cleanupDom 隔离每个用例创建的 DOM。 */ () => cleanup());

// emptyForm 是渠道弹窗使用的空表单，页面测试不修改它。
const emptyForm: ConnectorsState['form'] = { name: '', type: 'bark', enabled: true, config: {}, event_types: [] };

// resolveNothing 是页面测试共用的空异步动作实现，避免每个替身各写一个匿名函数。
const resolveNothing = async (): Promise<void> => undefined;

// createState 构造一份完整的通知页面状态，overrides 用于覆盖当前用例关心的字段。
const createState = (overrides: Partial<ConnectorsState> = {}): ConnectorsState => ({
  channels: [], loading: false, showModal: false, editing: null, saving: false, testingId: '', toast: null,
  savedCredentialKeys: [], form: emptyForm,
  setForm: vi.fn(),
  loadChannels: vi.fn(resolveNothing),
  openCreate: vi.fn(), openEdit: vi.fn(resolveNothing), closeModal: vi.fn(), resetCredentialState: vi.fn(), showToast: vi.fn(),
  handleSave: vi.fn(resolveNothing), handleDelete: vi.fn(resolveNothing),
  handleToggleEnabled: vi.fn(resolveNothing), handleTest: vi.fn(resolveNothing),
  ...overrides,
});

// createIntegrations 构造一份完整的用户集成配置状态。
const createIntegrations = (overrides: Partial<UserIntegrationsState> = {}): UserIntegrationsState => ({
  settings: {}, loading: false, loadError: '', saveState: 'idle', saveError: '',
  setSettings: vi.fn(), load: vi.fn(resolveNothing), save: vi.fn(resolveNothing), resetSaveState: vi.fn(),
  ...overrides,
});

describe('Connectors 页面', /* 当前测试组验证连接器页面的外壳、读取态、错误态与刷新语义。 */ () => {
  beforeEach(/* resetHookMock 清空上一条用例的调用记录。 */ () => {
    useConnectorsMock.mockReset();
    useUserIntegrationsMock.mockReset();
    useUserIntegrationsMock.mockReturnValue(createIntegrations());
  });

  test('页面使用统一外壳并渲染用户级通知与集成配置分区', /* 当前测试验证 PageShell/PageHeader 与用户级分区落位。 */ () => {
    useConnectorsMock.mockReturnValue(createState());
    render(<Connectors />);
    expect(useConnectorsMock).toHaveBeenCalledWith();
    expect(screen.getByRole('heading', { name: '连接器' })).toBeTruthy();
    expect(screen.getByText('配置第三方平台连接器与通知渠道，账号异常时主动推送告警')).toBeTruthy();
    expect(screen.getByText('通知渠道')).toBeTruthy();
    expect(screen.getByText('当前用户的集成配置')).toBeTruthy();
    expect(screen.getByText('邮件发送（SMTP）')).toBeTruthy();
    expect(screen.getByText('保存集成配置')).toBeTruthy();
    expect(screen.getByRole('button', { name: /刷新/ })).toBeTruthy();
  });

  test('集成配置读取中时区块显示加载中且刷新按钮禁用', /* 当前测试验证读取中先于就绪判定，避免误报未配置。 */ () => {
    useConnectorsMock.mockReturnValue(createState());
    useUserIntegrationsMock.mockReturnValue(createIntegrations({ loading: true }));
    render(<Connectors />);
    expect(screen.getByText('加载中...')).toBeTruthy();
    // refreshButton 是读取中的页头刷新按钮，必须禁用以免重复触发。
    expect((screen.getByRole('button', { name: /刷新/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('集成配置读取失败时错误在区块可见并可重新读取', /* 当前测试验证静默失败已被可见错误与重试入口替代。 */ () => {
    // integrations 是集成配置读取失败场景的页面状态。
    const integrations = createIntegrations({ loadError: '读取集成配置失败，请重试' });
    useConnectorsMock.mockReturnValue(createState());
    useUserIntegrationsMock.mockReturnValue(integrations);
    render(<Connectors />);
    expect(screen.getByText('读取集成配置失败，请重试')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '重新读取' }));
    expect(integrations.load).toHaveBeenCalledTimes(1);
  });

  test('页头刷新同时重载通知渠道与集成配置', /* 当前测试验证刷新按钮绑定两个数据源。 */ () => {
    // state 是可点击刷新场景的通知状态。
    const state = createState();
    // integrations 是可点击刷新场景的集成配置状态。
    const integrations = createIntegrations();
    useConnectorsMock.mockReturnValue(state);
    useUserIntegrationsMock.mockReturnValue(integrations);
    render(<Connectors />);
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    expect(state.loadChannels).toHaveBeenCalledTimes(1);
    expect(integrations.load).toHaveBeenCalledTimes(1);
  });
});
