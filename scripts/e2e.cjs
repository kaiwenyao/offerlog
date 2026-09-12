// Full E2E acceptance for the event-driven timeline (迁移 00006):
// 新增岗位 → 上传简历 → 添加事件（投递 / 面试 / Offer / 接受）→ 重排 → 移除,
// with reload persistence, analytics consistency and the reference flow chart.
//
// 用户不再在一条固定流水线上「更新进度」：他们往时间线上添加事件，岗位状态由
// 「时间线上最后一个事件」推导。所以每个断言的形状都是：加/改/删一个事件 →
// 看抽屉顶部的状态芯片有没有跟上。
//
// ⚠️ Always click the 添加事件 BUTTON (button:has-text), never `text=添加事件`:
// Playwright's text= engine matches substrings, and the drawer footer hint
// (「进度记在「时间线」里：添加事件即可，状态会自动跟上。」) sits in the same
// DOM — a bare text= selector steals the click and leaves the dialog closed.
const { chromium } = require('playwright');

/**
 * Add one timeline event through the drawer. The event-type field is a native
 * <select> (grouped by optgroup), so selectOption works directly — unlike the
 * portaled listbox the old 更新进度 dialog used.
 */
async function addEvent(page, kind, opts = {}) {
  await page.click('.drawer button:has-text("＋ 添加事件")');
  await page.waitForSelector('.modal >> text=事件类型');
  await page.selectOption('.modal select', kind);
  // ds/Input renders a bare <input> with no explicit type, so select by
  // position: 名称 is the first input, 发生时间 the datetime-local one.
  if (opts.label) await page.fill('.modal input >> nth=0', opts.label);
  if (opts.at) await page.fill('.modal input[type=datetime-local]', opts.at);
  if (opts.note) await page.fill('.modal textarea', opts.note);
  await page.click('.modal button:has-text("保存")');
  await page.waitForTimeout(1300);
  return (await page.locator('.drawer .status-chip').textContent()).replace(/\s+/g, ' ');
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

  // 2. 抽屉默认落在时间线；参考流程图可见，「投递」事件把状态带到已投递。
  await page.locator(`tbody tr:has-text("${company}")`).first().click();
  await page.waitForSelector('.drawer button:has-text("＋ 添加事件")');
  const guideTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('2a reference flow chart is visible:', guideTxt.includes('参考流程'));
  console.log('2a flow chart says steps are skippable:', guideTxt.includes('每一步都可跳过'));
  let chip = await addEvent(page, 'apply', { at: '2026-09-01T10:00' });
  console.log('2b applied chip:', chip);

  // 3. upload resume through drawer Files tab
  await page.click('.drawer >> text=附件 (0)');
  await page.waitForTimeout(400);
  const setFile = await page.locator('.drawer input[type=file]');
  await setFile.setInputFiles({ name: 'resume-e2e.txt', mimeType: 'text/plain', buffer: Buffer.from('E2E resume content ' + Date.now()) });
  await page.waitForTimeout(2500);
  await page.waitForSelector('.drawer >> text=resume-e2e.txt', { timeout: 6000 });
  console.log('3 upload OK');

  // 4. interview round via overview (排期 / 提醒 / 日历仍由轮次面板承载)
  await page.click('.drawer >> text=概览');
  await page.click('.drawer >> text=＋ 安排');
  await page.selectOption('.modal select >> nth=0', '一面');
  await page.click('.modal button:has-text("保存")');
  await page.waitForTimeout(1200);
  console.log('4 interview round OK');

  // 5. 面试 → Offer → 接受，全部靠添加事件推导出来
  await page.click('.drawer >> text=时间线');
  await page.waitForTimeout(400);
  chip = await addEvent(page, 'interview', { at: '2026-09-10T14:00' });
  console.log('5a interviewing chip:', chip);
  chip = await addEvent(page, 'offer', { at: '2026-09-18T09:00' });
  console.log('5b offer chip:', chip);
  chip = await addEvent(page, 'accept', { at: '2026-09-20T09:00' });
  console.log('5c accepted chip:', chip);

  // 6. reload persistence + cross-view consistency
  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForSelector('text=求职数据库', { timeout: 7000 });
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector(`tbody tr:has-text("${company}")`, { timeout: 7000 });
  const rowTxt = (await page.locator(`tbody tr:has-text("${company}")`).textContent()) || '';
  console.log('6 reload: row contains 已接受:', rowTxt.includes('已接受'));

  // 7. analytics consistency: 事件驱动的阶段也要进桑基图（视图 application_stage_points）
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

  // 8. 跳过中间步骤：一个全新的岗位直接记一个「面试」，没有 OA、没有初筛。
  const skipCo = 'E2E-Skip-' + Date.now();
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector('text=求职数据库');
  await page.click('button:has-text("＋ 新增岗位")');
  await page.fill('#cf-company', skipCo);
  await page.fill('#cf-pos', '数据工程师');
  await page.click('button:has-text("创建")');
  await page.waitForSelector(`text=${skipCo}`, { timeout: 7000 });
  await page.locator(`tbody tr:has-text("${skipCo}")`).first().click();
  await page.waitForSelector('.drawer button:has-text("＋ 添加事件")');
  const skipChip = await addEvent(page, 'interview', { label: '一面', at: '2026-09-20T14:00' });
  console.log('8a skip-ahead chip (no OA, no 初筛):', skipChip);

  // 8b. 时间线显示用户填的业务时间，不是写库时刻。
  const tlTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('8b timeline has 建档 row:', tlTxt.includes('建档'));
  console.log('8b timeline shows the entered day (09/20):', tlTxt.includes('09/20'));
  console.log('8b node says what it did to the status:', /→\s*面试/.test(tlTxt));

  // 8c. 补录一个更早的「初筛」：时间线重排，但当前状态**不变**（它排在面试之前）。
  const backfillChip = await addEvent(page, 'screen', { at: '2026-09-05T10:00' });
  console.log('8c backfilling an earlier event keeps the status at 面试:', backfillChip);
  const orderTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('8c timeline re-sorted (09/05 before 09/20):',
    orderTxt.indexOf('09/05') < orderTxt.indexOf('09/20'));

  // 9. 移除最后一个事件 → 状态退回上一格（不会停在一个已不存在的阶段里）。
  await page.locator('.drawer button:has-text("移除")').last().click();
  await page.waitForTimeout(1500);
  const afterRemove = (await page.locator('.drawer .status-chip').textContent()).replace(/\s+/g, ' ');
  console.log('9 removing the last event rolls the status back:', afterRemove);

  // 10. 终态岗位不被冻结：还能继续加事件（重开就是再加一个更晚的非终态事件）。
  await page.click('.drawer button[aria-label="关闭"]');
  await page.waitForTimeout(400);
  await page.locator(`tbody tr:has-text("${company}")`).first().click();
  await page.waitForTimeout(600);
  const endedCanAdd = await page.locator('.drawer button:has-text("＋ 添加事件")').count();
  console.log('10a ended record still accepts events:', endedCanAdd > 0);
  const reopenChip = await addEvent(page, 'interview', { label: '加轮面试', at: '2026-09-25T10:00' });
  console.log('10b reopening an ended record by adding a later event:', reopenChip);

  // 11. 参考流程图的节点是最快的记录入口：点它就预填好事件类型。
  await page.click('.drawer button[aria-label="关闭"]');
  await page.waitForTimeout(400);
  await page.locator(`tbody tr:has-text("${skipCo}")`).first().click();
  await page.waitForTimeout(600);
  await page.click('.drawer .flow-node[data-kind="oa"]');
  await page.waitForSelector('.modal >> text=事件类型');
  const prefilled = await page.locator('.modal select').inputValue();
  console.log('11a clicking a flow-chart node pre-fills its kind:', prefilled === 'oa');
  const modalTxt = (await page.locator('.modal').innerText()) || '';
  console.log('11a form explains the stage effect:', modalTxt.includes('岗位状态会更新为'));
  // 11b 旧的固定流程界面已经彻底不在了。
  console.log('11b no 目标状态 picker anywhere:', !modalTxt.includes('目标状态'));
  console.log('11b no 子状态 picker anywhere:', !modalTxt.includes('具体进度'));
  await page.fill('.modal input[type=datetime-local]', '2026-09-08T09:00');
  await page.click('.modal button:has-text("保存")');
  await page.waitForTimeout(1400);

  // 12. OA / 作业轮次面板仍在（承载截止提醒与日历），与事件并存。
  await page.click('.drawer >> text=概览');
  await page.waitForTimeout(600);
  const oaOverview = (await page.locator('.drawer').textContent()) || '';
  console.log('12a OA panel is still the round entry point:',
    /OA\s*\/\s*作业\s*\(0\)/.test(oaOverview) && oaOverview.includes('新增一轮'));
  await page.click('.drawer button:has-text("新增一轮")');
  await page.waitForSelector('.modal >> text=新增一轮测评');
  await page.fill('.modal input[type=datetime-local] >> nth=2', '2026-09-20T23:59'); // 截止时间
  await page.click('.modal button:has-text("保存")');
  await page.waitForTimeout(1200);
  const oaAfter = (await page.locator('.drawer').textContent()) || '';
  console.log('12b OA round listed with due:',
    /OA\s*\/\s*作业\s*\(1\)/.test(oaAfter) && oaAfter.includes('截止'));

  // 13. 「投递」事件的时间就是投递时间（漏斗与等待天数读这个快照字段）。
  await page.click('.drawer button[aria-label="关闭"]');
  await page.waitForTimeout(300);
  await page.locator(`tbody tr:has-text("${company}")`).first().click();
  await page.waitForTimeout(600);
  await page.click('.drawer >> text=概览');
  await page.waitForTimeout(500);
  const submittedTxt = (await page.locator('.drawer').textContent()) || '';
  console.log('13 投递时间 shows the 投递 event day (2026/09/01):',
    /投递时间\s*2026\/09\/01/.test(submittedTxt));

  // 14. 表格列宽（公司与岗位）：默认按内容自适应，长名字不再被旧的固定 150px 截断；
  //     表头分隔线可拖拽调整、跨刷新保留，双击手柄 /「重置列宽」回到自动宽度。
  const longCo = '某某某科技（深圳）有限公司' + String(Date.now()).slice(-4);
  const longPos = '高级后端开发工程师（Go / 云原生方向）';
  // 上一节结束时抽屉还开着，它的 backdrop 会吃掉侧边栏的点击。
  await page.click('.drawer button[aria-label="关闭"]');
  await page.waitForTimeout(400);
  await page.locator('a:has-text("求职数据库")').first().click();
  await page.waitForSelector('text=求职数据库');
  await page.click('button:has-text("＋ 新增岗位")');
  await page.fill('#cf-company', longCo);
  await page.fill('#cf-pos', longPos);
  await page.click('button:has-text("创建")');
  await page.waitForSelector(`tbody tr:has-text("${longCo}")`, { timeout: 7000 });

  const cellFit = await page.evaluate((co) => {
    const row = [...document.querySelectorAll('tbody tr')].find((tr) => tr.textContent.includes(co));
    const tds = row.querySelectorAll('td');
    const company = tds[1].querySelector('span.ellipsis');
    const position = tds[2].querySelector('span.ellipsis');
    return {
      company: company.scrollWidth <= company.clientWidth + 1,
      position: position.scrollWidth <= position.clientWidth + 1,
      companyTextWidth: company.scrollWidth,
    };
  }, longCo);
  console.log('14a 公司 / 岗位默认就显示全（未被 .ellipsis 截断）:', cellFit.company && cellFit.position);
  console.log('14a 公司名宽于旧的固定 150px（旧布局必然截断）:', cellFit.companyTextWidth > 150);

  const headerWidths = () => page.$$eval('thead th', (ths) => ths.map((th) => Math.round(th.getBoundingClientRect().width)));
  const widthsBefore = await headerWidths();
  const companyHandle = await page.locator('thead th.col-head:has-text("公司") .col-resize').boundingBox();
  await page.mouse.move(companyHandle.x + companyHandle.width / 2, companyHandle.y + companyHandle.height / 2);
  await page.mouse.down();
  await page.mouse.move(companyHandle.x + companyHandle.width / 2 + 80, companyHandle.y + companyHandle.height / 2, { steps: 10 });
  await page.mouse.up();
  await page.waitForTimeout(300);
  const widthsDragged = await headerWidths();
  console.log('14b 拖公司列表头手柄可加宽 ~80px:', widthsDragged[1] - widthsBefore[1] >= 76);
  console.log('14b 拖动只影响被拖的那一列:', widthsDragged[2] === widthsBefore[2]);

  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForSelector(`tbody tr:has-text("${longCo}")`, { timeout: 7000 });
  const widthsReloaded = await headerWidths();
  console.log('14c 手动列宽跨刷新保留:', widthsReloaded[1] === widthsDragged[1]);

  await page.locator('thead th.col-head:has-text("公司") .col-resize').dblclick();
  await page.waitForTimeout(300);
  const widthsReset = await headerWidths();
  console.log('14d 双击手柄恢复按内容自适应的宽度:', Math.abs(widthsReset[1] - widthsBefore[1]) <= 2);
  const stored = await page.evaluate(() => localStorage.getItem('offerlog:db-col-widths'));
  console.log('14d 重置后本地不再留手动宽度:', stored === '{}');

  console.log('E2E JS errors:', errors.length ? errors : 'none');
  await browser.close();
})().catch((e) => { console.error('E2E FAIL', e.message); process.exit(1); });
