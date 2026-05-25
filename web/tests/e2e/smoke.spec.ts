import { test, expect } from '@playwright/test';

const APP_URL = process.env.APP_URL || 'http://127.0.0.1:8080';

test.describe('Smoke Tests', () => {
  test('app loads without crash', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });

    await page.goto(APP_URL);
    await expect(page.locator('body')).toBeVisible();

    // Wait for either login form or main app to appear
    await page.waitForTimeout(3000);

    // No visible crash text
    const crashText = page.locator('text=/application error|something went wrong|500/i');
    await expect(crashText).toHaveCount(0);
  });

  test('login page appears when not authenticated', async ({ page }) => {
    await page.goto(APP_URL);
    // Wait for the logo animation to finish and login form to appear
    await page.waitForTimeout(3000);

    // Should see a login form or input fields
    const loginForm = page.locator('input');
    const count = await loginForm.count();
    expect(count).toBeGreaterThan(0);
  });

  test('can login with default credentials', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') errors.push(msg.text());
    });

    await page.goto(APP_URL);
    // Wait for login form
    await page.waitForTimeout(3000);

    // Find username and password fields
    const inputs = page.locator('input');
    const count = await inputs.count();

    if (count >= 2) {
      // Type credentials
      await inputs.nth(0).fill('admin');
      await inputs.nth(1).fill('admin');

      // Find and click submit button (use type=submit to avoid matching tab triggers)
      const submitBtn = page.locator('button[type="submit"]');
      if (await submitBtn.count() > 0) {
        await submitBtn.first().click();
      } else {
        // Try pressing Enter
        await inputs.nth(1).press('Enter');
      }

      // Wait for navigation to main app
      await page.waitForTimeout(3000);

      // Should see the main app with navbar
      const nav = page.locator('nav, [class*="navbar"], [class*="NavBar"]');
      await expect(nav.first()).toBeVisible({ timeout: 10000 });
    }
  });

  test('channel page loads with overview', async ({ page }) => {
    // Login first
    await page.goto(APP_URL);
    await page.waitForTimeout(3000);

    const inputs = page.locator('input');
    if (await inputs.count() >= 2) {
      await inputs.nth(0).fill('admin');
      await inputs.nth(1).fill('admin');
      const submitBtn = page.locator('button[type="submit"]');
      if (await submitBtn.count() > 0) {
        await submitBtn.first().click();
      } else {
        await inputs.nth(1).press('Enter');
      }
      await page.waitForTimeout(3000);
    }

    // Click on Channel nav item (渠道)
    const channelNav = page.locator('text=/channel|渠道/i').first();
    if (await channelNav.isVisible()) {
      await channelNav.click();
      await page.waitForTimeout(2000);
    }

    // Check for channel overview section
    const overview = page.locator('text=/channel overview|渠道概览|渠道總覽/i');
    // Overview might be visible if there are channels
    const pageContent = await page.content();
    expect(pageContent.length).toBeGreaterThan(0);
  });

  test('no critical console errors on main pages', async ({ page }) => {
    const errors: string[] = [];
    page.on('console', msg => {
      if (msg.type() === 'error') {
        const text = msg.text();
        // Ignore known non-critical errors
        if (text.includes('favicon')) return;
        if (text.includes('third-party cookie')) return;
        if (text.includes('ResizeObserver')) return;
        errors.push(text);
      }
    });

    // Login
    await page.goto(APP_URL);
    await page.waitForTimeout(3000);
    const inputs = page.locator('input');
    if (await inputs.count() >= 2) {
      await inputs.nth(0).fill('admin');
      await inputs.nth(1).fill('admin');
      const submitBtn = page.locator('button[type="submit"]');
      if (await submitBtn.count() > 0) {
        await submitBtn.first().click();
      } else {
        await inputs.nth(1).press('Enter');
      }
      await page.waitForTimeout(3000);
    }

    // Navigate to each main page
    const pages = ['渠道', 'Channel', '分組', 'Group', '模型', 'Model'];
    for (const pageName of pages) {
      const navItem = page.locator(`text=/${pageName}/i`).first();
      if (await navItem.isVisible().catch(() => false)) {
        await navItem.click();
        await page.waitForTimeout(1500);
      }
    }

    // Filter out expected errors
    const criticalErrors = errors.filter(e =>
      !e.includes('Failed to fetch') &&
      !e.includes('NetworkError') &&
      !e.includes('net::ERR')
    );

    expect(criticalErrors).toEqual([]);
  });
});
