import { readdirSync,readFileSync,statSync } from 'node:fs';
import { resolve } from 'node:path';
import { describe,expect,test } from 'vitest';

// lazyPageNames 是产生独立分片的页面名唯一来源，分片识别与预算登记都以它为准。
import { lazyPageNames } from './app/router/pageEntries';

// staticRoot 是 Go 服务嵌入的前端生产资源目录。
const staticRoot = resolve(__dirname, '../internal/webui/static');
// indexHtml 是生产构建生成的入口 HTML。
const indexHtml = readFileSync(resolve(staticRoot, 'index.html'), 'utf8');
// PAGE_CHUNK_BUDGETS 定义每个业务页面动态分片允许的原始字节上限。
const PAGE_CHUNK_BUDGETS: Record<string, number> = {
  // Dashboard 页面承载统计卡与筛选区；语义令牌化后实测 30382 字节，余量过窄故上调。
  Dashboard: 32 * 1024,
  // AccountList 页面承载账号编辑弹窗、全局居中对话框服务与账号级客服 Agent 授权控件，实测 75104 字节，预算随之上调。
  AccountList: 76 * 1024,
  OrderList: 40 * 1024,
  CardList: 45 * 1024,
  ItemList: 65 * 1024,
  // Settings 页面承载基础设置与安全阀门；MCP 服务卡与连接器迁出后实测 15937 字节，预算随之下调。
  Settings: 18 * 1024,
  // AISettings 页面承载模型凭据、接管范围编辑与外部 MCP 工具；改用共享骨架后实测 24535 字节。
  AISettings: 27 * 1024,
  // AgentCenter 页面承接 MCP 服务卡、三类 Agent 入口与前置依赖状态，实测 21009 字节，预算随之上调。
  AgentCenter: 26 * 1024,
  // Rules 页面包含模板发货模式入口、模板变量编辑器与全局居中对话框调用点。
  Rules: 73 * 1024,
  // DeliveryTemplates 页面承载模板 CRUD 编辑器；新增重复文案提醒与预览弹窗后实测 12940 字节，预算随之上调。
  DeliveryTemplates: 14 * 1024,
  // Connectors 页面承载连接器卡、渠道编辑器、SMTP、页面分区与按类型的凭据继承提示，实测 52683 字节。
  Connectors: 54 * 1024,
  Chat: 50 * 1024,
};

// pageChunkPattern 由懒加载页面名派生，页面清单增删时分片识别自动跟随，不需要手工维护正则。
const pageChunkPattern = new RegExp(`^(?:${lazyPageNames.join('|')})-.+\\.js$`);

describe('frontend production bundle boundary', /* 当前回调验证生产入口包体和页面分片。 */ () => {
  test('入口脚本保持轻量且业务页面不被预加载', /* 当前回调验证首屏入口不同步载入所有页面。 */ () => {
    // entryMatch 保存入口脚本的静态资源匹配结果。
    const entryMatch = indexHtml.match(/src="\/static\/assets\/(index-[^"]+\.js)"/);
    expect(entryMatch).not.toBeNull();
    // entryBytes 保存入口 JavaScript 的原始字节数。
    const entryBytes = statSync(resolve(staticRoot, 'assets', entryMatch![1])).size;
    expect(entryBytes).toBeLessThan(100 * 1024);
    // preloadedAssets 保存 HTML 声明的模块预加载资源。
    const preloadedAssets = [...indexHtml.matchAll(/modulepreload[^>]+href="\/static\/assets\/([^"]+)"/g)].map(/* 当前回调提取模块预加载文件名。 */ match => match[1]);
    expect(preloadedAssets.some(/* 当前回调判断 React 运行时是否被预加载。 */ asset => asset.startsWith('react-vendor-'))).toBe(true);
    expect(preloadedAssets.some(/* 当前回调判断图表依赖是否被首屏预加载。 */ asset => asset.startsWith('charts-vendor-'))).toBe(false);
  });

  test('十二个业务页面都生成独立页面 chunk', /* 当前回调验证各页面可按路由延迟下载。 */ () => {
    // pageChunkNames 保存 Vite 输出的业务页面 chunk 文件名。
    const pageChunkNames = readdirSync(resolve(staticRoot, 'assets')).filter(/* 当前回调筛选业务页面分片文件。 */ fileName => pageChunkPattern.test(fileName));
    expect(pageChunkNames).toHaveLength(12);
  });

  test('每个产生独立分片的页面都在分片预算表登记', /* 当前回调验证页面清单与预算表不会互相缺漏。 */ () => {
    // missingBudgets 保存出现在懒加载清单却没有预算条目的页面名。
    const missingBudgets = lazyPageNames.filter(/* 当前回调筛选未在预算表登记的页面。 */ pageName => PAGE_CHUNK_BUDGETS[pageName] === undefined);
    expect(missingBudgets).toEqual([]);
  });

  test('每个业务页面 chunk 都保持在独立预算内', /* 当前回调验证单个页面不会重新膨胀首屏后的按需下载。 */ () => {
    // assetNames 保存当前生产构建输出的所有静态资源文件名。
    const assetNames = readdirSync(resolve(staticRoot, 'assets'));
    for (const budget /* budget 表示一个页面分片的预算条目。 */ of Object.entries(PAGE_CHUNK_BUDGETS)) {
      // pageName 表示预算对应的页面名称。
      const pageName = budget[0];
      // maxBytes 表示该页面分片允许的最大原始字节数。
      const maxBytes = budget[1];
      // pageChunkNames 保存该页面匹配到的生产分片文件。
      const pageChunkNames = assetNames.filter(/* 当前回调筛选当前页面的动态分片。 */ fileName => fileName.startsWith(`${pageName}-`) && fileName.endsWith('.js'));
      expect(pageChunkNames, `${pageName} 应只有一个动态分片`).toHaveLength(1);
      // actualBytes 保存当前页面分片的实际原始字节数。
      const actualBytes = statSync(resolve(staticRoot, 'assets', pageChunkNames[0])).size;
      expect(actualBytes, `${pageName} 分片超过 ${maxBytes} 字节预算`).toBeLessThanOrEqual(maxBytes);
    }
  });
});
