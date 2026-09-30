// copyRisk 提供发货模板话术重复风控的纯判断逻辑，不依赖 React 与网络。

/** 重复话术判定可见的最小模板消息形状。 */
interface CopyRiskMessage {
  /** content 是消息正文原文。 */
  content: string;
}

/** 重复话术判定可见的最小模板形状。 */
interface CopyRiskTemplate {
  /** id 是模板主键，用于排除正在编辑的模板。 */
  id: number;
  /** messages 是模板的全部消息。 */
  messages: CopyRiskMessage[];
}

/** collectOtherTemplateMessages 收集除当前编辑模板外其它模板的全部消息原文。 */
export const collectOtherTemplateMessages = (
  /** allTemplates 是当前已加载的全部模板。 */
  allTemplates: ReadonlyArray<CopyRiskTemplate>,
  /** editingID 是正在编辑的模板 ID；空值表示新建，此时全部模板都算其它模板。 */
  editingID: number | null,
): string[] => {
  // messages 收集其它模板的消息原文，顺序保持模板与消息的原始顺序。
  const messages: string[] = [];
  for (const /* template 是待筛选的模板。 */ template of allTemplates) {
    if (template.id === editingID) continue;
    for (const /* message 是其它模板中的单条消息。 */ message of template.messages) {
      messages.push(message.content);
    }
  }
  return messages;
};

/** isDuplicateAcrossTemplates 判断一行文本（trim 后）是否与其它模板任一消息完全一致。 */
export const isDuplicateAcrossTemplates = (
  /** content 是当前编辑行的消息文本。 */
  content: string,
  /** otherMessages 是其它模板的全部消息原文。 */
  otherMessages: readonly string[],
): boolean => {
  // normalized 是去除首尾空白后的当前行文本；空文本不参与重复判定。
  const normalized = content.trim();
  if (!normalized) return false;
  return otherMessages.some(/* other 是待比对的其它模板消息原文。 */ other => other.trim() === normalized);
};

/** DUPLICATE_COPY_WARNING 是话术与其它模板完全一致时的行旁警示文案。 */
export const DUPLICATE_COPY_WARNING = '与其它模板文案完全一致，高频重复发送易触发营销风控';

/** COPY_RISK_BANNER_TEXT 是模板编辑器顶部固定提示条文案。 */
export const COPY_RISK_BANNER_TEXT = '固定话术高频逐字重复易被平台判为营销信息，建议同义微调、避免群发感。';
