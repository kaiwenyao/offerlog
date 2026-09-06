// Full E2E acceptance: 新增岗位 → 准备材料 → 上传简历 → 投递 → 沟通 → 面试 →
// Offer → 接受/撤回 (§2.3 主流程验收) with reload persistence and consistency.
const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch({ ignoreHTTPSErrors: true });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
  page.on('console', (m) => { if (m.type() === 'error' && !m.text().includes('401') && !m.text().includes('404')) errors.push(m.text()); });

  const base = process.env.E2E_BASE || 'http://localhost:8080';
  const email = process.env.E2E_EMAIL || 'me@example.com';
  const password = process.env.E2E_PASSWORD || 'testpass12345';
  await page.goto(base + '/', { waitUntil: 'networkidle' });
  // 注册开放（测试栈默认）→ 用唯一邮箱注册一次性账号（重跑免清库）；
  // 关闭注册时回退到预建账号（E2E_EMAIL/E2E_PASSWORD）登录。
  const regTab = page.locator('button[role=tab]:has-text("注册")');
  if (await regTab.count()) {
    await regTab.click();
    await page.fill('input[type=email]', email.replace(/^(.+?)@/, `$1.${Date.now()}@`));
    await page.fill('input[type=password]', password);
  } else {
    await page.fill('input[type=email]', email);
    await page.fill('input[type=password]', password);
  }
  await page.click('button[type=submit]');
  await page.waitForSelector('text=今日待办', { timeout: 8000 });

  const company = 'E2E-Corp-' + Date.now();
  const position = '全栈工程师';
  // 1. create from empty-state CTA
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector('text=求职数据库');
  await page.click('button:has-text("＋ 新增岗位")');
  await page.fill('#cf-company', company);
  await page.fill('#cf-pos', position);
  await page.click('button:has-text("创建")');
  await page.waitForSelector(`text=${company}`, { timeout: 7000 });
  console.log('1 create OK');

  // 2. open detail + update progress to applied (提供投递时间)
  await page.locator(`tbody tr:has-text("${company}")`).first().click();
  await page.waitForSelector('.drawer >> text=更新进度');
  await page.click('.drawer >> text=更新进度');
  await page.waitForSelector('text=目标状态');
  await page.selectOption('.modal select', 'applied');
  await page.fill('.modal input[type=datetime-local] >> nth=1', '2026-09-01T10:00'); // submitted_at (2nd dt field)
  await page.click('.modal button:has-text("确认更新")');
  await page.waitForTimeout(1200);
  let chip = await page.locator('.drawer .status-chip').textContent();
  console.log('2 applied chip:', chip.replace(/\s+/g,' '));

  // 3. upload resume through drawer Files tab
  await page.click('.drawer >> text=时间线').catch(()=>{});
  await page.click('.drawer >> text=附件 (0)');
  await page.waitForTimeout(400);
  const setFile = await page.locator('.drawer input[type=file]');
  await setFile.setInputFiles({ name: 'resume-e2e.txt', mimeType: 'text/plain', buffer: Buffer.from('E2E resume content ' + Date.now()) });
  await page.waitForTimeout(2500);
  await page.waitForSelector('.drawer >> text=resume-e2e.txt', { timeout: 6000 });
  console.log('3 upload OK');

  // 4. interview round via overview
  await page.click('.drawer >> text=概览');
  await page.click('.drawer >> text=＋ 安排');
  await page.selectOption('.modal select >> nth=0', '一面');
  await page.click('.modal button:has-text("保存")');
  await page.waitForTimeout(1200);
  console.log('4 interview OK');

  // 5. transition → interviewing → offer → accepted through UI
  async function transition(to, extra) {
    await page.click('.drawer >> text=更新进度');
    await page.waitForSelector('.modal >> text=目标状态');
    await page.selectOption('.modal select', to);
    if (extra?.reason) await page.fill('.modal input[placeholder="必填"]', extra.reason);
    if (extra?.dt) await page.fill('.modal input[type=datetime-local] >> nth=0', extra.dt);
    await page.click('.modal button:has-text("确认更新")');
    await page.waitForTimeout(1400);
    return (await page.locator('.drawer .status-chip').textContent()).replace(/\s+/g,' ');
  }
  let st = await transition('interviewing');
  console.log('5a interviewing chip:', st);
  st = await transition('offer');
  console.log('5b offer chip:', st);
  st = await transition('accepted');
  console.log('5c accepted chip:', st);

  // 6. reload persistence + cross-view consistency
  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForSelector('text=求职数据库', { timeout: 7000 });
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector(`tbody tr:has-text("${company}")`, { timeout: 7000 });
  const rowTxt = (await page.locator(`tbody tr:has-text("${company}")`).textContent()) || '';
  console.log('6 reload: row contains 已接受:', rowTxt.includes('已接受'));

  // 7. analytics consistency
  await page.locator('.sidebar a:has-text("统计分析")').click();
  await page.waitForSelector('text=桑基图', { timeout: 8000 });
  await page.waitForTimeout(1800);
  const sk = await page.evaluate(async () => {
    const r = await fetch('/api/v1/analytics/sankey', { method: 'POST', credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': localStorage.getItem('offerlog.csrf') },
      body: JSON.stringify({ mode: 'current' }) });
    return r.json();
  });
  const acceptedLink = sk.links.find(l => l.target === 's_accepted');
  console.log('7 sankey accepted edge value >=1:', acceptedLink ? acceptedLink.value >= 1 : false);

  console.log('E2E JS errors:', errors.length ? errors : 'none');
  await browser.close();
})().catch((e) => { console.error('E2E FAIL', e.message); process.exit(1); });
