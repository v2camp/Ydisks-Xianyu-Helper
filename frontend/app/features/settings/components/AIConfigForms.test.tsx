// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, test } from 'vitest';
import { useState } from 'react';
import type { ReactNode } from 'react';
import type { SystemSettings } from '../api';
import {
  FAQ_RECOMMENDED,
  INTENT_TEMPLATES,
  KnowledgeConfigForm,
  NEGATIVE_RECOMMENDED,
  PolicyConfigForm,
  ScopeConfigForm,
} from './AIConfigForms';

afterEach(/* 当前回调隔离每个用例创建的 DOM。 */ () => cleanup());

// FormHarnessProps 描述 AI 配置表单受控测试容器的输入。
interface FormHarnessProps {
  /** initial 是渲染前的系统配置草稿初值。 */
  initial: SystemSettings;
  /** configKey 是底部展示草稿原文的配置键名。 */
  configKey: string;
  /** children 用当前草稿渲染被测表单。 */
  children: (settings: SystemSettings, onChange: (patch: Partial<SystemSettings>) => void) => ReactNode;
}

// FormHarness 是 AI 配置表单的受控测试容器，保存草稿并展示指定键的序列化结果。
function FormHarness({ initial, configKey, children }: FormHarnessProps) {
  // settings 是受控的系统配置草稿状态。
  const [settings, setSettings] = useState<SystemSettings>(initial);
  // mergePatch 合并表单写回的字段补丁到草稿状态。
  const mergePatch = (patch: Partial<SystemSettings>) => setSettings(/* prev 是合并补丁前的草稿，返回合并后的新草稿。 */ prev => ({ ...prev, ...patch }));
  return (
    <div>
      {children(settings, mergePatch)}
      <pre data-testid="draft">{typeof settings[configKey] === 'string' ? settings[configKey] : ''}</pre>
    </div>
  );
}

// parseDraft 读取测试容器底部展示的配置草稿 JSON 对象。
function parseDraft(): Record<string, unknown> {
  return JSON.parse(screen.getByTestId('draft').textContent || '{}');
}

// clickJsonToggle 点击右上角「JSON」按钮进入高级视图。
function clickJsonToggle() {
  fireEvent.click(screen.getByRole('button', { name: 'JSON' }));
}

// clickFormToggle 点击右上角「表单」按钮尝试切回表单视图。
function clickFormToggle() {
  fireEvent.click(screen.getByRole('button', { name: '表单' }));
}

describe('PolicyConfigForm', /* 当前测试组验证策略话术表单的推荐填充、序列化与双视图。 */ () => {
  test('空话术展示推荐填充并写入已知字段', /* 当前测试验证「填入推荐话术」按钮。 */ () => {
    render(
      <FormHarness initial={{}} configKey="ai_policy_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <PolicyConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.click(screen.getByRole('button', { name: /填入推荐话术/ }));
    // draft 是填充后的策略配置对象。
    const draft = parseDraft();
    expect(String(draft.min_price_reply)).toContain('{amount}');
    expect(String(draft.no_discount_reply)).toContain('最低价');
    expect(screen.queryByRole('button', { name: /填入推荐话术/ })).toBeNull();
  });

  test('编辑话术只改已知字段并保留未知键', /* 当前测试验证未知键保留。 */ () => {
    render(
      <FormHarness initial={{ ai_policy_config: JSON.stringify({ min_price_reply: '旧', no_discount_reply: '旧2', custom_extra: 'keep' }) }} configKey="ai_policy_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <PolicyConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.change(screen.getByLabelText('最低价回复'), { target: { value: '新话术' } });
    // draft 是编辑后的策略配置对象。
    const draft = parseDraft();
    expect(draft.min_price_reply).toBe('新话术');
    expect(draft.no_discount_reply).toBe('旧2');
    expect(draft.custom_extra).toBe('keep');
  });

  test('表单与 JSON 切换共用草稿且非法 JSON 禁止切回', /* 当前测试验证双视图切换与非法 JSON 拦截。 */ () => {
    render(
      <FormHarness initial={{ ai_policy_config: JSON.stringify({ min_price_reply: '甲', no_discount_reply: '' }) }} configKey="ai_policy_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <PolicyConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    clickJsonToggle();
    // jsonBox 是 JSON 高级视图的编辑框。
    const jsonBox = screen.getByRole('textbox') as HTMLTextAreaElement;
    expect(JSON.parse(jsonBox.value).min_price_reply).toBe('甲');
    fireEvent.change(jsonBox, { target: { value: '{ 坏掉的 JSON' } });
    clickFormToggle();
    expect(screen.getByText('JSON 格式非法，修正后才能切回表单')).toBeTruthy();
    expect(screen.getByRole('textbox')).toBe(jsonBox);
    fireEvent.change(jsonBox, { target: { value: '{"min_price_reply":"修好了","no_discount_reply":""}' } });
    clickFormToggle();
    expect(screen.getByLabelText('最低价回复')).toBeTruthy();
  });
});

describe('KnowledgeConfigForm', /* 当前测试组验证语料 FAQ 的推荐填充、增删行与 catalog 兼容保留。 */ () => {
  test('填入推荐语料只写 FAQ 并支持增删 FAQ 行', /* 当前测试验证推荐填充不含 catalog 且列表可增删。 */ () => {
    // recommendedAnswers 是推荐 FAQ 回答拼接文本，用于断言不含旧的全局发货承诺。
    const recommendedAnswers = FAQ_RECOMMENDED.map(/* row 是推荐 FAQ 行，取回答作文案断言。 */ row => row.answer).join('\n');
    expect(recommendedAnswers).not.toContain('本店支持百度网盘');
    expect(FAQ_RECOMMENDED.some(/* row 判断是否存在 catalog 相关分类。 */ row => row.category.includes('在售'))).toBe(false);
    render(
      <FormHarness initial={{}} configKey="ai_knowledge_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <KnowledgeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.click(screen.getByRole('button', { name: /填入推荐语料/ }));
    // draft 是填充后的语料配置对象。
    const draft = parseDraft();
    expect(Array.isArray(draft.faq)).toBe(true);
    expect((draft.faq as unknown[]).length).toBeGreaterThan(0);
    // 推荐语料不再写入 catalog，在售商品与库存由系统动态查询注入。
    expect(draft.catalog).toBeUndefined();
    // faqRows 是填充后的 FAQ 行列表，用于断言新推荐文案。
    const faqRows = draft.faq as Array<Record<string, unknown>>;
    // 发货答复按商品区分方式，不再写死全局网盘清单。
    expect(String(faqRows[0].answer)).toContain('不同商品发货方式不同');
    expect(String(faqRows[0].answer)).not.toContain('本店支持百度网盘');
    expect(String(faqRows[0].match)).toContain('怎么发');
    expect(String(faqRows[0].match)).toContain('什么网盘');
    // 退款答复与官方规则对齐：非质量问题不支持退款，失效可补发。
    expect(String(faqRows[2].answer)).toContain('非质量问题不支持退款');
    expect(String(faqRows[2].answer)).toContain('补发');
    fireEvent.click(screen.getByRole('button', { name: '添加 FAQ' }));
    expect((parseDraft().faq as unknown[]).length).toBe((draft.faq as unknown[]).length + 1);
    fireEvent.click(screen.getByRole('button', { name: '删除 FAQ 第 1 行' }));
    expect((parseDraft().faq as unknown[]).length).toBe((draft.faq as unknown[]).length);
  });

  test('表单不提供在售清单编辑器', /* 当前测试验证 catalog 编辑器已移除。 */ () => {
    render(
      <FormHarness initial={{ ai_knowledge_config: JSON.stringify({ faq: [{ category: '发货', match: '百度', answer: '答案' }] }) }} configKey="ai_knowledge_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <KnowledgeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    expect(screen.queryByText('在售清单')).toBeNull();
    expect(screen.queryByRole('button', { name: '添加商品' })).toBeNull();
    expect(screen.getByRole('button', { name: '添加 FAQ' })).toBeTruthy();
  });

  test('旧配置含 catalog 时表单编辑不丢键', /* 当前测试验证旧 catalog 在表单往返中原样保留。 */ () => {
    // legacyCatalog 是旧配置里的在售清单数据，表单不展示但必须原样保留。
    const legacyCatalog = [{ title: '糯糯下山', detail: '1-3季全' }];
    render(
      <FormHarness initial={{ ai_knowledge_config: JSON.stringify({ faq: [{ category: '发货', match: '百度', answer: '答案' }], catalog: legacyCatalog }) }} configKey="ai_knowledge_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <KnowledgeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.change(screen.getByLabelText('第 1 行分类'), { target: { value: '物流' } });
    // draft 是编辑后的语料配置对象。
    const draft = parseDraft();
    expect(draft.catalog).toEqual(legacyCatalog);
    fireEvent.click(screen.getByRole('button', { name: '添加 FAQ' }));
    expect(parseDraft().catalog).toEqual(legacyCatalog);
  });

  test('编辑 FAQ 行保留行内未知键', /* 当前测试验证行级未知键保留。 */ () => {
    render(
      <FormHarness initial={{ ai_knowledge_config: JSON.stringify({ faq: [{ category: '发货', match: '百度', answer: '答案', priority: 9 }], catalog: [] }) }} configKey="ai_knowledge_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <KnowledgeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.change(screen.getByLabelText('第 1 行分类'), { target: { value: '物流' } });
    // firstFaq 是编辑后的第一条 FAQ 行。
    const firstFaq = (parseDraft().faq as Array<Record<string, unknown>>)[0];
    expect(firstFaq.category).toBe('物流');
    expect(firstFaq.priority).toBe(9);
    expect(firstFaq.answer).toBe('答案');
  });
});

describe('ScopeConfigForm', /* 当前测试组验证边界意图表单与常用意图芯片。 */ () => {
  test('常用意图芯片插入意图行并可编辑负向词', /* 当前测试验证模板芯片与负向词编辑。 */ () => {
    render(
      <FormHarness initial={{}} configKey="ai_scope_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <ScopeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.click(screen.getByRole('button', { name: '+ 砍价' }));
    // draft 是插入芯片后的边界配置对象。
    const draft = parseDraft();
    // intents 是插入后的意图列表。
    const intents = draft.intents as Array<Record<string, unknown>>;
    expect(intents[0].id).toBe('bargain');
    expect(intents[0].enabled).toBe(true);
    expect(intents[0].ai).toBe(true);
    fireEvent.change(screen.getByLabelText('负向词'), { target: { value: '退款|投诉' } });
    expect(parseDraft().negative).toBe('退款|投诉');
  });

  test('意图行支持改 ID 与开关并保留未知键', /* 当前测试验证意图字段编辑与未知键保留。 */ () => {
    render(
      <FormHarness initial={{ ai_scope_config: JSON.stringify({ intents: [{ id: 'bargain', enabled: true, ai: true, match: '便宜', weight: 3 }], negative: '退款' }) }} configKey="ai_scope_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <ScopeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.change(screen.getByLabelText('第 1 行意图 ID'), { target: { value: 'deal' } });
    fireEvent.click(screen.getByLabelText('AI 接管'));
    // firstIntent 是编辑后的第一条意图行。
    const firstIntent = (parseDraft().intents as Array<Record<string, unknown>>)[0];
    expect(firstIntent.id).toBe('deal');
    expect(firstIntent.ai).toBe(false);
    expect(firstIntent.weight).toBe(3);
    expect(parseDraft().negative).toBe('退款');
  });

  test('JSON 非法时禁止切回表单', /* 当前测试验证边界块的非法 JSON 拦截。 */ () => {
    render(
      <FormHarness initial={{ ai_scope_config: '{"intents":[]}' }} configKey="ai_scope_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <ScopeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    clickJsonToggle();
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '不是 JSON' } });
    clickFormToggle();
    expect(screen.getByText('JSON 格式非法，修正后才能切回表单')).toBeTruthy();
  });

  test('推荐负向词覆盖站外引流与维权处罚词', /* 当前测试验证负向词推荐值含新增高风险词。 */ () => {
    // negativeTerms 是负向词推荐值按分隔符拆出的词表。
    const negativeTerms = NEGATIVE_RECOMMENDED.split('|');
    // 每个词都必须出现在推荐负向词中，覆盖引流、线下、司法与侵权风险。
    for (const term /* term 是待断言的新增负向高风险词。 */ of ['微信', '加微', '加V', '维信', '薇信', '二维码', '扫码', '线下', '站外', '起诉', '法院', '报警', '消协', '工商', '12315', '侵权', '盗版']) {
      expect(negativeTerms).toContain(term);
    }
    // 既有售后维权词必须保留。
    for (const term /* term 是待断言的既有售后维权负向词。 */ of ['退款', '投诉', '维权', '违规', '扣分', '封号', '申诉']) {
      expect(negativeTerms).toContain(term);
    }
  });

  test('发货意图模板含发货方式与时效词', /* 当前测试验证发货意图 match 追加了发货问答词。 */ () => {
    // deliveryTemplate 是发货意图模板，管发货方式与时效类提问。
    const deliveryTemplate = INTENT_TEMPLATES.find(/* template 按 ID 找出发货意图模板。 */ template => template.id === 'delivery');
    expect(deliveryTemplate).toBeTruthy();
    // matchWords 是发货意图匹配规则拆出的词表。
    const matchWords = String(deliveryTemplate?.match).split('|');
    for (const term /* term 是待断言的发货方式与时效追问词。 */ of ['怎么发', '什么时候发', '多久发', '发货方式', '什么网盘']) {
      expect(matchWords).toContain(term);
    }
  });

  test('插入发货意图后草稿 match 含新词', /* 当前测试验证芯片插入后草稿落地新 match 词。 */ () => {
    render(
      <FormHarness initial={{}} configKey="ai_scope_config">
        {/* childRenderer 用当前草稿渲染被测表单组件。 */ (settings, onChange) => <ScopeConfigForm settings={settings} onChange={onChange} />}
      </FormHarness>,
    );
    fireEvent.click(screen.getByRole('button', { name: '+ 发货' }));
    // intentMatch 是插入后的发货意图匹配规则原文。
    const intentMatch = String((parseDraft().intents as Array<Record<string, unknown>>)[0].match);
    expect(intentMatch).toContain('怎么发');
    expect(intentMatch).toContain('发货方式');
    expect(intentMatch).toContain('什么网盘');
  });
});
