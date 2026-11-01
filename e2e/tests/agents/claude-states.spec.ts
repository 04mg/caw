import { test, type Page } from '@playwright/test'
import { randomUUID } from 'node:crypto'
import { createTempGitRepo, cleanupWorkspace } from '../../fixtures/workspace'
import { isAgentAvailable, getAvailableAgents } from '../../fixtures/agents'
import {
  openControlCenter,
  waitForCardInColumn,
  getAgentStatuses,
  setWorkspace,
  waitForAppReady,
  type AgentStatus,
} from '../../fixtures/terminal'

// Claude Code 2.1 interactive states beyond the basic idle/working/idle cycle,
// driven through Caw's own terminal API/WebSocket (the PTY is real; only the
// typing bypasses the pane UI) and asserted on the Kanban board + status API:
//   - a tool permission dialog (Claude writes nothing to its transcript while
//     it is pending) must land in Needs Input, and approving it must go back
//     through Working to Idle;
//   - AskUserQuestion must land in Needs Input and return to Idle once
//     answered;
//   - /clear (Claude continues in a brand-new transcript) must leave the card
//     idle instead of stuck "working", and the next prompt must be tracked.
// These PTYs are not part of any workspace layout, and the board hides idle
// cards it cannot map to a workspace, so idle is asserted through the status
// API while Working / Needs Input are asserted on the board.

const AGENT_ID = 'claude'

test.describe('Claude Code interactive states', () => {
  test.skip(!isAgentAvailable(AGENT_ID), 'Claude Code not installed')

  let workspace: string
  let term: ClaudeTerm | undefined

  test.beforeEach(async ({ baseURL }) => {
    workspace = createTempGitRepo()
    await setWorkspace(baseURL!, workspace)
  })

  test.afterEach(async ({ baseURL }, testInfo) => {
    if (term && testInfo.status !== testInfo.expectedStatus) {
      console.log(`terminal ${term.id} output tail:\n${term.tail()}`)
    }
    await term?.kill(baseURL!)
    term = undefined
    cleanupWorkspace(workspace)
  })

  test('permission dialog -> Needs Input -> approved -> Idle', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    term = await ClaudeTerm.start(baseURL!, workspace, ['--permission-mode', 'default'])
    await board(page)
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle', 30_000)

    await term.prompt('Run the shell command `touch perm-check.txt` with the Bash tool (not the Write tool), then reply with exactly: touched.')
    await waitForCardInColumn(page, term.id, 'needs_input', 120_000)
    await expectStatus(baseURL!, term.id, (s) => s.status === 'waiting_input' && s.tool === 'Bash', 10_000)

    term.send('\r') // "1. Yes"
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle', 120_000)
  })

  test('AskUserQuestion -> Needs Input -> answered -> Idle', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    term = await ClaudeTerm.start(baseURL!, workspace)
    await board(page)
    await term.prompt('Use the AskUserQuestion tool to ask me which color I prefer (options: red, blue). Then reply with my answer in one word.')

    await waitForCardInColumn(page, term.id, 'needs_input', 120_000)
    await expectStatus(baseURL!, term.id, (s) => s.status === 'waiting_input' && s.tool === 'AskUserQuestion', 10_000)

    term.send('\r') // first option
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle', 120_000)
  })

  test('/clear keeps the card idle and follows the new conversation', async ({ page, baseURL }) => {
    test.setTimeout(300_000)
    term = await ClaudeTerm.start(baseURL!, workspace)
    await board(page)
    await term.prompt('Reply with exactly: first')
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle' && (s.details || '').includes('first'), 120_000)

    await term.prompt('/clear')
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle' && !s.title, 30_000)
    // The slash-command bookkeeping lines must not flip the card to working.
    await page.waitForTimeout(5_000)
    await expectStatus(baseURL!, term.id, (s) => s.status === 'idle' && !s.title, 1_000)

    await term.prompt('Reply with exactly: second')
    await expectStatus(baseURL!, term.id,
      (s) => s.status === 'idle' && (s.details || '').includes('second') && (s.title || '').includes('second'), 120_000)
  })
})

async function board(page: Page): Promise<void> {
  await waitForAppReady(page)
  await openControlCenter(page)
}

/** A Claude Code PTY created through Caw's terminal API and typed into over its WebSocket. */
class ClaudeTerm {
  private output = ''
  private constructor(readonly id: string, private ws: WebSocket) {
    ws.onmessage = (m) => {
      try {
        const msg = JSON.parse(String(m.data))
        if (msg.type === 'output' && typeof msg.data === 'string') this.output = (this.output + msg.data).slice(-20_000)
      } catch { /* ignore */ }
    }
  }

  static async start(baseURL: string, cwd: string, extraArgs: string[] = []): Promise<ClaudeTerm> {
    const agent = (await getAvailableAgents(baseURL)).find((a) => a.id === AGENT_ID)!
    const cmd = extraArgs.length
      ? ['claude', ...extraArgs]
      : agent.cmd
    const id = randomUUID()
    const res = await fetch(`${baseURL}/api/terminals`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ id, cwd, cmd, env: [['IS_SANDBOX', '1']] }),
    })
    if (!res.ok) throw new Error(`create terminal: ${res.status}`)
    const ws = new WebSocket(`${baseURL.replace(/^http/, 'ws')}/ws/terminals/${id}`)
    await new Promise<void>((resolve, reject) => { ws.onopen = () => resolve(); ws.onerror = () => reject(new Error('ws error')) })
    ws.send(JSON.stringify({ type: 'resize', cols: 140, rows: 40 }))
    const t = new ClaudeTerm(id, ws)
    await t.acceptTrust()
    return t
  }

  send(data: string): void {
    this.ws.send(JSON.stringify({ type: 'input', data }))
  }

  async prompt(text: string): Promise<void> {
    this.send(text)
    await sleep(400)
    this.send('\r')
  }

  private async acceptTrust(): Promise<void> {
    const deadline = Date.now() + 20_000
    while (Date.now() < deadline) {
      const plain = this.output.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, ' ').replace(/\s+/g, ' ').toLowerCase()
      if (plain.includes('trust this folder') || plain.includes('trust the files')) {
        // The highlighted default is "No, exit": move to "Yes, I trust this
        // folder". Give the dialog a moment to start accepting keys first.
        await sleep(1_500)
        this.send('\x1b[B')
        await sleep(500)
        this.send('\r')
        await sleep(3_000)
        return
      }
      if (plain.includes('for shortcuts') || plain.includes('shift+tab to cycle')) return
      await sleep(500)
    }
  }

  /** Plain-text tail of the terminal output (for failure diagnostics). */
  tail(n = 1500): string {
    return this.output.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, ' ').replace(/\s+/g, ' ').slice(-n)
  }

  async kill(baseURL: string): Promise<void> {
    try { this.ws.close() } catch { /* ignore */ }
    await fetch(`${baseURL}/api/terminals/${this.id}`, { method: 'DELETE' }).catch(() => {})
  }
}

async function expectStatus(
  baseURL: string,
  sessionId: string,
  pred: (s: AgentStatus) => boolean,
  timeout: number,
): Promise<void> {
  const deadline = Date.now() + timeout
  let last: AgentStatus | undefined
  while (Date.now() < deadline) {
    last = (await getAgentStatuses(baseURL)).find((s) => s.sessionId === sessionId)
    if (last && pred(last)) return
    await sleep(500)
  }
  throw new Error(`status of ${sessionId} did not match within ${timeout}ms; last=${JSON.stringify(last)}`)
}

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms))
}
