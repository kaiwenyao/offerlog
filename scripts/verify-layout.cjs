// Regression check for: clicking view/filter chips must NOT switch the layout.
// (PR #35：点筛选 chip 不再把布局拽到看板，表格 / 看板 / 列表的激活状态由用户掌控)
//
// 用法（不需要后端，API 全部走 Playwright 拦截）：
//   cd frontend && npx vite --port 5173 &
//   cd scripts && node verify-layout.cjs
//
// Runs against `vite dev` with ALL API routes stubbed via Playwright interception,
// so no backend is needed. Verifies in a real browser that:
//   1. default layout is 表格 and the table renders
//   2. clicking 待投递 (board-built-in view chip) keeps 表格 active / table rendered
//   3. clicking 看板 tab renders the board, and clicking 进行中 keeps the board
//   4. clicking a quick filter (高优先级) keeps the board
//   5. layout Tabs themselves still switch layouts
const { chromium } = require('playwright');

const ROW = (id, company, status) => ({
  id,
  company_id: id,
  company_name: company,
  position: `岗位${id}`,
  job_url: '',
  jd_snapshot: '',
  location: '北京',
  remote_policy: '',
  employment_type: '',
  salary_min: 10,
  salary_max: 20,
  salary_currency: 'k',
  channel: 'BOSS',
  status,
  substatus: '',
  focus_activity_kind: '',
  focus_activity_id: null,
  priority: 'medium',
  tags: [],
  custom_values: {},
  notes: '',
  saved_at: '2025-01-01T00:00:00Z',
  submitted_at: null,
  first_response_at: null,
  deadline: '2025-07-01',
  accepted_at: null,
  rejected_at: null,
  reason: '',
  next_action: '',
  next_action_due_at: null,
  version: 1,
  archived: false,
  deleted: false,
  created_at: '2025-01-01T00:00:00Z',
  updated_at: '2025-06-01T00:00:00Z',
  stage_history: {},
});

const ROWS = [
  ROW(1, '字节跳动', 'saved'),
  ROW(2, '腾讯', 'applied'),
  ROW(3, '美团', 'interviewing'),
  ROW(4, '阿里', 'accepted'),
];

const ME = {
  id: 1,
  email: 'demo@offerlog.local',
  display_name: 'Demo',
  timezone: 'Asia/Shanghai',
  locale: 'zh-CN',
  csrf_token: 'stub-csrf',
};

async function main() {
  const base = process.env.BASE ?? 'http://localhost:5173';
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
  page.on('console', (m) => {
    if (m.type() === 'error') errors.push('console: ' + m.text());
  });

  const json = (body) => (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });

  // Playwright 里后注册的 route 优先匹配，所以兜底路由先注册、具体路由后注册。
  await page.route('**/api/**', json({ items: [] }));
  await page.route('**/api/v1/auth/me', json(ME));
  await page.route('**/api/v1/views', json({ items: [] }));
  await page.route('**/api/v1/views/query', json({ items: ROWS, total: ROWS.length }));
  await page.route('**/api/v1/home/summary**', json({ total: ROWS.length, active: ROWS.length, todos: { open: 0 }, items: [] }));
  await page.route('**/api/v1/files', json({ items: [] }));
  await page.route('**/api/v1/notifications**', json({ items: [], unread: 0 }));
  // 空对象会让 hydrateStatusModel 抛异常，必须回 null（旧后端行为）。
  await page.route('**/api/v1/meta/**', (route) => route.fulfill({ status: 200, contentType: 'application/json', body: 'null' }));

  await page.goto(base + '/database', { waitUntil: 'networkidle' });

  const tabSelected = (label) =>
    page.locator(`button[role=tab]:has-text("${label}")`).getAttribute('aria-selected');

  const results = [];
  const check = async (name, cond) => {
    const ok = await cond();
    results.push(`${ok ? 'PASS' : 'FAIL'}  ${name}`);
    if (!ok) process.exitCode = 1;
  };

  await page.waitForSelector('table.tbl');

  await check('默认布局是表格（表格可见，表格 tab 选中）', async () =>
    (await tabSelected('表格')) === 'true' && (await page.locator('table.tbl').count()) === 1);

  // 1. 点内置 board 视图 chip（待投递）→ 布局必须保持表格
  await page.click('span[role=button]:has-text("待投递")');
  await page.waitForTimeout(300);
  await check('点「待投递」筛选 chip 后仍停留在表格布局', async () =>
    (await tabSelected('表格')) === 'true' && (await page.locator('table.tbl').count()) === 1);

  await page.click('span[role=button]:has-text("进行中")');
  await page.waitForTimeout(300);
  await check('点「进行中」筛选 chip 后仍停留在表格布局', async () =>
    (await tabSelected('表格')) === 'true' && (await page.locator('table.tbl').count()) === 1);

  // 2. 切到看板 → 点视图 chip / 快捷筛选，必须保持看板
  await page.click('button[role=tab]:has-text("看板")');
  await page.waitForSelector('.board');
  await check('布局 Tabs 切换到看板仍然有效', async () => (await page.locator('.board').count()) === 1);

  await page.click('span[role=button]:has-text("待投递")');
  await page.waitForTimeout(300);
  await check('看板下点「待投递」chip 后仍保持看板', async () =>
    (await tabSelected('看板')) === 'true' && (await page.locator('.board').count()) === 1);

  await page.click('span[role=button]:has-text("高优先级")');
  await page.waitForTimeout(300);
  await check('看板下点快捷筛选「高优先级」后仍保持看板', async () =>
    (await tabSelected('看板')) === 'true' && (await page.locator('.board').count()) === 1);

  // 3. 布局 Tabs 自身还能来回切
  await page.click('button[role=tab]:has-text("表格")');
  await page.waitForTimeout(300);
  await check('切回表格仍然生效', async () => (await page.locator('table.tbl').count()) === 1);

  const apiErrors = errors.filter((e) => !e.includes('404') && !e.includes('401'));
  if (apiErrors.length) {
    results.push('WARN  console errors: ' + apiErrors.slice(0, 3).join(' | '));
  }

  console.log(results.join('\n'));
  await browser.close();
  if (process.exitCode) console.log('RESULT: FAILED');
  else console.log('RESULT: OK');
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});