import { test } from '@playwright/test';
const APP_URL = process.env.APP_URL || 'http://127.0.0.1:8080';

test('debug login', async ({ page }) => {
  await page.goto(APP_URL);
  await page.waitForTimeout(3000);
  await page.screenshot({ path: '/tmp/step1-login-page.png', fullPage: true });

  const inputs = page.locator('input');
  const count = await inputs.count();
  console.log(`Found ${count} inputs`);

  if (count >= 2) {
    await inputs.nth(0).fill('admin');
    await inputs.nth(1).fill('admin');
    await page.screenshot({ path: '/tmp/step2-filled.png', fullPage: true });

    const submitBtn = page.locator('button[type="submit"], button:has-text("login"), button:has-text("Login"), button:has-text("登录")');
    const btnCount = await submitBtn.count();
    console.log(`Found ${btnCount} submit buttons`);

    if (btnCount > 0) {
      await submitBtn.first().click();
    } else {
      await inputs.nth(1).press('Enter');
    }

    await page.waitForTimeout(5000);
    await page.screenshot({ path: '/tmp/step3-after-login.png', fullPage: true });

    const nav = page.locator('nav');
    const navCount = await nav.count();
    console.log(`Found ${navCount} nav elements`);
    
    const bodyText = await page.locator('body').innerText().catch(() => 'failed to get text');
    console.log(`Body text (first 500): ${bodyText.substring(0, 500)}`);
  }
});
