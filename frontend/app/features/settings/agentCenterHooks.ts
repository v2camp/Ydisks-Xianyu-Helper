import { useCallback,useEffect,useRef,useState } from 'react';
import { isQQConnectorReady } from '../../../shared/api-contract/settings';
import type { DependencyState } from '../../../shared/ui/DependencyRow';
import { getAgentSupportSettings,getMCPServiceStatus,getSystemSettings,updateAgentSupportSettings } from './api';
import { createMCPServiceMessage,isCurrentSettingsRequest,isSettingsAbortError,settingsErrorMessage } from './state';
import type { AgentSupportSettings,AgentSupportSettingsUpdate,MCPServiceMessage } from './models';

/** AgentDependencies 汇总三类 Agent 共同依赖的 AI 能力与外部通道现状。 */
export interface AgentDependencies {
  /** aiModel 表示默认 AI 模型与密钥是否都已生效。 */
  aiModel: DependencyState;
  /** aiScope 表示 AI 接管范围是否显式配置；留空是合法默认，回落内置边界。 */
  aiScope: DependencyState;
  /** aiTools 表示是否至少配置了一个外部 MCP 工具服务。 */
  aiTools: DependencyState;
  /** qqConnector 表示 QQ 机器人凭据是否已配好，决定运营 Agent 能否启用。 */
  qqConnector: DependencyState;
}

// AI_SCOPE_SETTING_KEY 是边界配置 JSON 的设置键，留空表示沿用内置接管边界。
const AI_SCOPE_SETTING_KEY = 'ai_scope_config';
// MCP_SERVERS_SETTING_KEY 是外部 MCP 工具服务的设置键，值为 [{"name":"...","url":"..."}] 的 JSON 字符串。
const MCP_SERVERS_SETTING_KEY = 'mcp.servers';

/** countMCPServers 统计 mcp.servers 中可用的服务条目数量；空串、非法 JSON 与非数组一律视为 0。 */
const countMCPServers = (raw: unknown): number => {
  if (typeof raw !== 'string' || raw.trim() === '') return 0;
  try {
    // parsed 是解析后的设置值，只有数组形态才可能是服务列表。
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.length : 0;
  } catch {
    return 0;
  }
};

/** deriveAgentDependencies 依据一次系统设置读取结果推导四条依赖状态，判定规则与连接器页保持一致。 */
const deriveAgentDependencies = (settings: Record<string, unknown>): AgentDependencies => ({
  // 模型与密钥缺一不可：只填模型没有密钥同样调不通，反之亦然。
  aiModel: settings.ai_api_key_configured === true && String(settings.ai_model ?? '').trim() !== '' ? 'ready' : 'missing',
  // 接管范围留空是允许的默认值，此时引擎使用内置边界，因此是回落态而不是缺失态。
  aiScope: String(settings[AI_SCOPE_SETTING_KEY] ?? '').trim() !== '' ? 'ready' : 'fallback',
  // 至少一个外部 MCP 服务才算具备工具能力；空串与非法 JSON 都按未配置处理。
  aiTools: countMCPServers(settings[MCP_SERVERS_SETTING_KEY]) >= 1 ? 'ready' : 'missing',
  // QQ 凭据判定与运营 Agent 的启用前提一致，由跨 feature 的共享判定函数给出。
  qqConnector: isQQConnectorReady(settings) ? 'ready' : 'missing',
});

/** Agent 中心页的 Hook 返回值。 */
export interface UseAgentCenterResult {
  /** settings 是租户级客服 Agent 编辑草稿；未加载成功时为 null。 */
  settings: AgentSupportSettings | null;
  /** loading 表示租户配置是否正在加载。 */
  loading: boolean;
  /** loadError 保存租户配置加载失败信息；空串表示无错误。 */
  loadError: string;
  /** saving 表示租户配置是否正在保存。 */
  saving: boolean;
  /** saveMessage 保存最近一次提交的结果提示；null 表示本次进入页面尚未提交过。 */
  saveMessage: MCPServiceMessage | null;
  /** harnessEnabled 表示外部 Harness 依赖的 MCP 服务是否已启用。 */
  harnessEnabled: boolean;
  /** harnessEndpoint 是外部 Harness 的接入地址；未启用或读取失败时为空串。 */
  harnessEndpoint: string;
  /** harnessLoading 表示 Harness 接入状态是否正在加载。 */
  harnessLoading: boolean;
  /** dependencies 保存三类 Agent 的前置依赖状态；读取成功前为 null，页面据此展示「读取中」。 */
  dependencies: AgentDependencies | null;
  /** dependenciesLoading 表示依赖状态是否正在读取。 */
  dependenciesLoading: boolean;
  /** dependenciesError 保存依赖状态读取失败信息；空串表示无错误。 */
  dependenciesError: string;
  /** load 重新读取租户级客服 Agent 配置。 */
  load: () => void;
  /** reloadDependencies 重新读取前置依赖状态，供页面刷新与错误重试共用。 */
  reloadDependencies: () => void;
  /** updateDraft 把单字段变化合并进租户配置草稿。 */
  updateDraft: (patch: Partial<AgentSupportSettingsUpdate>) => void;
  /** submit 保存租户配置。 */
  submit: () => Promise<void>;
  /** resetSaveMessage 清空提交结果提示，供保存条成功态回落为待提交时调用。 */
  resetSaveMessage: () => void;
}

/** 管理 Agent 中心的租户配置草稿、保存状态与外部 Harness 接入现状。 */
export const useAgentCenter = (): UseAgentCenterResult => {
  // settings 保存租户级客服 Agent 编辑草稿；未加载成功时为 null。
  const [settings, setSettings] = useState<AgentSupportSettings | null>(null);
  // loading 表示租户配置是否正在加载。
  const [loading, setLoading] = useState(false);
  // loadError 保存租户配置加载失败信息。
  const [loadError, setLoadError] = useState('');
  // saving 表示租户配置是否正在保存。
  const [saving, setSaving] = useState(false);
  // saveMessage 保存最近一次提交的结果提示。
  const [saveMessage, setSaveMessage] = useState<MCPServiceMessage | null>(null);
  // harnessEnabled 表示 MCP 服务是否已启用。
  const [harnessEnabled, setHarnessEnabled] = useState(false);
  // harnessEndpoint 保存外部 Harness 的接入地址。
  const [harnessEndpoint, setHarnessEndpoint] = useState('');
  // harnessLoading 表示 Harness 接入状态是否正在加载。
  const [harnessLoading, setHarnessLoading] = useState(false);
  // dependencies 保存三类 Agent 的前置依赖状态；读取成功前保持 null 以免闪现「未配置」。
  const [dependencies, setDependencies] = useState<AgentDependencies | null>(null);
  // dependenciesLoading 表示依赖状态是否正在读取。
  const [dependenciesLoading, setDependenciesLoading] = useState(false);
  // dependenciesError 保存依赖状态读取失败信息。
  const [dependenciesError, setDependenciesError] = useState('');
  // settingsController 保存当前可取消的租户配置请求。
  const settingsController = useRef<AbortController | null>(null);
  // dependenciesController 保存当前可取消的依赖读取请求；与租户配置请求各自独立，互不打断。
  const dependenciesController = useRef<AbortController | null>(null);
  // dependenciesSequence 递增标记最新的依赖读取代次，用于丢弃被后续刷新取代的响应。
  const dependenciesSequence = useRef(0);

  /** load 读取租户级客服 Agent 配置，并用取消信号拒绝离开页面后到达的响应。 */
  const load = useCallback(/* 当前回调封装租户配置读取的取消与错误收敛。 */ () => {
    settingsController.current?.abort();
    // controller 是本次租户配置请求的取消控制器。
    const controller = new AbortController();
    settingsController.current = controller;
    setLoading(true);
    setLoadError('');
    void getAgentSupportSettings({ signal: controller.signal })
      .then(/* response 是归一后的租户级客服 Agent 配置。 */ response => {
        if (controller.signal.aborted) return;
        setSettings(response);
      })
      .catch(/* error 是租户配置读取失败或取消原因。 */ error => {
        if (controller.signal.aborted || isSettingsAbortError(error)) return;
        setLoadError(settingsErrorMessage(error, '读取客服 Agent 配置失败'));
      })
      .finally(/* 当前回调在租户配置读取结束后收敛加载标记。 */ () => {
        if (!controller.signal.aborted) setLoading(false);
      });
  }, []);

  // mountEffect 在页面挂载时读取配置并把取消控制器交还给卸载清理。
  useEffect(/* effect 在页面挂载时触发首次读取，并在卸载时释放请求。 */ () => {
    load();
    return /* cleanup 在页面卸载时取消尚未完成的租户配置请求。 */ () => settingsController.current?.abort();
  }, [load]);

  /** reloadDependencies 读取一次系统设置并推导四条依赖状态；失败信息写入页面可见状态而不只是打印。 */
  const reloadDependencies = useCallback(/* 当前回调封装依赖读取的取消、代次与错误收敛。 */ () => {
    // 这里沿用本文件的「代次 ref + AbortController」写法而没有使用 commitIfLatest：该函数要求同时比对请求标识与键名，
    // 面向同一区块内多个并发子请求；本处每次只有一个依赖请求，且必须真正中断底层 HTTP 请求，因此保留取消控制器。
    dependenciesController.current?.abort();
    // controller 是本次依赖读取的取消控制器。
    const controller = new AbortController();
    dependenciesController.current = controller;
    // sequence 是本次依赖读取的代次，晚到的旧响应会因代次不符被丢弃。
    const sequence = dependenciesSequence.current + 1;
    dependenciesSequence.current = sequence;
    setDependenciesLoading(true);
    setDependenciesError('');
    void getSystemSettings({ signal: controller.signal })
      .then(/* settings 是归一后的系统设置快照，用于推导依赖状态。 */ response => {
        if (!isCurrentSettingsRequest(dependenciesSequence.current, sequence, controller.signal)) return;
        setDependencies(deriveAgentDependencies(response));
      })
      .catch(/* error 是依赖读取失败或取消原因；取消不提示，真实失败要留在页面上。 */ error => {
        if (controller.signal.aborted || isSettingsAbortError(error)) return;
        setDependenciesError(settingsErrorMessage(error, '读取 Agent 前置依赖失败'));
      })
      .finally(/* 当前回调在依赖读取结束后收敛加载标记。 */ () => {
        if (isCurrentSettingsRequest(dependenciesSequence.current, sequence, controller.signal)) setDependenciesLoading(false);
      });
  }, []);

  // dependenciesEffect 在页面挂载时读取依赖状态，卸载时取消请求以免回写到已离开的页面。
  useEffect(/* effect 在页面挂载时触发依赖读取，并在卸载时释放请求。 */ () => {
    reloadDependencies();
    return /* cleanup 在页面卸载时取消尚未完成的依赖读取请求。 */ () => dependenciesController.current?.abort();
  }, [reloadDependencies]);

  // harnessEffect 只读取一次 Harness 接入现状，失败时保持「未启用」的保守展示。
  useEffect(/* effect 在页面挂载时读取 MCP 服务现状，并在卸载时取消请求。 */ () => {
    // controller 取消页面卸载后不再需要的 MCP 状态请求。
    const controller = new AbortController();
    setHarnessLoading(true);
    void getMCPServiceStatus({ signal: controller.signal })
      .then(/* response 是 MCP 服务的非敏感状态。 */ response => {
        if (controller.signal.aborted) return;
        setHarnessEnabled(response.enabled);
        setHarnessEndpoint(response.endpoint);
      })
      .catch(/* 当前回调吞掉状态读取失败，本栏是只读展示，失败不阻断页面其余部分。 */ () => undefined)
      .finally(/* 当前回调在 MCP 状态读取结束后收敛加载标记。 */ () => {
        if (!controller.signal.aborted) setHarnessLoading(false);
      });
    return /* cleanup 在页面卸载时释放 MCP 状态请求。 */ () => controller.abort();
  }, []);

  /** updateDraft 合并单个字段变化；档位名单始终保留服务端下发的那一份。 */
  const updateDraft = useCallback(/* 当前回调把单字段变化合并进当前草稿。 */ (patch: Partial<AgentSupportSettingsUpdate>) => {
    setSettings(/* updater 基于当前草稿合并补丁，草稿缺失时不重建。 */ current => current ? { ...current, ...patch } : current);
  }, []);

  /** submit 保存租户级客服 Agent 的开关与档位。 */
  const submit = useCallback(/* 当前回调提交租户配置并收敛保存提示。 */ async (): Promise<void> => {
    if (!settings || saving) return;
    setSaving(true);
    setSaveMessage(null);
    try {
      await updateAgentSupportSettings({ enabled: settings.enabled, preset: settings.preset });
      setSaveMessage(createMCPServiceMessage('success', '客服 Agent 配置已保存'));
    } catch (/* error 是保存请求的失败原因，转为页面提示而不改动草稿。 */ error) {
      setSaveMessage(createMCPServiceMessage('error', settingsErrorMessage(error, '保存客服 Agent 配置失败')));
    } finally {
      setSaving(false);
    }
  }, [saving, settings]);

  /** resetSaveMessage 清空最近一次提交的结果提示，使区块级保存条回到待提交态。 */
  const resetSaveMessage = useCallback(/* 当前回调清空保存结果提示，不改动草稿。 */ () => setSaveMessage(null), []);

  return {
    settings, loading, loadError, saving, saveMessage,
    harnessEnabled, harnessEndpoint, harnessLoading,
    dependencies, dependenciesLoading, dependenciesError,
    load, reloadDependencies, updateDraft, submit, resetSaveMessage,
  };
};
