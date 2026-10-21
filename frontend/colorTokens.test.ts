import { readdirSync,readFileSync } from 'node:fs';
import { relative,resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

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

// paletteClassPattern 匹配 Tailwind 调色板类：前缀允许 hover:/md: 等变体与任意 utility 名，
// 色板只收录非中性族。中性族（gray/slate/zinc/neutral/stone）与语义令牌（brand/danger-*/success-*/warning-*/ink/canvas）不在其中。
const paletteClassPattern = /\b[a-z][a-z0-9]*(?:-[a-z0-9]+)*-(?:blue|sky|violet|indigo|emerald|amber|green|red|purple|rose|orange|teal|cyan|lime|fuchsia|pink)-\d{2,3}\b/g;

// paletteScannedSources 是调色板门禁的扫描范围：feature 页面与共享 UI 的生产 TSX。
// 共享 UI 的令牌层 tokens.ts 不在此列，它是唯一允许持有调色板类字的文件。
const paletteScannedSources = [...collectTSXSources(featureRoot), ...collectTSXSources(sharedUIRoot)];

// paletteBaseline 是 colorPaletteBaseline.json 的内容：相对 frontend 根目录的源码路径 → 该文件当前的调色板类出现次数。
// 该基线由现有代码快照生成（54 个文件、780 处），用于把存量调色板类冻结在「不再增加」的状态，允许后续批次逐文件替换为语义令牌。
// 生成方式：用与下方 paletteClassPattern 完全相同的正则，统计 app/features 与 shared/ui 下非测试 TSX 的命中次数，按路径排序写回该 JSON。
// 只有在确认文件已被清理后才手工下调对应数字，禁止为了让新代码通过而抬高基线。
const paletteBaseline: Record<string, number> = JSON.parse(readFileSync(resolve(__dirname, 'colorPaletteBaseline.json'), 'utf8'));

// countPaletteClasses 统计单个源码文件中非中性调色板类的出现次数，返回相对路径与计数。
const countPaletteClasses = (filePath: string): { file: string; count: number } => {
  const source = readFileSync(filePath, 'utf8'); /* source 是待统计的源码文本。 */
  return { file: relative(__dirname, filePath).split('\\').join('/'), count: [...source.matchAll(paletteClassPattern)].length };
};

describe('global color token contract', () => {
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
    // actualCounts 是本次扫描得到的每个文件的调色板类出现次数。
    const actualCounts = new Map(paletteScannedSources.map(countPaletteClasses).map(/* pairMapper 把单文件统计结果转成「路径 → 次数」的映射项。 */ item => [item.file, item.count]));
    // regressions 汇总超出基线的文件：历史调色板类只允许减少，不允许新增。
    const regressions = [...actualCounts.entries()]
      .filter(/* regressionFilter 只保留实际出现次数高于基线的文件。 */ ([file, count]) => count > (paletteBaseline[file] ?? 0))
      .map(/* regressionFormatter 把超出基线的文件整理成「实际 / 基线」对照文本。 */ ([file, count]) => `${file}: 实际 ${count} 处，基线 ${paletteBaseline[file] ?? 0} 处`);
    // 新增调色板类的失败信息必须直接给出整改路径，避免开发者用改基线的方式绕过门禁。
    const remediation = '请改用语义令牌（brand/success-*/warning-*/danger-*/ink/canvas 或 shared/ui/tokens.ts）；如确需下调基线请手工更新该文件 colorPaletteBaseline.json';
    // shrunkenFiles 是已经低于基线的文件，仅作下降提示，不参与断言。
    const shrunkenFiles = [...actualCounts.entries()]
      .filter(/* shrinkFilter 只保留实际出现次数低于基线的文件。 */ ([file, count]) => count < (paletteBaseline[file] ?? 0))
      .map(/* shrinkFormatter 把低于基线的文件整理成「实际 / 基线」对照文本。 */ ([file, count]) => `${file}: 实际 ${count} 处，基线 ${paletteBaseline[file] ?? 0} 处`);
    if (shrunkenFiles.length > 0) {
      console.info(`[color-tokens] 下列文件的调色板类已低于基线，可手工下调 colorPaletteBaseline.json：\n${shrunkenFiles.join('\n')}`);
    }

    expect(regressions, remediation).toEqual([]);
  } /* 测试回调断言没有任何文件新增非中性调色板类。 */);
} /* 测试套件回调汇总页面颜色令牌的静态契约。 */);
