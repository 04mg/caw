import { type Page, expect } from '@playwright/test'
import { getAvailableAgents, type AgentInfo } from './agents'

export async function waitForAppReady(page: Page): Promise<void> {
  await page.goto('/', { waitUntil: 'domcontentloaded' })
  // Wait for the status bar to appear — the React app must be hydrated
  await expect(page.getByTestId('status-bar-control-center')).toBeVisible({ timeout: 60_000 })
  await page.waitForTimeout(500)
}

export async function openCommandPalette(page: Page): Promise<void> {
  await page.keyboard.press('Alt+P')
  await expect(page.getByTestId('command-palette-input')).toBeVisible({ timeout: 10_000 })
}

export async function launchAgent(page: Page, agentLabel: string): Promise<void> {
  await openCommandPalette(page)
  const input = page.getByTestId('command-palette-input')
  await input.fill(`Launch ${agentLabel}`)
  const item = page.locator(`[data-testid^="command-palette-item-agent-"]`).filter({ hasText: agentLabel })
  await expect(item).toBeVisible()
  await item.click()
}

export async function launchTerminal(page: Page): Promise<void> {
  await openCommandPalette(page)
  const input = page.getByTestId('command-palette-input')
  await input.fill('New Terminal')
  await page.getByTestId('command-palette-item-new-terminal').click()
}

export async function focusTerminal(page: Page, terminalId: string): Promise<void> {
  const panel = page.getByTestId(`terminal-panel-${terminalId}`)
  await expect(panel).toBeVisible({ timeout: 10_000 })
  const textarea = panel.locator('textarea[aria-label="Terminal input"]')
  await textarea.focus()
}

export async function typeInTerminal(page: Page, _terminalId: string, text: string): Promise<void> {
  await page.keyboard.type(text)
}

export async function sendEnter(page: Page): Promise<void> {
  await page.keyboard.press('Enter')
}

export async function sendInterrupt(page: Page): Promise<void> {
  await page.keyboard.press('Control+c')
}

export async function openControlCenter(page: Page): Promise<void> {
  await page.getByTestId('status-bar-control-center').click()
  await expect(page.getByTestId('kanban-board')).toBeVisible({ timeout: 10_000 })
}

export async function closeControlCenter(page: Page): Promise<void> {
  await page.getByTestId('status-bar-control-center').click()
  await expect(page.getByTestId('kanban-board')).toBeHidden({ timeout: 10_000 })
}

export interface AgentStatus {
  sessionId: string
  agentId: string
  status: string
  cwd: string
  tool: string
  details: string
  title: string
  sequence: number
}

export async function getAgentStatuses(baseURL?: string): Promise<AgentStatus[]> {
  const url = `${baseURL ?? ''}/api/agents/statuses`
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(10_000) })
    if (!res.ok) return []
    const json = await res.json()
    const data = json?.data
    if (Array.isArray(data)) return data
    if (data && typeof data === 'object') {
      return Object.values(data)
    }
  } catch {
    // server might be temporarily unavailable
  }
  return []
}

export async function waitForAgentSession(
  baseURL: string,
  agentId: string,
  timeout = 30_000,
  excludeIds: Set<string> = new Set(),
): Promise<AgentStatus> {
  const start = Date.now()
  while (Date.now() - start < timeout) {
    const statuses = await getAgentStatuses(baseURL)
    const found = statuses.find((s) => s.agentId === agentId && !excludeIds.has(s.sessionId))
    if (found) return found
    await sleep(500)
  }
  throw new Error(`Agent ${agentId} session not found within ${timeout}ms`)
}

export async function getExistingSessionIds(baseURL: string, agentId: string): Promise<Set<string>> {
  const statuses = await getAgentStatuses(baseURL)
  return new Set(statuses.filter((s) => s.agentId === agentId).map((s) => s.sessionId))
}

export async function waitForAgentCount(
  baseURL: string,
  count: number,
  timeout = 30_000,
): Promise<void> {
  const start = Date.now()
  while (Date.now() - start < timeout) {
    const statuses = await getAgentStatuses(baseURL)
    if (statuses.length >= count) return
    await sleep(500)
  }
  throw new Error(`Expected ${count} agents within ${timeout}ms`)
}

export async function waitForCardInColumn(
  page: Page,
  sessionId: string,
  columnId: 'idle' | 'working' | 'needs_input',
  timeout = 120_000,
): Promise<void> {
  const deadline = Date.now() + timeout
  while (Date.now() < deadline) {
    const card = page.locator(`[data-card-id="${sessionId}"]`)
    const cardExists = await card.count()
    if (cardExists) {
      const column = page.getByTestId(`kanban-column-${columnId}`)
      const colHasCard = await column.locator(`[data-card-id="${sessionId}"]`).count()
      if (colHasCard) return
    }
    await sleep(500)
  }
  throw new Error(`Card ${sessionId} not in column ${columnId} within ${timeout}ms`)
}

export async function getCardColumn(
  page: Page,
  sessionId: string,
): Promise<string | null> {
  for (const col of ['idle', 'working', 'needs_input']) {
    const column = page.getByTestId(`kanban-column-${col}`)
    const has = await column.locator(`[data-card-id="${sessionId}"]`).count()
    if (has) return col
  }
  return null
}

export async function setWorkspace(baseURL: string, workspacePath: string): Promise<void> {
  const wsId = `ws-${Date.now()}`
  const name = workspacePath.split(/[\\/]/).pop() || 'workspace'
  const state = {
    workspaces: [{
      id: wsId,
      path: workspacePath,
      name: name,
      emoji: '',
      layouts: [],
      activeTabIndex: 0,
      activePaneId: '',
      enableWorktrees: true,
      tabGroupsJson: '',
    }],
    activeWorkspaceId: wsId,
  }
  for (let attempt = 0; attempt < 5; attempt++) {
    try {
      const controller = new AbortController()
      const timeout = setTimeout(() => controller.abort(), 30_000)
      const res = await fetch(`${baseURL}/api/workspaces`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(state),
        signal: controller.signal,
      })
      clearTimeout(timeout)
      if (!res.ok) throw new Error(`Failed to set workspace: ${res.status}`)
      return
    } catch (e) {
      if (attempt === 4) throw e
      await new Promise((r) => setTimeout(r, 2000))
    }
  }
}

export async function getAvailableAgentsForPage(baseURL?: string): Promise<AgentInfo[]> {
  return getAvailableAgents(baseURL)
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}