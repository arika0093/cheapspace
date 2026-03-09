import { expect, test } from "@playwright/test";

test("phase1 smoke workflow", async ({ page }) => {
  await page.goto("/workspaces");
  await expect(page.getByRole("heading", { name: "Workspaces" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await page.getByRole("button", { name: "Dark mode" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  await page.getByRole("link", { name: "Create workspace" }).first().click();
  await expect(page.getByRole("heading", { name: "Create a workspace" })).toBeVisible();
  const sourcePanel = page.locator("[data-source-panel='source_ref']");
  await expect(sourcePanel).toBeHidden();
  await page.locator("select[name='source_type']").selectOption("image_ref");
  await expect(sourcePanel).toBeVisible();
  await page.locator("select[name='source_type']").selectOption("builtin_image");
  await expect(sourcePanel).toBeHidden();
  await expect(page.locator("input[name='no_proxy']")).toHaveValue(/localhost/);

  await page.locator("input[name='repo_url']").fill("https://github.com/example/repo.git");
  await page.locator("input[name='repo_branch']").fill("main");
  await page.locator("input[name='name']").fill("local-smoke");
  await page.locator("input[name='ssh_port']").fill("2301");
  await page.locator("input[name='cpu_cores']").fill("2");
  await page.locator("input[name='http_proxy']").fill("http://proxy.internal:8080");
  await expect(page.locator("input[name='https_proxy']")).toHaveValue("http://proxy.internal:8080");
  await page.locator("input[name='proxy_pac_url']").fill("https://proxy.internal/proxy.pac");
  await page.getByRole("button", { name: "Queue workspace" }).click();

  await expect(page).toHaveURL(/\/workspaces\/.+/);
  await expect(page.locator("#workspace-state")).toHaveText(/pending|provisioning|running/);
  await expect(page.locator("#workspace-state")).toHaveText("running", { timeout: 15000 });
  await expect(page.locator("#workspace-ssh-command")).toContainText("ssh -p 2301");
  await expect(page.getByText("Repository branch").locator("..")).toContainText("main");
  await expect(page.getByText("CPU").locator("..")).toContainText("2 cores");
  await page.evaluate(() => {
    globalThis.__copiedText = "";
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: {
        writeText: async (value) => {
          globalThis.__copiedText = value;
        },
      },
    });
  });
  await page.getByRole("button", { name: "Copy SSH command" }).click();
  await expect(page.getByRole("button", { name: "Copied" })).toBeVisible();
  await expect.poll(() => page.evaluate(() => globalThis.__copiedText)).toContain("ssh -p 2301");

  await page.getByRole("link", { name: "View jobs" }).click();
  await expect(page.getByRole("heading", { name: "Jobs" })).toBeVisible();
  await expect(page.getByText("provision_workspace")).toBeVisible();

  await page.getByRole("link", { name: "Details" }).first().click();
  await expect(page.locator("#job-status")).toHaveText(/done|running/);
  await expect(page.locator("[data-log-toggle='detailed']")).toBeChecked();
  await page.locator("[data-log-toggle='time']").check();
  await expect(page.locator("main[data-job-detail]")).toHaveAttribute("data-log-time", "true");
  await expect(page.locator("#job-log-output .log-message").first()).toContainText(/Workspace|Job started|Reserved SSH host port/);
});

