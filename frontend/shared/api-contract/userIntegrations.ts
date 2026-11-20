// userIntegrations 公开当前本地用户集成配置的共享传输适配；具体页面只消费它导出的函数与模型。
import { normalizeUserIntegrationSettingsUpdate } from './settings';
import { contractClient,runContractRequest } from './client';
import type { RequestControlOptions } from '../http/client';

/** UserIntegrationSettings 是当前用户 AI、QQ 与 SMTP 集成配置的共享 UI 模型；敏感键只含 configured 标记。 */
export interface UserIntegrationSettings {
  /** 允许承载服务端声明的标量值；敏感明文永远不会进入该模型。 */
  [key: string]: string | number | boolean | undefined;
}

/** AIConnectionTestResult 是用户级 AI 连接测试的非敏感结果。 */
export interface AIConnectionTestResult {
  /** model 是实际测试的模型名称。 */
  model: string;
  /** latency_ms 是请求往返毫秒数。 */
  latency_ms: number;
  /** reply 是模型回复摘要。 */
  reply: string;
}

/** 获取当前用户的 AI、QQ 与 SMTP 集成配置；敏感键只返回 configured 标记。 */
export const getUserIntegrations = async (options?: RequestControlOptions): Promise<UserIntegrationSettings> =>
  runContractRequest(/* signal 控制用户集成配置读取的取消和超时。 */ signal => contractClient.GET('/api/v1/settings/user/integrations', { signal }), options) as unknown as Promise<UserIntegrationSettings>;

/** 保存当前用户的 AI、QQ 与 SMTP 集成配置，敏感键在传输层转换为三态命令。 */
export const updateUserIntegrations = async (settings: UserIntegrationSettings, options?: RequestControlOptions): Promise<{ /** success 表示保存是否完成。 */ success: boolean }> => {
  // payload 是普通值与敏感命令分离后的请求体。
  const payload = normalizeUserIntegrationSettingsUpdate(settings);
  return runContractRequest(/* signal 控制用户集成配置保存的取消和超时。 */ signal => contractClient.PUT('/api/v1/settings/user/integrations', { body: payload, signal }), options);
};

/** UserAIModelsResponse 是用户级模型目录接口的响应。 */
interface UserAIModelsResponse {
  /** models 是服务端返回的模型名称列表。 */
  models?: string[];
}

/** 使用当前用户的配置读取可用模型列表。 */
export const fetchUserAIModels = async (baseURL: string, apiKey = '', options?: RequestControlOptions): Promise<string[]> => {
  // response 是服务端返回的模型目录。
  const response = await runContractRequest(/* signal 控制模型目录读取的取消和超时。 */ signal => contractClient.POST('/api/v1/settings/user/ai-models', { body: { base_url: baseURL, api_key: apiKey }, signal }), options) as unknown as UserAIModelsResponse;
  return Array.isArray(response.models) ? response.models : [];
};

/** 使用当前用户的配置测试 AI 连接。 */
export const testUserAIConnection = async (baseURL: string, apiKey: string, model: string, options?: RequestControlOptions): Promise<AIConnectionTestResult> =>
  runContractRequest(/* signal 控制 AI 连接测试的取消和超时。 */ signal => contractClient.POST('/api/v1/settings/user/ai-test', { body: { base_url: baseURL, api_key: apiKey, model }, signal }), options) as unknown as Promise<AIConnectionTestResult>;
