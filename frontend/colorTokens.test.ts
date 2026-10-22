import { readdirSync,readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';
// @ts-ignore 门禁脚本是 .mjs 且没有类型声明；测试只复用其导出的正则与扫描函数，以保证脚本与测试同口径。
import { collectRegressions,collectShrunken,createPaletteClassPattern,readPaletteBaseline,scanPaletteUsage } from './scripts/color-baseline.mjs';

// featureRoot 是 feature 页面与专属组件所在的目录；根 components 已由阶段七迁移移除。
const featureRoot = resolve(__dirname, 'app/features');
// sharedUIRoot 是跨 feature 复用的纯展示组件目录。
const sharedUIRoot = resolve(__dirname, 'shared/ui');
// collectTSXSources 递归收集目标目录内非测试 TSX，供设计令牌契约审计使用。
const collectTSXSources = (directory: string): string[] => readdirSync(directory, { withFileTypes: true }).flatMap(/* sourceEntryMapper 处理当前目录的一个文件或子目录。 */ entry => {
  // entryPath 是当前文件系统条目的绝对路径。
  const entryPath = resolve(directory, entry.name);
  if (entry.isDirectory()) return collectTSXSources(entryPath);
  return entry.name.endsWith('.tsx') && !entry.name.endsWith('.test.tsx') ? [entryPath] : [];
});
// pageSources 是根应用、feature 页面和共享 UI 的生产 TSX 源码集合。
const pageSources = [
  resolve(__dirname, 'App.tsx'),
  ...collectTSXSources(featureRoot),
  ...collectTSXSources(sharedUIRoot),
];

// rgb(var(--color-*)), CSS classes and the central index.css are references.
// Literal colors in page code would bypass the design-token system.
const hardCodedColorPattern = /#[0-9a-f]{3,8}\b|rgba?\((?!var\(--color-)/gi; /* hardCodedColorPattern 表示hardCodedColorPattern。 */

// 调色板正则只由 scripts/color-baseline.mjs 导出，测试复用同一工厂函数做静态契约自检，
// 防止有人只改一处正则后门禁与基线口径脱节。
const paletteClassPattern = createPaletteClassPattern();

// paletteBaseline 是 colorPaletteBaseline.json 的内容：相对 frontend 根目录的源码路径 → 该文件当前的调色板类出现次数。
// 口径演进：最初仅统计 .tsx（54 文件 780 处），扩展到 .ts 后为 58 文件 845 处，随后逐批替换清零。
// 生成方式：node scripts/color-baseline.mjs --write（与下方断言使用同一份正则与扫描函数）。
// 只有在确认文件已被清理后才手工下调对应数字，禁止为了让新代码通过而抬高基线。
const paletteBaseline: Record<string, number> = readPaletteBaseline();

// PaletteBaselineDiff 描述单个文件的调色板类实际数与基线，供失败与提示信息统一格式化。
interface PaletteBaselineDiff {
  // file 是相对 frontend 根目录的源码路径。
  file: string;
  // count 是本次扫描到的调色板类出现次数。
  count: number;
  // baseline 是基线文件中该文件的冻结次数。
  baseline: number;
}

describe('global color token contract', () => {
  test('palette scanner and test share the same pattern', () => {
    // 正则必须保留全局标志，否则 matchAll 无法统计同一文件内的多处命中。
    expect(paletteClassPattern.flags).toBe('g');
    // 代表性样本覆盖带变体前缀、任意 utility 名与非中性色族三种情况；变体前缀（hover:）不计入命中。
    expect('hover:bg-purple-100 text-rose-500'.match(paletteClassPattern)).toEqual(['bg-purple-100', 'text-rose-500']);
    // 语义令牌与中性色板不得被调色板门禁误判。
    expect('bg-brand-100 text-gray-500 bg-success-100'.match(paletteClassPattern)).toBeNull();
  } /* 测试回调断言门禁正则与脚本口径一致，且不误伤语义令牌。 */);

  test('page components do not contain hard-coded color values', () => {
    const violations = pageSources.flatMap((filePath) => {
      const source = readFileSync(filePath, 'utf8'); /* source 表示source。 */
      return [...source.matchAll(hardCodedColorPattern)].map((match) => `${filePath}:${match[0]}` /* match 是命中的硬编码颜色及其源码位置。 */);
    } /* flatMap 回调读取单个页面源码并收集其中的颜色令牌违规项。 */); /* violations 汇总每个页面绕过设计令牌的颜色值。 */

    expect(violations).toEqual([]);
  } /* 测试回调断言页面源码不直接写入色值。 */);

  test('the primary brand and highlight colors are defined in the central stylesheet', () => {
    const globalStyles = readFileSync(resolve(__dirname, 'index.css'), 'utf8'); /* globalStyles 表示globalStyles。 */

    expect(globalStyles).toContain('--color-brand: 0 148 247;');
    expect(globalStyles).toContain('--color-brand-highlight: 0 113 227;');
    expect(globalStyles).toContain('--color-success-500:');
    expect(globalStyles).toContain('--color-warning-500:');
    expect(globalStyles).toContain('--color-danger-500:');
  } /* 测试回调断言全局样式提供全部核心色彩令牌。 */);

  test('palette classes must not exceed the frozen baseline', () => {
    // usage 是本次扫描结果：扫描范围含 .ts 与 .tsx，已排除测试文件与 shared/ui/tokens.ts。
    const usage = scanPaletteUsage();    // regressions 汇总超出基线的文件：历史调色板类只允许减少，不允许新增。
    const regressions = collectRegressions(usage, paletteBaseline);
    // 新增调色板类的失败信息必须直接给出整改路径，避免开发者用改基线的方式绕过门禁。
    const remediation = '请改用语义令牌（brand/success-*/warning-*/danger-*/ink/canvas 或 shared/ui/tokens.ts）；如确需下调基线请手工更新该文件 colorPaletteBaseline.json';
    // shrunkenFiles 是已经低于基线的文件，仅作下降提示，不参与断言。
    const shrunkenFiles = collectShrunken(usage, paletteBaseline);
    if (shrunkenFiles.length > 0) {
      console.info(`[color-tokens] 下列文件的调色板类已低于基线，可手工下调 colorPaletteBaseline.json：\n${shrunkenFiles.map(/* shrinkFormatter 把低于基线的文件整理成「实际 / 基线」对照文本。 */ (item: PaletteBaselineDiff) => `${item.file}: 实际 ${item.count} 处，基线 ${item.baseline} 处`).join('\n')}`);
    }

    expect(regressions.map(/* regressionFormatter 把超出基线的文件整理成「实际 / 基线」对照文本。 */ (item: PaletteBaselineDiff) => `${item.file}: 实际 ${item.count} 处，基线 ${item.baseline} 处`), remediation).toEqual([]);
  } /* 测试回调断言没有任何文件新增非中性调色板类。 */);

  test('palette baseline stays empty after cleanup', () => {
    // 调色板类已全量清零，基线为空是当前不变量：任何新增都必须改用语义令牌，
    // 而不是把新债务写进基线。确需冻结历史债务时，必须先在设计评审里说明原因。
    expect(Object.keys(paletteBaseline)).toEqual([]);
  } /* 测试回调锁住「基线为空」这一状态，防止悄悄抬高基线。 */);
} /* 测试套件回调汇总页面颜色令牌的静态契约。 */);
