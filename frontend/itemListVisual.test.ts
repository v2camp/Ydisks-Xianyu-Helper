import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

const itemList = readFileSync(resolve(__dirname, 'app/features/items/pages/ItemList.tsx'), 'utf8'); /* itemList 表示当前商品List。 */

describe('item list primary action colors', () => {
  test('uses the shared primary brand color for batch publishing', () => {
    expect(itemList).toContain('bg-brand text-white hover:bg-brand-highlight');
    expect(itemList).not.toContain('bg-brand-600 text-white hover:bg-brand-700');
  } /* 测试回调断言批量发布使用共享品牌主色。 */);

  test('uses the lighter success tone for publishing actions', () => {
    expect(itemList).toContain('bg-success-500 text-white hover:bg-success-600');
    expect(itemList).not.toContain('bg-success-600 text-white hover:bg-success-700');
  } /* 测试回调断言发布操作使用约定的浅色成功令牌。 */);
} /* 测试套件回调汇总商品列表视觉令牌契约。 */);
