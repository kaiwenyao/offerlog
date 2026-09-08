// Full E2E acceptance: 新增岗位 → 准备材料 → 上传简历 → 投递 → 沟通 → 面试 →
// Offer → 接受/撤回 (§2.3 主流程验收) with reload persistence and consistency.
const { chromium } = require('playwright');
// The target-status field is a custom blueprint listbox (ds/Listbox), not a
// native <select>: open the trigger, then click the option by its data-value.
// The panel is portaled to document.body (so the dialog's overflow can't clip
// it), hence the option selector is NOT scoped to `.modal`.
async function pickTarget(page, value) {
  await page.click('.modal button.status-target-trigger');
  await page.click(`[role=option][data-value="${value}"]`);
}

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
  // 注册开放（测试栈固定 REGISTRATION_OPEN=true）→ 用唯一邮箱注册一次性账号
  // （重跑免清库）；关闭注册时回退到预建账号（E2E_EMAIL/E2E_PASSWORD）登录。
  // 两条路都打印出来：静默回退会把「api 镜像过期（没有 /auth/config）」伪装成
  // 登录失败超时，难以排查（PR #11 CI 实测）。
  const regTab = page.locator('button[role=tab]:has-text("注册")');
  if (await regTab.count()) {
    console.log('0 register tab found: signing up a one-off account');
    await regTab.click();
    await page.fill('input[type=email]', email.replace(/^(.+?)@/, `$1.${Date.now()}@`));
    await page.fill('input[type=password]', password);
  } else {
    console.log('0 register tab NOT found (registration closed or stale api image): falling back to E2E_EMAIL login');
    await page.fill('input[type=email]', email);
    await page.fill('input[type=password]', password);
  }
  await page.click('button[type=submit]');
  try {
    await page.waitForSelector('text=今日待办', { timeout: 15000 });
  } catch {
    // 带出表单上的错误文案，避免只留一个裸超时。
    const form = await page.locator('form').innerText().catch(() => '');
    throw new Error('login/register did not reach dashboard; form says: '
      + form.replace(/\s+/g, ' ').trim().slice(0, 200));
  }

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
  await pickTarget(page, 'applied');
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
    await pickTarget(page, to);
    if (extra?.reason) await page.fill('.modal textarea[placeholder="必填"]', extra.reason);
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

  // 8. skip-ahead: a brand-new record goes 待投递 -> 面试中 in one step, with
  //    the interview round scheduled inline from the same dialog.
  const skipCo = 'E2E-Skip-' + Date.now();
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector('text=求职数据库');
  await page.click('button:has-text("＋ 新增岗位")');
  await page.fill('#cf-company', skipCo);
  await page.fill('#cf-pos', '数据工程师');
  await page.click('button:has-text("创建")');
  await page.waitForSelector(`text=${skipCo}`, { timeout: 7000 });
  await page.locator(`tbody tr:has-text("${skipCo}")`).first().click();
  await page.waitForSelector('.drawer >> text=更新进度');
  await page.click('.drawer >> text=更新进度');
  await page.waitForSelector('.modal >> text=目标状态');
  await pickTarget(page, 'interviewing');           // only reachable after the skip-ahead change
  await page.fill('.modal input[type=datetime-local] >> nth=1', '2026-09-02T09:00'); // submitted_at
  await page.fill('.modal input[type=datetime-local] >> nth=2', '2026-09-20T14:00'); // inline round
  await page.click('.modal button:has-text("确认更新")');
  await page.waitForTimeout(1600);
  const skipChip = (await page.locator('.drawer .status-chip').textContent()).replace(/\s+/g,' ');
  console.log('8a skip-ahead chip:', skipChip);
  await page.click('.drawer >> text=概览');
  await page.waitForTimeout(600);
  const overviewTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('8b inline round created (一面):', overviewTxt.includes('一面'));

  // 8c. The timeline must show the BUSINESS times the user typed, not the DB
  //     write clock: 投递 was entered as 2026-09-02, so a 已投递 row has to carry
  //     that day even though the record was created and advanced just now.
  await page.click('.drawer >> text=时间线');
  await page.waitForTimeout(600);
  const tlTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('8c timeline has 建档 row:', tlTxt.includes('建档'));
  console.log('8c timeline shows the entered 投递 day (09/02):',
    /待投递\s*→\s*已投递/.test(tlTxt) && tlTxt.includes('09/02'));

  // 9. ended records stay editable: the button becomes 重开 / 更正.
  //    Close the open drawer first — its backdrop swallows row clicks.
  await page.click('.drawer button[aria-label="关闭"]');
  await page.waitForTimeout(400);
  await page.locator(`tbody tr:has-text("${company}")`).first().click();
  await page.waitForTimeout(600);
  const reopenVisible = await page.locator('.drawer button:has-text("重开 / 更正")').count();
  console.log('9 ended record offers reopen:', reopenVisible > 0);

  console.log('E2E JS errors:', errors.length ? errors : 'none');
  await browser.close();
})().catch((e) => { console.error('E2E FAIL', e.message); process.exit(1); });
