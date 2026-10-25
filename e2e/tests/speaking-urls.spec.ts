import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// Speaking canonical URLs (1.2): channels and Talks are addressed by slug,
// old slugs and legacy id links move to the current address. Conversations are
// covered in dm.spec.ts.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `Größe Café ${suffix}`
const RENAMED = `Neuer Name ${suffix}`
const TALK = `Treff ${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('channels and Talks have speaking addresses that survive a rename', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await ctx.newPage()
  const created: string[] = []
  try {
    await signIn(page)
    const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    created.push(channelId)
    const slug = `groesse-cafe-${suffix}`
    expect(responseString(channel, 'slug')).toBe(slug)

    // A legacy link by id moves to the slug.
    await page.goto(`/c/${channelId}`)
    await expect(page).toHaveURL(new RegExp(`/c/${slug}$`))
    await expect(page.getByTestId('chat-channel-name')).toContainText(CHANNEL)

    // A rename moves the open address; the old one still leads here.
    const renamed = await apiFetch(page, 'PATCH', `/api/admin/channels/${channelId}`, { name: RENAMED })
    expect(renamed.status).toBe(200)
    const newSlug = `neuer-name-${suffix}`
    expect(responseString(renamed, 'slug')).toBe(newSlug)
    await expect(page).toHaveURL(new RegExp(`/c/${newSlug}$`))
    await page.goto(`/c/${slug}`)
    await expect(page).toHaveURL(new RegExp(`/c/${newSlug}$`))
    await expect(page.getByTestId('chat-channel-name')).toContainText(RENAMED)

    // An address that names no channel the member can see is just gone.
    await page.goto(`/c/nirgendwo-${suffix}`)
    await expect(page).toHaveURL(/\/$/)

    // Talks live under /v/<slug>, legacy ids included.
    const talk = await apiFetch(page, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })
    expect(talk.status).toBe(201)
    const talkId = responseString(talk, 'id')
    created.push(talkId)
    const talkSlug = `treff-${suffix}`
    expect(responseString(talk, 'slug')).toBe(talkSlug)
    await page.goto(`/v/${talkId}`)
    await expect(page).toHaveURL(new RegExp(`/v/${talkSlug}$`))
    await page.goto(`/v/${talkSlug}/chat`)
    await expect(page).toHaveURL(new RegExp(`/v/${talkSlug}/chat$`))
    // A text channel opened as a Talk is corrected.
    await page.goto(`/v/${newSlug}`)
    await expect(page).toHaveURL(new RegExp(`/c/${newSlug}$`))
  } finally {
    for (const id of created) await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
    await ctx.close()
  }
})
