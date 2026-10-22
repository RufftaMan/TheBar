// Takes the README screenshots of a running TheBar (see run.sh).
// BASE: TheBar's address. OUT: where the PNGs go.
import { chromium } from 'playwright';

const BASE = process.env.BASE || 'http://127.0.0.1:18800';
const OUT = process.env.OUT || './out';

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1360, height: 960 }, deviceScaleFactor: 1 });
const page = await ctx.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));

// The layout the screenshots show: two open groups, one collapsed, then single cards.
const layout = {
  v: 1,
  top: ['g:p:media', 'g:p:monitoring', 'g:p:tools'],
  groups: { 'p:tools': { collapsed: true } },
  cards: {},
  dissolved: [],
  seen: [],
  settings: { preferDomain: true, linkHost: '', grafana: '' },
};

await page.goto(BASE);
await page.waitForSelector('article.card');
await page.evaluate(async (layout) => {
  await fetch('/api/layout', {
    method: 'PUT',
    headers: { 'X-TheBar': '1', 'Content-Type': 'application/json' },
    body: JSON.stringify({ layout, rev: -1, origin: 'screenshots' }),
  });
  await fetch('/api/updates/check', { method: 'POST', headers: { 'X-TheBar': '1', 'Content-Type': 'application/json' }, body: '{}' });
}, layout);
await page.reload();
await page.waitForSelector('article.card');
await page.waitForTimeout(6000); // update badges, logos, drift check

const shot = (name, opts = {}) => page.screenshot({ path: `${OUT}/${name}.png`, ...opts });

// 1. the dashboard
await page.mouse.move(5, 5);
await shot('dashboard', { clip: { x: 0, y: 0, width: 1360, height: 960 } });

// the sticky header would paint over the top of tall element shots
await page.addStyleTag({ content: 'header.top{position:static !important}' });

// 2. an open card with live charts
await page.locator('h3:text-is("Movies")').click();
await page.waitForTimeout(1500);
await page.locator('article.card.expanded').screenshot({ path: `${OUT}/card.png` });
await page.locator('article.card.expanded button[title="Close"]').click();
await page.waitForTimeout(400);

// 3. the compose editor, on a stack whose file changed since it was started
await page.locator('h3:text-is("Dashboards")').click();
await page.waitForTimeout(500);
await page.click('button[role=tab]:has-text("Config")');
await page.waitForSelector('.cm-editor');
await page.waitForTimeout(2500);
await page.locator('article.card.expanded').screenshot({ path: `${OUT}/editor.png` });
await page.keyboard.press('Escape');

// 4. the phone
const phone = await browser.newContext({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true, isMobile: true });
const p = await phone.newPage();
await p.goto(BASE);
await p.waitForSelector('article.card');
await p.waitForTimeout(3000);
await p.screenshot({ path: `${OUT}/phone.png` });

// 5. the picture shown when the repository link is shared (1280x640)
const dash = (await import('node:fs')).readFileSync(`${OUT}/dashboard.png`).toString('base64');
const social = await browser.newContext({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 1 });
const s = await social.newPage();
await s.setContent(`<!doctype html><meta charset="utf-8"><style>
  *{box-sizing:border-box;margin:0}
  body{width:1280px;height:640px;overflow:hidden;background:radial-gradient(900px 500px at 15% 0%,#2a1f0a,transparent 60%),radial-gradient(800px 500px at 100% 100%,#0e2238,transparent 60%),#0c0e12;
       font-family:ui-sans-serif,system-ui,-apple-system,'Segoe UI',Roboto,Arial,sans-serif;color:#eceef2;position:relative}
  .left{position:absolute;left:84px;top:150px;width:520px}
  .brand{display:flex;align-items:center;gap:22px;font-size:84px;letter-spacing:-0.03em;font-weight:500}
  .brand b{color:#f5a524;font-weight:750}
  .tag{margin-top:26px;font-size:34px;line-height:1.25;color:#b4bac5}
  .pills{margin-top:34px;display:flex;gap:12px;flex-wrap:wrap}
  .pills span{border:1px solid #343a46;background:#14171d;border-radius:999px;padding:8px 16px;font-size:20px;color:#b4bac5}
  .shot{position:absolute;left:640px;top:70px;width:1000px;border-radius:16px;border:1px solid #343a46;box-shadow:0 30px 80px rgba(0,0,0,.6);transform:rotate(-2deg)}
</style>
<div class="left">
  <div class="brand"><svg width="96" height="96" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#16130c"/><path d="M42 28h4.5a5.5 5.5 0 0 1 5.5 5.5v7a5.5 5.5 0 0 1-5.5 5.5H42" fill="none" stroke="#f5a524" stroke-width="4.5"/><rect x="13" y="22" width="30" height="32" rx="4.5" fill="#f0a020"/><rect x="13" y="48" width="30" height="6" rx="3" fill="#c4740c"/><g fill="#fff3d6"><rect x="11" y="17" width="34" height="8" rx="4"/><circle cx="17" cy="17" r="5.5"/><circle cx="25.5" cy="13.5" r="6.5"/><circle cx="34.5" cy="14.5" r="6"/><circle cx="41" cy="18" r="4.5"/></g></svg><span>The<b>Bar</b></span></div>
  <div class="tag">Where all your containers meet.</div>
  <div class="pills"><span>Open any app with one click</span><span>Live stats</span><span>Compose editor</span></div>
</div>
<img class="shot" src="data:image/png;base64,${dash}">`);
await s.waitForTimeout(500);
await s.screenshot({ path: `${OUT}/social-preview.png` });

await browser.close();
if (errors.length) {
  console.error('page errors:\n' + errors.join('\n'));
  process.exit(1);
}
console.log('screenshots written to', OUT);
