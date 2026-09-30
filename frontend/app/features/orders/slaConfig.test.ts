import { describe, expect, test } from 'vitest';
import { DEFAULT_DELIVERY_SLA_CONFIG, parseDeliverySlaConfig, serializeDeliverySlaConfig } from './slaConfig';

describe('parseDeliverySlaConfig', /* 当前测试组验证 delivery_sla_config JSON 的解析与非法回退。 */ () => {
  test('缺失或空白按默认关闭展示且不告警', /* 当前测试验证未配置场景的默认回退。 */ () => {
    expect(parseDeliverySlaConfig(undefined)).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: false });
    expect(parseDeliverySlaConfig('')).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: false });
    expect(parseDeliverySlaConfig('   ')).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: false });
    expect(parseDeliverySlaConfig(null)).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: false });
  });

  test('合法 JSON 解析默认分钟数与账号覆盖', /* 当前测试验证合法配置的字段读取。 */ () => {
    // parsed 是含默认 30 分钟与两个账号覆盖的解析结果。
    const parsed = parseDeliverySlaConfig(JSON.stringify({ default_minutes: 30, per_account: { acc1: 45, acc2: 0 } }));
    expect(parsed.invalid).toBe(false);
    expect(parsed.config.default_minutes).toBe(30);
    expect(parsed.config.per_account).toEqual({ acc1: 45, acc2: 0 });
  });

  test('非法 JSON 回退默认并要求提示', /* 当前测试验证非法原文的回退与告警标记。 */ () => {
    // parsed 是非法 JSON 的解析结果。
    const parsed = parseDeliverySlaConfig('{not json');
    expect(parsed.invalid).toBe(true);
    expect(parsed.config).toEqual(DEFAULT_DELIVERY_SLA_CONFIG);
  });

  test('非对象 JSON 回退默认并要求提示', /* 当前测试验证数组或标量 JSON 的回退。 */ () => {
    expect(parseDeliverySlaConfig('[1,2]')).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: true });
    expect(parseDeliverySlaConfig('42')).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: true });
    expect(parseDeliverySlaConfig(42)).toEqual({ config: DEFAULT_DELIVERY_SLA_CONFIG, invalid: true });
  });

  test('非法分钟数回退默认并丢弃非法覆盖条目', /* 当前测试验证数值字段的容错归一。 */ () => {
    // parsed 是含非法默认值与非法覆盖值的解析结果。
    const parsed = parseDeliverySlaConfig(JSON.stringify({ default_minutes: -5, per_account: { good: 20, bad: 'x' } }));
    expect(parsed.invalid).toBe(false);
    expect(parsed.config.default_minutes).toBe(0);
    expect(parsed.config.per_account).toEqual({ good: 20 });
  });
});

describe('serializeDeliverySlaConfig', /* 当前测试组验证 SLA 配置序列化往返。 */ () => {
  test('序列化后可原样解析回来', /* 当前测试验证保存与读取的往返一致性。 */ () => {
    // config 是待往返的 SLA 配置。
    const config = { default_minutes: 15, per_account: { acc1: 60 } };
    // raw 是序列化后的 JSON 串。
    const raw = serializeDeliverySlaConfig(config);
    expect(JSON.parse(raw)).toEqual(config);
    expect(parseDeliverySlaConfig(raw)).toEqual({ config, invalid: false });
  });

  test('负数默认值在序列化时归零', /* 当前测试验证保存路径同样拒绝非法分钟数。 */ () => {
    expect(JSON.parse(serializeDeliverySlaConfig({ default_minutes: -3, per_account: {} }))).toEqual({ default_minutes: 0, per_account: {} });
  });
});
