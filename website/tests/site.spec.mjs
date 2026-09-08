import { test, expect } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

test("homepage works under the project path, demo and installation tabs are accessible", async ({
  page,
}) => {
  test.setTimeout(120000);
  const errors = [];
  const externalRequests = [];
  page.on("request", (request) => {
    if (!request.url().startsWith("http://127.0.0.1:4173/"))
      externalRequests.push(request.url());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("./");
  await expect(page).toHaveTitle("Correlux — Less cluster. More clarity.");
  await expect(page.locator("h1")).toContainText("Get the plot.");
  await page.getByRole("button", { name: "Pause motion" }).click();
  await expect(
    page.getByRole("button", { name: "Resume motion" }),
  ).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Resume motion" }).click();
  await page.getByRole("button", { name: "Start interactive terminal" }).click();
  const terminal = page.locator(".demo-screen");
  await expect(terminal).toContainText("PROD prod-eu", { timeout: 60000 });
  await expect(terminal).toContainText("Ctrl+P");
  await page.locator("#demo-screen").selectOption("why");
  await expect(terminal).toContainText("memory limit");
  await page.locator("#demo-screen").selectOption("fleet");
  await expect(terminal).toContainText("staging");
  await expect(terminal).toContainText("dev-local");
  await page.getByRole("tab", { name: "Go", exact: true }).click();
  await expect(page.locator("#install-go")).toBeVisible();
  await page.getByRole("tab", { name: "Go", exact: true }).press("ArrowRight");
  await expect(
    page.getByRole("tab", { name: "Binary", exact: true }),
  ).toBeFocused();
  await expect(page.locator("#install-binary")).toBeVisible();
  await page
    .getByText("Does anything run inside my cluster?", { exact: false })
    .click();
  await expect(page.locator(".faq-list details").nth(1)).toHaveAttribute(
    "open",
    "",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  expect(errors).toEqual([]);
  expect(externalRequests).toEqual([]);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const accessibility = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(
    accessibility.violations.map((v) => ({
      id: v.id,
      nodes: v.nodes.map((n) => ({
        target: n.target,
        reason: n.failureSummary,
      })),
    })),
  ).toEqual([]);
});

test("documentation search, links and mobile navigation", async ({
  page,
  isMobile,
}) => {
  await page.goto("docs/");
  await page.getByRole("button", { name: "Search docs" }).click();
  await page.getByRole("searchbox").fill("production confirmation");
  await expect(page.locator(".search-results a").first()).toBeVisible();
  await page.locator(".search-results a").first().click();
  await expect(page).toHaveURL(/docs\/.+\.html#/);
  await expect(page.locator("h1")).toBeVisible();
  await page.getByRole("button", { name: "Search docs" }).click();
  await page.getByRole("searchbox").fill("zzzz-no-such-concept");
  await expect(page.locator(".search-count")).toContainText("No results");
  await page.getByRole("button", { name: "Close search" }).click();
  await expect(page.getByRole("button", { name: "Search docs" })).toBeFocused();
  await page.goto("docs/getting-started.html");
  if (isMobile) await page.locator(".docs-mobile-nav summary").first().click();
  await expect(
    page.locator(".docs-sidebar a:visible").filter({ hasText: /^User guide$/ }),
  ).toBeVisible();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.emulateMedia({ reducedMotion: "reduce" });
  const accessibility = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  expect(
    accessibility.violations.map((v) => ({
      id: v.id,
      nodes: v.nodes.map((n) => ({
        target: n.target,
        reason: n.failureSummary,
      })),
    })),
  ).toEqual([]);
});

test("reduced motion and no-JavaScript content remain usable", async ({
  browser,
}) => {
  const context = await browser.newContext({
    javaScriptEnabled: false,
    reducedMotion: "reduce",
    viewport: { width: 375, height: 812 },
  });
  const page = await context.newPage();
  await page.goto("http://127.0.0.1:4173/correlux/");
  await expect(page.locator("h1")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Full installation guide" }),
  ).toBeVisible();
  await page.getByRole("link", { name: "Full installation guide" }).click();
  await expect(page.locator("h1")).toContainText("From install to insight");
  await context.close();
});

test("copy command provides exact executable text", async ({
  page,
  context,
}) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto("./");
  await page
    .getByRole("button", { name: "Copy Homebrew install command", exact: true })
    .click();
  await expect(page.locator(".toast")).toContainText("Copied.");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
    "brew install aronk11/tap/correlux",
  );
});


test("real terminal demo supports navigation, filters, overlays and safe sample changes", async ({ page }) => {
  test.setTimeout(120000);
  await page.goto("./");
  await page.getByRole("button", { name: "Start interactive terminal" }).click();
  const terminal = page.locator(".demo-screen");
  const views = page.locator("#demo-screen");
  await expect(terminal).toContainText("PROD prod-eu", { timeout: 60000 });
  // The on-screen keys are for devices without any: a pointer device with a
  // keyboard gets the terminal itself, and the row would only repeat what the
  // status bar already prints.
  const touch = await page.evaluate(
    () => matchMedia("(max-width: 760px), (pointer: coarse)").matches,
  );
  const keys = page.locator(".demo-controls");
  if (touch) await expect(keys).toBeVisible();
  else await expect(keys).toBeHidden();
  const pressEnter = async () => {
    const button = page.getByRole("button", { name: "Enter ↵", exact: true });
    if (await button.isVisible()) await button.click();
    else await terminal.press("Enter");
  };
  for (const name of ["application", "pods", "object", "yaml", "logs", "events", "usage", "session", "fleet-resources", "fleet-clusters", "fleet-namespaces", "commands", "resources", "clusters", "namespaces", "help"]) {
    await views.selectOption(name);
    await expect(terminal).toContainText("prod-eu");
    await expect(terminal).not.toContainText("Looking for applications");
  }
  await views.selectOption("scale");
  await expect(terminal).toContainText("Scale Deployment/payments");
  await page.locator("#demo-text").fill("5");
  await page.getByRole("button", { name: "Send ↵", exact: true }).click();
  await expect(terminal).toContainText("3 replicas → 5 replicas");
  await pressEnter();
  await expect(terminal).toContainText("Type prod-eu");
  await page.locator("#demo-text").fill("prod-eu");
  await page.getByRole("button", { name: "Send ↵", exact: true }).click();
  await expect(terminal).toContainText("Simulated: Scale");
  await views.selectOption("apps");
  await expect(terminal).toContainText("0/5");
  await page.getByRole("button", { name: "Reset demo", exact: true }).click();
  await expect(terminal).toContainText("0/3");
  await views.selectOption("commands");
  await page.locator("#demo-text").fill("Switch to cluster staging");
  await page.getByRole("button", { name: "Send ↵", exact: true }).click();
  await expect(terminal).not.toContainText("PROD prod-eu");
  await expect(terminal).toContainText("staging");
  await expect(terminal).toContainText("healthy");
});
