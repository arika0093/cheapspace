import { expect, test } from "@playwright/test";

test("phase1 smoke workflow", async ({ page }) => {
  await page.goto("/workspaces");
  await expect(page.getByRole("heading", { name: "Workspaces" })).toBeVisible();

  await page.getByRole("link", { name: "Create workspace" }).first().click();
  await expect(page.getByRole("heading", { name: "Create a workspace" })).toBeVisible();

  await page.locator("input[name='repo_url']").fill("https://github.com/example/repo.git");
  await page.locator("input[name='name']").fill("local-smoke");
  await page.locator("textarea[name='ssh_keys']").fill("");
  await page.locator("input[name='ttl_minutes']").fill("15");
  await page.getByRole("button", { name: "Queue workspace" }).click();

  await expect(page).toHaveURL(/\/workspaces\/.+/);
  await expect(page.locator("#workspace-state")).toHaveText(/pending|provisioning|running/);
  await expect(page.locator("#workspace-state")).toHaveText("running", { timeout: 15000 });
  await expect(page.locator("#workspace-ssh-command")).toContainText("ssh -p");

  await page.getByRole("link", { name: "View jobs" }).click();
  await expect(page.getByRole("heading", { name: "Jobs" })).toBeVisible();
  await expect(page.getByText("provision_workspace")).toBeVisible();

  await page.getByRole("link", { name: "Details" }).first().click();
  await expect(page.locator("#job-status")).toHaveText(/done|running/);
  await expect(page.locator("#job-log-output")).toContainText(/Workspace|Job started/);
});

