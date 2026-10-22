// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { ConnectorsState } from '../types';
import Connectors from './Connectors';

// useConnectorsMock 是页面测试使用的 Hook 替身，让页面只依赖受控的状态输入。
const { useConnectorsMock } = vi.hoisted(/* hoistedHookMock 在模块求值前创建 Hook 替身。 */ () => ({ useConnectorsMock: vi.fn() }));

vi.mock('../hooks', /* useConnectorsModuleMock 用替身替换真实请求逻辑。 */ () => ({
  useConnectors: /* useConnectorsStub 记录权限入参并返回当前替身实现。 */ (isAdmin: boolean) => useConnectorsMock(isAdmin),
}));

afterEach(/* cleanupDom 隔离每个用例创建的 DOM。 */ () => cleanup());

// emptyForm 是渠道弹窗使用的空表单，页面测试不修改它。
const emptyForm: ConnectorsState['form'] = { name: '', type: 'bark', enabled: true, config: {}, event_types: [] };

// resolveNothing 是页面测试共用的空异步动作实现，避免每个替身各写一个匿名函数。
const resolveNothing = async (): Promise<void> => undefined;

// createState 构造一份完整的页面状态，overrides 用于覆盖当前用例关心的字段。
const createState = (overrides: Partial<ConnectorsState> = {}): ConnectorsState => ({
  channels: [], loading: false, showModal: false, editing: null, saving: false, testingId: '', toast: null,
  smtp: {}, connectorSettings: {}, connectorSaving: false, smtpSaving: false,
  credentialSource: null, savedCredentialKeys: [],
  settingsLoading: false, settingsError: '',
  connectorSaveState: 'idle', connectorSaveError: '', smtpSaveState: 'idle', smtpSaveError: '',
  showSmtpPassword: false, showChannelSmtpPassword: false, form: emptyForm,
  setForm: vi.fn(), setSmtp: vi.fn(), setConnectorSettings: vi.fn(),
  setShowSmtpPassword: vi.fn(), setShowChannelSmtpPassword: vi.fn(),
  loadChannels: vi.fn(resolveNothing), loadSmtp: vi.fn(resolveNothing), refresh: vi.fn(resolveNothing),
  openCreate: vi.fn(), openEdit: vi.fn(resolveNothing), closeModal: vi.fn(), resetCredentialState: vi.fn(), showToast: vi.fn(),
  handleSave: vi.fn(resolveNothing), handleDelete: vi.fn(resolveNothing),
  handleToggleEnabled: vi.fn(resolveNothing), handleTest: vi.fn(resolveNothing),
  handleSaveSmtp: vi.fn(resolveNothing), handleSaveConnector: vi.fn(resolveNothing),
  resetConnectorSaveState: vi.fn(), resetSmtpSaveState: vi.fn(),
  ...overrides,
});

describe('Connectors 页面', /* 当前测试组验证连接器页面的外壳、读取态、错误态与刷新语义。 */ () => {
  beforeEach(/* resetHookMock 清空上一条用例的调用记录。 */ () => {
    useConnectorsMock.mockReset();
  });

  test('管理员视角使用统一外壳并在两个区块各放一条保存条', /* 当前测试验证 PageShell/PageHeader 与区块级保存条落位。 */ () => {
    useConnectorsMock.mockReturnValue(createState());
    render(<Connectors isAdmin />);
    expect(useConnectorsMock).toHaveBeenCalledWith(true);
    expect(screen.getByRole('heading', { name: '连接器' })).toBeTruthy();
    expect(screen.getByText('配置第三方平台连接器与通知渠道，账号异常时主动推送告警')).toBeTruthy();
    // 管理员在同一分区内既看到「系统级配置」总标题，也看到归入其中的平台连接器与系统邮件子标题。
    expect(screen.getByText('系统级配置')).toBeTruthy();
    expect(screen.getByText('系统邮件（SMTP）')).toBeTruthy();
    expect(screen.getByText('平台连接器')).toBeTruthy();
    expect(screen.getByText('保存连接器配置')).toBeTruthy();
    expect(screen.getByText('修改平台凭据或能力开关后需保存')).toBeTruthy();
    expect(screen.getByText('保存 SMTP 配置')).toBeTruthy();
    expect(screen.getByRole('button', { name: /刷新/ })).toBeTruthy();
  });

  test('系统设置读取中时区块显示加载中且不展示未配置徽章', /* 当前测试验证读取中先于就绪判定，避免误报未配置。 */ () => {
    useConnectorsMock.mockReturnValue(createState({ settingsLoading: true }));
    render(<Connectors isAdmin />);
    expect(screen.getAllByText('加载中...').length).toBe(2);
    expect(screen.queryByTestId('connector-status-qqbot')).toBeNull();
    // refreshButton 是读取中的页头刷新按钮，必须禁用以免重复触发。
    expect((screen.getByRole('button', { name: /刷新/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('系统设置读取失败时错误在区块可见并可重新读取', /* 当前测试验证静默失败已被可见错误与重试入口替代。 */ () => {
    // state 是系统设置读取失败场景的页面状态。
    const state = createState({ settingsError: '读取系统配置失败' });
    useConnectorsMock.mockReturnValue(state);
    render(<Connectors isAdmin />);
    expect(screen.getAllByText('读取系统配置失败').length).toBe(2);
    fireEvent.click(screen.getAllByRole('button', { name: '重新读取' })[0]);
    expect(state.loadSmtp).toHaveBeenCalledTimes(1);
  });

  test('页头刷新按管理员语义触发整体重载', /* 当前测试验证刷新按钮绑定 Hook 的 refresh 而不是只刷渠道。 */ () => {
    // state 是可点击刷新场景的页面状态。
    const state = createState();
    useConnectorsMock.mockReturnValue(state);
    render(<Connectors isAdmin />);
    fireEvent.click(screen.getByRole('button', { name: /刷新/ }));
    expect(state.refresh).toHaveBeenCalledTimes(1);
  });

  test('普通用户视角不渲染平台连接器与 SMTP 区块', /* 当前测试验证系统级配置的权限边界未被外壳改造破坏。 */ () => {
    useConnectorsMock.mockReturnValue(createState());
    render(<Connectors />);
    expect(useConnectorsMock).toHaveBeenCalledWith(false);
    // 普通用户只看到自己可操作的通知渠道分区，系统级分区的标题也不能出现。
    expect(screen.getByText('通知渠道')).toBeTruthy();
    expect(screen.queryByText('系统级配置')).toBeNull();
    expect(screen.queryByText('系统邮件（SMTP）')).toBeNull();
    expect(screen.queryByText('平台连接器')).toBeNull();
    expect(screen.queryByText('保存连接器配置')).toBeNull();
    expect(screen.queryByText('保存 SMTP 配置')).toBeNull();
  });
});
