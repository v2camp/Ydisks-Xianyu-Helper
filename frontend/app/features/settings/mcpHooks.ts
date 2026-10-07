import { useCallback,useEffect,useRef,useState } from 'react';
import type { MCPAuditPage,MCPServiceMessage,MCPServiceStatus } from './api';
import { generateMCPToken,getMCPAudit,getMCPServiceStatus,revokeMCPToken,updateMCPServiceSettings,type MCPSettingsUpdate } from './api';
import { MCP_AUDIT_PAGE_SIZE,createMCPServiceMessage,isSettingsAbortError,settingsErrorMessage } from './state';

/** MCP 服务卡片的 Hook 返回值。 */
export interface UseMCPServiceResult {
  /** 当前 MCP 服务非敏感状态；未加载成功时为 null。 */
  status: MCPServiceStatus | null;
  /** 状态是否正在加载。 */
  statusLoading: boolean;
  /** 状态加载失败信息；空串表示无错误。 */
  statusError: string;
  /** 启停或网络策略是否正在保存。 */
  saving: boolean;
  /** 最近一次操作结果提示。 */
  message: MCPServiceMessage | null;
  /** 仅在生成后短暂保留在页面内的一次性令牌明文；空串表示没有。 */
  token: string;
  /** 令牌生成或吊销是否进行中。 */
  tokenLoading: boolean;
  /** 最近一次审计分页结果。 */
  audit: MCPAuditPage | null;
  /** 审计是否正在加载。 */
  auditLoading: boolean;
  /** 审计加载失败信息；空串表示无错误。 */
  auditError: string;
  /** 重新读取 MCP 服务状态。 */
  reloadStatus: () => void;
  /** 保存启用开关与网络策略。 */
  submitSettings: (update: MCPSettingsUpdate) => Promise<void>;
  /** 生成或轮换持久化令牌。 */
  generateToken: () => Promise<void>;
  /** 吊销全部持久化令牌。 */
  revokeToken: () => Promise<void>;
  /** 加载指定页的调用审计，页码从 1 开始。 */
  loadAudit: (page: number) => void;
  /** 清除页面上保留的一次性令牌明文。 */
  dismissToken: () => void;
}

/** 管理 MCP 服务状态、启停策略、令牌生命周期与调用审计；一次令牌明文只保留到管理员主动清除。 */
export const useMCPService = (): UseMCPServiceResult => {
  // status 保存 MCP 服务非敏感状态；未加载成功时为 null。
  const [status, setStatus] = useState<MCPServiceStatus | null>(null);
  // statusLoading 表示状态是否正在加载。
  const [statusLoading, setStatusLoading] = useState(false);
  // statusError 保存状态加载失败信息。
  const [statusError, setStatusError] = useState('');
  // saving 表示启停或网络策略是否正在保存。
  const [saving, setSaving] = useState(false);
  // message 保存最近一次操作结果提示。
  const [message, setMessage] = useState<MCPServiceMessage | null>(null);
  // token 是仅在生成后短暂保留在页面内的一次性明文。
  const [token, setToken] = useState('');
  // tokenLoading 表示令牌生成或吊销是否进行中。
  const [tokenLoading, setTokenLoading] = useState(false);
  // audit 保存最近一次审计分页结果。
  const [audit, setAudit] = useState<MCPAuditPage | null>(null);
  // auditLoading 表示审计是否正在加载。
  const [auditLoading, setAuditLoading] = useState(false);
  // auditError 保存审计加载失败信息。
  const [auditError, setAuditError] = useState('');
  // statusSequence 隔离状态刷新产生的旧响应。
  const statusSequence = useRef(0);
  // statusController 保存当前可取消的状态请求。
  const statusController = useRef<AbortController | null>(null);
  // auditSequence 隔离审计分页产生的旧响应。
  const auditSequence = useRef(0);
  // auditController 保存当前可取消的审计请求。
  const auditController = useRef<AbortController | null>(null);
  // actionController 保存当前可取消的令牌生成或吊销请求。
  const actionController = useRef<AbortController | null>(null);

  /** 读取 MCP 服务状态，并用代次与取消信号拒绝过期响应。 */
  const reloadStatus = useCallback(/* 当前回调封装状态读取的取消和代次隔离。 */ () => {
    statusController.current?.abort();
    // controller 是本次状态请求的取消控制器。
    const controller = new AbortController();
    statusController.current = controller;
    statusSequence.current += 1;
    // sequence 是本次状态请求的代次，用于丢弃晚到响应。
    const sequence = statusSequence.current;
    setStatusLoading(true);
    setStatusError('');
    void getMCPServiceStatus({ signal: controller.signal })
      .then(/* response 是状态接口返回的 UI 模型。 */ response => {
        if (sequence !== statusSequence.current || controller.signal.aborted) return;
        setStatus(response);
      })
      .catch(/* error 是状态读取失败或取消原因。 */ error => {
        if (sequence !== statusSequence.current || isSettingsAbortError(error)) return;
        setStatusError(settingsErrorMessage(error, '读取 MCP 服务状态失败'));
      })
      .finally(/* 当前回调在状态请求结束后收敛加载标记。 */ () => {
        if (sequence === statusSequence.current) setStatusLoading(false);
      });
  }, []);

  /** 加载指定页的调用审计，并拒绝过期响应。 */
  const loadAudit = useCallback(/* 当前回调封装审计分页读取的取消和代次隔离。 */ (page: number) => {
    auditController.current?.abort();
    // controller 是本次审计请求的取消控制器。
    const controller = new AbortController();
    auditController.current = controller;
    auditSequence.current += 1;
    // sequence 是本次审计请求的代次，用于丢弃晚到响应。
    const sequence = auditSequence.current;
    setAuditLoading(true);
    setAuditError('');
    void getMCPAudit({ page, pageSize: MCP_AUDIT_PAGE_SIZE }, { signal: controller.signal })
      .then(/* pageResult 是审计接口返回的分页 UI 模型。 */ pageResult => {
        if (sequence !== auditSequence.current || controller.signal.aborted) return;
        setAudit(pageResult);
      })
      .catch(/* error 是审计读取失败或取消原因。 */ error => {
        if (sequence !== auditSequence.current || isSettingsAbortError(error)) return;
        setAuditError(settingsErrorMessage(error, '读取 MCP 调用审计失败'));
      })
      .finally(/* 当前回调在审计请求结束后收敛加载标记。 */ () => {
        if (sequence === auditSequence.current) setAuditLoading(false);
      });
  }, []);

  /** 首次进入设置页时并行加载状态与首页审计；卸载时中止全部在途请求。 */
  useEffect(/* 当前回调在挂载时触发一次初始加载并在卸载时中止请求。 */ () => {
    reloadStatus();
    loadAudit(1);
    return () => {
      statusController.current?.abort();
      auditController.current?.abort();
      actionController.current?.abort();
    }; // 卸载回调中止全部在途请求，避免晚到响应写入已卸载组件。
  }, [loadAudit, reloadStatus]);

  /** 保存启用开关与网络策略；成功后重新拉取状态确认生效。 */
  const submitSettings = useCallback(/* 当前回调封装 MCP 启停与网络策略保存流程。 */ async (update: MCPSettingsUpdate) => {
    setSaving(true);
    setMessage(null);
    try {
      await updateMCPServiceSettings(update);
      setMessage(createMCPServiceMessage('success', 'MCP 服务设置已保存'));
      reloadStatus();
    } catch (error /* error 是 MCP 设置保存失败原因。 */) {
      if (isSettingsAbortError(error)) return;
      setMessage(createMCPServiceMessage('error', settingsErrorMessage(error, '保存 MCP 服务设置失败')));
    } finally {
      setSaving(false);
    }
  }, [reloadStatus]);

  /** 生成或轮换持久化令牌，并把一次性明文保留在当前页面状态。 */
  const generateToken = useCallback(/* 当前回调封装 MCP 令牌生成与轮换流程。 */ async () => {
    actionController.current?.abort();
    // controller 是本次令牌请求的取消控制器。
    const controller = new AbortController();
    actionController.current = controller;
    setTokenLoading(true);
    setMessage(null);
    try {
      // plaintext 是只在本次响应出现一次的令牌明文。
      const plaintext = await generateMCPToken({ signal: controller.signal });
      setToken(plaintext);
      setMessage(createMCPServiceMessage('success', '已生成新令牌，请立即复制并妥善保存'));
      reloadStatus();
    } catch (error /* error 是令牌生成失败原因。 */) {
      if (isSettingsAbortError(error)) return;
      setMessage(createMCPServiceMessage('error', settingsErrorMessage(error, '生成 MCP 令牌失败')));
    } finally {
      setTokenLoading(false);
    }
  }, [reloadStatus]);

  /** 吊销全部持久化令牌，并清除页面上保留的明文。 */
  const revokeToken = useCallback(/* 当前回调封装 MCP 令牌吊销流程。 */ async () => {
    actionController.current?.abort();
    // controller 是本次吊销请求的取消控制器。
    const controller = new AbortController();
    actionController.current = controller;
    setTokenLoading(true);
    setMessage(null);
    try {
      await revokeMCPToken({ signal: controller.signal });
      setToken('');
      setMessage(createMCPServiceMessage('success', '已吊销全部持久化令牌'));
      reloadStatus();
    } catch (error /* error 是令牌吊销失败原因。 */) {
      if (isSettingsAbortError(error)) return;
      setMessage(createMCPServiceMessage('error', settingsErrorMessage(error, '吊销 MCP 令牌失败')));
    } finally {
      setTokenLoading(false);
    }
  }, [reloadStatus]);

  /** 清除页面上保留的一次性令牌明文。 */
  const dismissToken = useCallback(/* 当前回调清除一次性令牌明文。 */ () => setToken(''), []);

  return {
    status, statusLoading, statusError, saving, message, token, tokenLoading,
    audit, auditLoading, auditError,
    reloadStatus, submitSettings, generateToken, revokeToken, loadAudit, dismissToken,
  };
};
