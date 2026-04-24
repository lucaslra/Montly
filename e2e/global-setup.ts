import { chromium, FullConfig } from '@playwright/test'
import * as fs from 'fs'
import * as path from 'path'

const ADMIN_USER = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PASS = process.env.ADMIN_PASS ?? 'adminpass123'

async function waitForApp(baseURL: string): Promise<void> {
  for (let attempt = 0; attempt < 30; attempt++) {
    try {
      const res = await fetch(`${baseURL}/api/auth/setup`)
      if (res.ok) return
    } catch {
      // not ready yet
    }
    await new Promise(r => setTimeout(r, 2000))
  }
  throw new Error(`App at ${baseURL} did not respond after 60 seconds`)
}

export default async function globalSetup(config: FullConfig): Promise<void> {
  const baseURL = config.projects[0].use.baseURL as string

  // Ensure the app is up before proceeding (belt-and-suspenders on top of Docker healthcheck)
  await waitForApp(baseURL)

  // Bootstrap the admin account (idempotent: 409 is fine if already created)
  const setupRes = await fetch(`${baseURL}/api/auth/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: ADMIN_USER, password: ADMIN_PASS }),
  })
  if (!setupRes.ok && setupRes.status !== 409) {
    throw new Error(`Admin setup failed: ${setupRes.status} ${await setupRes.text()}`)
  }

  // Create the receipt fixture used by completions tests
  const fixturesDir = path.join(__dirname, 'fixtures')
  fs.mkdirSync(fixturesDir, { recursive: true })
  // Minimal 1×1 red pixel PNG
  const pngB64 =
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADklEQVQI12P4z8BQDwAEgAF/QualIQAAAABJRU5ErkJggg=='
  fs.writeFileSync(path.join(fixturesDir, 'receipt.png'), Buffer.from(pngB64, 'base64'))

  // Log in via browser and persist the session so individual specs skip the login flow
  const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-setuid-sandbox'] })
  const page = await browser.newPage()

  await page.goto(baseURL)
  await page.fill('#login-username', ADMIN_USER)
  await page.fill('#login-password', ADMIN_PASS)
  await page.click('button[type="submit"]')
  await page.waitForSelector('.app-header', { timeout: 15000 })

  const authDir = path.join(__dirname, 'playwright', '.auth')
  fs.mkdirSync(authDir, { recursive: true })
  await page.context().storageState({ path: path.join(authDir, 'admin.json') })

  await browser.close()
}
