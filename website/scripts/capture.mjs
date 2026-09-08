import { chromium } from "@playwright/test";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const browser = await chromium.launch({
  executablePath: process.env.CHROME_PATH || undefined,
});
const page = await browser.newPage({
  viewport: { width: 1440, height: 1100 },
  deviceScaleFactor: 1,
  reducedMotion: "reduce",
});
await page.goto("http://127.0.0.1:4173/correlux/", {
  waitUntil: "networkidle",
});
await page.screenshot({ path: "/tmp/correlux-desktop.png", fullPage: true });
await page.setViewportSize({ width: 390, height: 844 });
await page.screenshot({ path: "/tmp/correlux-mobile.png", fullPage: true });
await page.goto("http://127.0.0.1:4173/correlux/docs/getting-started.html");
await page.setViewportSize({ width: 1440, height: 1000 });
await page.screenshot({ path: "/tmp/correlux-docs.png" });
await page.setViewportSize({ width: 1200, height: 630 });
const social = await readFile(path.join(site, "social.html"), "utf8");
await page.goto("http://127.0.0.1:4173/correlux/");
await page.setContent(social);
await page.evaluate(() => document.fonts.ready);
await page.screenshot({ path: path.join(site, "assets/social.png") });
await browser.close();
console.log(
  "Saved desktop, mobile and documentation screenshots to /tmp/correlux-*.png; social card to assets/social.png",
);
