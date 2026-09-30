import { describe, expect, test } from 'vitest';
import { collectOtherTemplateMessages, isDuplicateAcrossTemplates } from './copyRisk';

// templatesFixture 是话术重复判定使用的多模板夹具。
const templatesFixture = [
  { id: 1, messages: [{ content: '感谢购买！' }, { content: '卡密是 {{cards.main}}' }] },
  { id: 2, messages: [{ content: '欢迎再来～' }] },
];

describe('collectOtherTemplateMessages', /* 当前测试组验证其它模板消息收集范围。 */ () => {
  test('编辑中模板自身的消息不参与比对', /* 当前测试验证排除当前模板后的消息集合。 */ () => {
    expect(collectOtherTemplateMessages(templatesFixture, 1)).toEqual(['欢迎再来～']);
    expect(collectOtherTemplateMessages(templatesFixture, 2)).toEqual(['感谢购买！', '卡密是 {{cards.main}}']);
  });

  test('新建模板时全部模板都算其它模板', /* 当前测试验证 editingID 为空时的收集范围。 */ () => {
    expect(collectOtherTemplateMessages(templatesFixture, null)).toEqual(['感谢购买！', '卡密是 {{cards.main}}', '欢迎再来～']);
  });
});

describe('isDuplicateAcrossTemplates', /* 当前测试组验证话术重复警示的命中与不命中。 */ () => {
  // otherMessages 是其它模板消息集合。
  const otherMessages = collectOtherTemplateMessages(templatesFixture, 1);

  test('trim 后完全一致命中警示', /* 当前测试验证重复话术命中。 */ () => {
    expect(isDuplicateAcrossTemplates('欢迎再来～', otherMessages)).toBe(true);
    expect(isDuplicateAcrossTemplates('  欢迎再来～  ', otherMessages)).toBe(true);
  });

  test('文本不同不命中警示', /* 当前测试验证差异化文案不触发警示。 */ () => {
    expect(isDuplicateAcrossTemplates('欢迎下次光临～', otherMessages)).toBe(false);
    expect(isDuplicateAcrossTemplates('', otherMessages)).toBe(false);
    expect(isDuplicateAcrossTemplates('   ', otherMessages)).toBe(false);
  });

  test('仅空白差异视为重复', /* 当前测试验证 trim 后的完全一致判定。 */ () => {
    // normalizedOthers 是含首尾空白的其它模板消息。
    const normalizedOthers = ['  欢迎再来～\n'];
    expect(isDuplicateAcrossTemplates('欢迎再来～', normalizedOthers)).toBe(true);
  });
});
