// slaConfig 管理发货 SLA 设置键 delivery_sla_config 的解析与序列化；只处理配置形状，不发起请求。

/** 发货 SLA 配置：默认分钟数与按账号覆盖，0 分钟表示关闭。 */
export type DeliverySlaConfig = {
  /** default_minutes 是默认发货 SLA 分钟数，0 表示关闭。 */
  default_minutes: number;
  /** per_account 以账号 cookie_id 为键覆盖默认分钟数。 */
  per_account: Record<string, number>;
};

/** DEFAULT_DELIVERY_SLA_CONFIG 是未配置时的默认 SLA：整体关闭且无账号覆盖。 */
export const DEFAULT_DELIVERY_SLA_CONFIG: DeliverySlaConfig = {
  default_minutes: 0,
  per_account: {},
};

/** coerceSlaMinutes 归一 SLA 分钟数；非法或负值回退默认，向下取整。 */
const coerceSlaMinutes = (value: unknown, fallback: number): number => {
  // minutes 是数值转换结果，非有限数回退默认。
  const minutes = Number(value);
  if (!Number.isFinite(minutes) || minutes < 0) return fallback;
  return Math.floor(minutes);
};

/** SLA 配置解析结果：可用配置与是否需要提示非法 JSON。 */
export type DeliverySlaParseResult = {
  /** config 是解析成功或回退后的 SLA 配置。 */
  config: DeliverySlaConfig;
  /** invalid 表示原文非法需要向用户提示；缺失或空白不算非法。 */
  invalid: boolean;
};

/** parseDeliverySlaConfig 解析 delivery_sla_config JSON 原文；缺失按默认展示，非法回退默认并要求提示。 */
export const parseDeliverySlaConfig = (raw: unknown): DeliverySlaParseResult => {
  // isBlank 表示服务端尚未写过该键或值为空白文本，此时按默认配置展示且无需告警。
  const isBlank = raw === undefined || raw === null || (typeof raw === 'string' && raw.trim() === '');
  if (isBlank) return { config: { ...DEFAULT_DELIVERY_SLA_CONFIG, per_account: {} }, invalid: false };
  if (typeof raw !== 'string') return { config: { ...DEFAULT_DELIVERY_SLA_CONFIG, per_account: {} }, invalid: true };
  try {
    // parsed 是解析后的 JSON 值，只有普通对象形态才继续读取已知字段。
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return { config: { ...DEFAULT_DELIVERY_SLA_CONFIG, per_account: {} }, invalid: true };
    }
    // source 是可安全取字段的 SLA JSON 对象。
    const source = parsed as Record<string, unknown>;
    // perAccount 是逐项校验后的账号覆盖表；非法条目直接丢弃。
    const perAccount: Record<string, number> = {};
    if (source.per_account && typeof source.per_account === 'object' && !Array.isArray(source.per_account)) {
      for (const /* entry 是账号覆盖表中的单个键值对。 */ entry of Object.entries(source.per_account as Record<string, unknown>)) {
        // cookieId 是覆盖项对应的账号标识，空键不参与覆盖。
        const cookieId = entry[0];
        // minutes 是覆盖项分钟数，非法值丢弃该条目。
        const minutes = coerceSlaMinutes(entry[1], -1);
        if (cookieId && minutes >= 0) perAccount[cookieId] = minutes;
      }
    }
    return {
      config: {
        default_minutes: coerceSlaMinutes(source.default_minutes, DEFAULT_DELIVERY_SLA_CONFIG.default_minutes),
        per_account: perAccount,
      },
      invalid: false,
    };
  } catch {
    return { config: { ...DEFAULT_DELIVERY_SLA_CONFIG, per_account: {} }, invalid: true };
  }
};

/** serializeDeliverySlaConfig 将 SLA 配置序列化为设置保存使用的 JSON 字符串。 */
export const serializeDeliverySlaConfig = (config: DeliverySlaConfig): string =>
  JSON.stringify({
    default_minutes: coerceSlaMinutes(config.default_minutes, DEFAULT_DELIVERY_SLA_CONFIG.default_minutes),
    per_account: config.per_account,
  });
