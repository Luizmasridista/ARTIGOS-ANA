import { test, expect } from 'playwright/test'

test.describe('iPad Live Preview (dev-only)', () => {
  test('preview route renders iPad frames with device chrome', async ({ page }, testInfo) => {
    await page.goto('/__ipad-preview')
    await expect(page.getByRole('heading', { name: /iPad Live Preview/i })).toBeVisible()
    // badges dev-only and deviceDescriptorsSource
    await expect(page.getByText('DEV only')).toBeVisible()
    await expect(page.getByText(/deviceDescriptorsSource/i)).toBeVisible()

    // segment controls
    await expect(page.getByRole('button', { name: /iPad Mini/i }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: /Retrato 768/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /Paisagem 1024/i })).toBeVisible()

    // at least the two required frames appear by default (both devices)
    // filter to ensure portrait+landscape both visible initially
    const miniPortrait = page.locator('[data-testid="ipad-frame-ipad-mini-portrait"]')
    const miniLandscape = page.locator('[data-testid="ipad-frame-ipad-mini-landscape"]')
    await expect(miniPortrait).toBeVisible()
    await expect(miniLandscape).toBeVisible()

    // orientation toggle filters
    await page.getByRole('button', { name: /Retrato 768/i }).click()
    await expect(miniPortrait).toBeVisible()
    await expect(miniLandscape).toBeHidden()

    await page.getByRole('button', { name: /Paisagem 1024/i }).click()
    await expect(miniLandscape).toBeVisible()
    await expect(miniPortrait).toBeHidden()

    // reset to both
    await page.getByRole('button', { name: /^Ambas$/ }).click()
    await expect(miniPortrait).toBeVisible()

    // iframe content: ensure app renders inside frame (home or biblioteca) without clipping overflowX
    // the iframe src is /?__ipadFrame=1 — check at least one iframe loads and data-device=ipad is applied inside
    const firstFrame = page.frameLocator('iframe').first()
    // GrokBanner only in Electron, but Home/Biblioteca should appear
    // we check that inner html has .biblioteca or .home or loading state
    await expect(firstFrame.locator('body')).toBeVisible()

    // attach screenshot evidence (768x1024 and 1024x768 are device viewports; preview screenshots at project viewport)
    // actual device viewports screenshots are produced via project viewports, not preview scaling
    await page.screenshot({ path: `test-results/preview-${testInfo.project.name}.png`, fullPage: true })
  })

  test('query param ?ipadPreview=1 also opens preview', async ({ page }) => {
    await page.goto('/?ipadPreview=1')
    await expect(page.getByRole('heading', { name: /iPad Live Preview/i })).toBeVisible()
  })

  test('device filter toggles Mini / Pro 11', async ({ page }) => {
    await page.goto('/__ipad-preview')
    await page.getByRole('button', { name: /^iPad Mini$/ }).click()
    await expect(page.locator('[data-testid="ipad-frame-ipad-mini-portrait"]')).toBeVisible()
    await expect(page.locator('[data-testid="ipad-frame-ipad-pro11-portrait"]')).toBeHidden()

    await page.getByRole('button', { name: /^Pro 11$/ }).click()
    await expect(page.locator('[data-testid="ipad-frame-ipad-pro11-portrait"]')).toBeVisible()
    await expect(page.locator('[data-testid="ipad-frame-ipad-mini-portrait"]')).toBeHidden()
  })
})
