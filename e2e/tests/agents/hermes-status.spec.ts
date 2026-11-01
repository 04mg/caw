import { test, expect } from '@playwright/test'
import { createAgentStatusTests } from '../../fixtures/agent-tests'
import { isAgentAvailable } from '../../fixtures/agents'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import {
  launchAgent,
  openControlCenter,
  focusTerminal,
  typeInTerminal,
  sendEnter,
  sendInterrupt,
  waitForAgentSession,
  getExistingSessionIds,
  getAgentStatuses,
  waitForCardInColumn,
  waitForRedDot,
  setWorkspace,
  waitForAppReady,
} from '../../fixtures/terminal'

createAgentStatusTests({
  agentId: 'hermes',
  label: 'Hermes',
  workPrompt: 'list all files in this directory and describe each one in one sentence',
  failPrompt: 'read the file /nonexistent/xyz.txt and tell me what is in it',
  responseTimeout: 180_000,
})

// Deterministic Hermes state walk. Requires Hermes to be configured against
// the Caw test LLM proxy (OpenAI-compatible, see testenv llm-proxy README),
// whose CAWTEST:<scenario> directives script the model's tool calls:
//
//   CAW_E2E_LLM_PROXY=1 CAW_PORT=<port> npx playwright test tests/agents/hermes-status.spec.ts
test.describe('Hermes status tracking (scripted LLM proxy)', () => {
  test.skip(!isAgentAvailable('hermes'), 'Hermes not installed')
  test.skip(process.env.CAW_E2E_LLM_PROXY !== '1', 'set CAW_E2E_LLM_PROXY=1 when Hermes uses the CAWTEST proxy')

  let workspace: string
  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })
  test.afterEach(() => cleanupWorkspace(workspace))

  async function status(baseURL: string, sessionId: string) {
    return (await getAgentStatuses(baseURL)).find((s) => s.sessionId === sessionId)
  }

  async function waitForStatus(baseURL: string, sessionId: string, want: string, timeout = 60_000) {
    const deadline = Date.now() + timeout
    let last = ''
    while (Date.now() < deadline) {
      const s = await status(baseURL, sessionId)
      last = s?.status ?? ''
      if (last === want) return s!
      await new Promise((r) => setTimeout(r, 250))
    }
    throw new Error(`status of ${sessionId} never became ${want} (last: ${last})`)
  }

  test('working, needs input, ESC vs Ctrl+C, API error', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    await waitForAppReady(page)
    await openControlCenter(page)
    const existing = await getExistingSessionIds(baseURL!, 'hermes')
    await launchAgent(page, 'Hermes')
    const session = await waitForAgentSession(baseURL!, 'hermes', 30_000, existing)
    const id = session.sessionId
    await waitForCardInColumn(page, id, 'idle', 30_000)
    await focusTerminal(page, id)
    await page.waitForTimeout(8_000) // banner + prompt_toolkit startup

    const prompt = async (text: string) => {
      await focusTerminal(page, id)
      await typeInTerminal(page, id, text)
      await sendEnter(page)
    }

    // Tool run: executing (terminal tool) in the working column, then idle.
    await prompt('CAWTEST:shell:sleep 8')
    const exec = await waitForStatus(baseURL!, id, 'executing')
    expect(exec.tool).toBe('terminal')
    await waitForCardInColumn(page, id, 'working', 10_000)
    await waitForStatus(baseURL!, id, 'idle', 60_000)
    await waitForCardInColumn(page, id, 'idle', 10_000)

    // clarify tool: needs input until answered (Enter picks option 1).
    await prompt('CAWTEST:ask')
    const asking = await waitForStatus(baseURL!, id, 'waiting_input')
    expect(asking.tool).toBe('clarify')
    await waitForCardInColumn(page, id, 'needs_input', 10_000)
    await focusTerminal(page, id)
    await sendEnter(page)
    await waitForStatus(baseURL!, id, 'idle', 60_000)

    // ESC is not an interrupt key in Hermes: the turn keeps running.
    await prompt('CAWTEST:shell:sleep 20')
    await waitForStatus(baseURL!, id, 'executing')
    await page.keyboard.press('Escape')
    await page.waitForTimeout(5_000) // > two watcher poll intervals
    expect((await status(baseURL!, id))?.status).toBe('executing')
    // Ctrl+C interrupts: red dot, idle column.
    await sendInterrupt(page)
    await waitForStatus(baseURL!, id, 'interrupted', 15_000)
    await waitForRedDot(page, id, 10_000)
    await waitForCardInColumn(page, id, 'idle', 10_000)

    // An API error persists nothing in state.db; the turn must still end.
    await prompt('CAWTEST:fail:400')
    const failed = await waitForStatus(baseURL!, id, 'idle', 30_000)
    expect(failed.details).toMatch(/^API error/)
  })
})
