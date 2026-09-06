const { chromium } = require('playwright');
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', (e) => errors.push('pageerror: ' + e.message));
  page.on('console', (m) => { if (m.type() === 'error' && !m.text().includes('401') && !m.text().includes('404')) errors.push('console: ' + m.text()); });
  await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
  await page.fill('input[type=email]', 'me@example.com');
  await page.fill('input[type=password]', 'testpass12345');
  await page.click('button:has-text("登录")');
  await page.waitForSelector('text=今日待办', { timeout: 8000 });
  // database
  await page.click('a:has-text("求职数据库")');
  await page.waitForSelector('text=求职数据库', { timeout: 8000 });
  // fresh deployment: create a row through the UI so the smoke has data to act on
  if ((await page.locator('table.tbl tbody tr').count()) === 0) {
    await page.click('button:has-text("＋ 新增岗位")');
    await page.fill('#cf-company', 'Smoke-Corp-' + Date.now());
    await page.fill('#cf-pos', '冒烟岗位');
    await page.click('button:has-text("创建")');
  }
  await page.waitForSelector('table.tbl tbody tr', { timeout: 8000 });
  // pick a non-ended application (update button enabled)
  const rowCount = await page.locator('table.tbl tbody tr').count();
  let target = 0;
  for (let i = 0; i < rowCount; i++) {
    const txt = (await page.locator('table.tbl tbody tr').nth(i).textContent()) || '';
    if (!['已接受','被拒绝','已撤回','岗位关闭'].some(s=>txt.includes(s))) { target = i; break; }
  }
  await page.locator('table.tbl tbody tr').nth(target).click();
  await page.waitForSelector('.drawer', { timeout: 8000 });
  await page.waitForSelector('.drawer button:not([disabled]) >> text=更新进度', { timeout: 6000 });
  console.log('drawer + 更新进度 visible OK');
  // open transition modal
  await page.click('.drawer button:not([disabled]) >> text=更新进度');
  await page.waitForSelector('text=目标状态', { timeout: 5000 });
  console.log('transition modal OK');
  await page.keyboard.press('Escape');
  await page.waitForTimeout(400);
  // close any overlay that may still cover the sidebar
  const backdrop = page.locator('.drawer-backdrop');
  if (await backdrop.count()) { await page.mouse.click(30, 400); await page.waitForTimeout(500); }
  // analytics
  await page.locator('.sidebar a:has-text("统计分析")').click();
  await page.waitForSelector('text=桑基图', { timeout: 9000 });
  await page.waitForTimeout(1500);
  const canvases = await page.locator('canvas').count();
  console.log('analytics canvases:', canvases);
  await page.screenshot({ path: '/tmp/shot-analytics.png' });
  // files
  await page.click('a:has-text("文件库")');
  await page.waitForSelector('text=文件库', { timeout: 6000 });
  const hasUpload = await page.isVisible('text=上传文件');
  console.log('files page upload visible:', hasUpload);
  // mobile viewport
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(600);
  const bottomNav = await page.isVisible('.bottom-nav');
  console.log('bottom nav visible on mobile:', bottomNav);
  await page.screenshot({ path: '/tmp/shot-mobile.png' });
  console.log('JS errors:', errors.length ? errors : 'none');
  await browser.close();
})();
