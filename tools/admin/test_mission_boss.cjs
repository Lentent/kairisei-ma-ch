// Run with NODE_PATH pointing to an installed Playwright package directory:
// node tools/admin/test_mission_boss.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { chromium } = require('playwright');

(async () => {
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
    const root = path.resolve(__dirname, '../..');
    const web = path.join(root, 'server/internal/admin/web');
    await page.setContent(fs.readFileSync(path.join(web, 'admin_ui.html'), 'utf8').replace(/<script[\s\S]*?<\/script>/g, ''));
    await page.addStyleTag({ content: fs.readFileSync(path.join(web, 'admin_ui.css'), 'utf8') });
    await page.evaluate(() => {
      window.$ = s => document.querySelector(s);
      window.esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
      window.contentKey = r => `${r.type ?? r.reward_type}:${r.reward_typeid ?? r.reward_type_id ?? 0}`;
      window.contentPicker = { labels: new Map() };
      window.policyBusy = () => false;
      window.updateDraftIndicators = () => {};
      window.state = { loaded: new Set() };
      window.toast = message => { window.lastToast = message; };
      window.contentAction = async (_, action) => action();
      window.confirm = () => true;
      window.api = async (_, options) => {
        window.savedRequest = JSON.parse(options.body);
        return { ...window.missionFixture, config: window.savedRequest.config, revision: 2 };
      };
      window.missionFixture = {
        revision: 1,
        boss_targets: [
          { boss_id: 123, name: '冰龙', difficulty: '超级' },
          { boss_id: 124, name: '冰龙', difficulty: '超弩级' },
          { boss_id: 125, name: '火龙', difficulty: '超弩级' },
        ],
        config: { missions: [{ id: 1000000, title: '击败冰龙', kind: 'boss_clear', boss_id: 124, target: 2, daily: false, enabled: true, rewards: [{ type: 10, num: 7, card_skill_lv: [] }] }] },
      };
      document.querySelectorAll('.view').forEach(e => e.classList.toggle('active', e.id === 'missions'));
    });
    await page.addScriptTag({ content: fs.readFileSync(path.join(web, 'admin_missions.js'), 'utf8') });
    await page.evaluate(() => acceptMissionPolicy(window.missionFixture));
    assert.equal(await page.locator('#mission-boss').inputValue(), '124');
    assert.equal(await page.locator('#mission-save').isDisabled(), true);
    await page.locator('#mission-boss-search').fill('超弩');
    assert.equal(await page.locator('#mission-boss option').count(), 3);
    await page.locator('#mission-boss').selectOption('125');
    assert.equal(await page.locator('#mission-identity-note').isVisible(), true);
    await page.locator('#mission-target').fill('3');
    await page.locator('#mission-save').click();
    await page.waitForFunction(() => window.savedRequest?.config.missions[0].boss_id === 125);
    const saved = await page.evaluate(() => window.savedRequest.config.missions[0]);
    assert.equal(saved.kind, 'boss_clear');
    assert.equal(saved.target, 3);
    assert.equal(saved.daily, false);
    assert.equal(await page.locator('#mission-boss').inputValue(), '125');
    assert.equal(await page.locator('#mission-save').isDisabled(), true);
    await page.locator('#mission-category').selectOption('daily');
    assert.equal(await page.locator('#mission-identity-note').isVisible(), true);
    await page.locator('#mission-copy').click();
    assert.equal(await page.locator('#mission-boss').inputValue(), '125');
    await page.locator('#mission-kind').selectOption('explore');
    assert.equal(await page.locator('#mission-boss-fields').isVisible(), false);
    await page.locator('#mission-kind').selectOption('boss_clear');
    assert.equal(await page.locator('#mission-boss').inputValue(), '0');
    await page.locator('#mission-save').click();
    assert.match(await page.evaluate(() => window.lastToast), /请选择目录中的 BOSS/);
    await page.locator('#mission-boss').selectOption('124');
    await page.locator('#mission-boss-search').fill('火龙');
    assert.equal(await page.locator('#mission-boss').inputValue(), '124', 'filtering must preserve the chosen boss');
    await page.locator('#mission-boss-search').fill('');
    await page.locator('#mission-fields').scrollIntoViewIfNeeded();
    fs.mkdirSync(path.join(root, '_local'), { recursive: true });
    await page.screenshot({ path: path.join(root, '_local/mission-boss-desktop.png') });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.waitForFunction(() => document.querySelector('.sidebar').getBoundingClientRect().right <= 0);
    await page.locator('#mission-boss-fields').scrollIntoViewIfNeeded();
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true, 'mobile layout must fit viewport');
    await page.screenshot({ path: path.join(root, '_local/mission-boss-mobile.png') });
    console.log('PASS: boss difficulty selection, filtering, save, identity changes, copy, required target and mobile layout');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
