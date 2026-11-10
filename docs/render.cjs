// Renders the C Tunnel banners and terminal screenshots to high-res PNGs.
// Usage (from repo root): NODE_PATH=/opt/node-tools/node_modules node docs/render.cjs
const { chromium } = require('playwright');
const path = require('path');

const dir = __dirname;
const shots = [
  { file: 'banner-en.html', out: 'img/banner-en.png', w: 1280, h: 720 },
  { file: 'banner-fa.html', out: 'img/banner-fa.png', w: 1280, h: 720 },
  { file: 'menu.html',      out: 'img/menu.png',      w: 1120, h: 760 },
  { file: 'linktest.html',  out: 'img/linktest.png',  w: 1120, h: 720 },
];

(async () => {
  const browser = await chromium.launch();
  for (const s of shots) {
    const page = await browser.newPage({ viewport: { width: s.w, height: s.h }, deviceScaleFactor: 2 });
    await page.goto('file://' + path.join(dir, s.file));
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(250);
    await page.screenshot({ path: path.join(dir, s.out) });
    await page.close();
    console.log('wrote', s.out);
  }
  await browser.close();
})();
