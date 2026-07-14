#!/usr/bin/env node
/**
 * Cross-platform test runner for Caw e2e tests.
 *
 * Starts a fresh caw server with isolated DB/state, runs Playwright tests,
 * then tears everything down. Works on Windows, macOS, and Linux.
 *
 * Usage:
 *   node e2e/run-tests.mjs [extra playwright args]
 *
 * Environment variables:
 *   CAW_PORT  — port to run caw on (default: 8080)
 *   CAW_ARGS  — extra args to pass to caw server
 */

import { spawn, execSync } from 'node:child_process'
import { existsSync, unlinkSync, mkdirSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { randomUUID } from 'node:crypto'

const PORT = process.env.CAW_PORT || '8080'
const EXTRA_ARGS = process.env.CAW_ARGS || ''
const ROOT = resolve(import.meta.dirname, '..')
const E2E_DIR = resolve(import.meta.dirname)
const BINARY = process.platform === 'win32' ? 'caw.exe' : 'caw'

// Create isolated temp paths for this run
const runId = randomUUID().slice(0, 8)
const stateDir = join(tmpdir(), `caw-e2e-${runId}`)
mkdirSync(stateDir, { recursive: true })

const dbPath = join(stateDir, 'caw.db')
const statePath = join(stateDir, 'caw-state.json')

// Ensure the binary exists
const binaryPath = join(ROOT, BINARY)
if (!existsSync(binaryPath)) {
  console.error(`ERROR: ${BINARY} not found at ${binaryPath}`)
  console.error('Run "make build" first.')
  process.exit(1)
}

console.log(`Run ID:        ${runId}`)
console.log(`DB path:       ${dbPath}`)
console.log(`State path:    ${statePath}`)
console.log(`Port:          ${PORT}`)
console.log()

// Start caw server
const serverEnv = {
  ...process.env,
  CAW_DB_PATH: dbPath,
  CAW_STATE_PATH: statePath,
}

const serverArgs = ['server', '--port', PORT, ...EXTRA_ARGS.split(' ').filter(Boolean)]
const server = spawn(binaryPath, serverArgs, {
  cwd: ROOT,
  env: serverEnv,
  stdio: ['ignore', 'pipe', 'pipe'],
})

let serverOutput = ''
server.stdout.on('data', (d) => { serverOutput += d.toString() })
server.stderr.on('data', (d) => { serverOutput += d.toString() })

// Wait for server to be ready
function waitForServer(timeoutMs = 15000) {
  return new Promise((resolve, reject) => {
    const start = Date.now()
    const check = async () => {
      try {
        const res = await fetch(`http://127.0.0.1:${PORT}/api/agents`)
        if (res.ok) {
          resolve()
          return
        }
      } catch {
        // not ready yet
      }
      if (Date.now() - start > timeoutMs) {
        reject(new Error(`Server did not start within ${timeoutMs}ms.\nOutput:\n${serverOutput}`))
        return
      }
      setTimeout(check, 500)
    }
    check()
  })
}

function cleanup() {
  try {
    server.kill('SIGTERM')
  } catch {
    // already dead
  }
  try {
    if (existsSync(dbPath)) unlinkSync(dbPath)
    if (existsSync(dbPath + '-wal')) unlinkSync(dbPath + '-wal')
    if (existsSync(dbPath + '-shm')) unlinkSync(dbPath + '-shm')
    if (existsSync(statePath)) unlinkSync(statePath)
  } catch {
    // best effort
  }
}

process.on('SIGINT', () => { cleanup(); process.exit(1) })
process.on('SIGTERM', () => { cleanup(); process.exit(1) })

try {
  console.log('Starting caw server...')
  await waitForServer()
  console.log('Server ready.\n')

  // Run playwright tests
  const testArgs = ['playwright', 'test', '--reporter=list', ...process.argv.slice(2)]
  const result = process.platform === 'win32'
    ? spawn('cmd.exe', ['/c', 'npx', ...testArgs], {
        cwd: E2E_DIR,
        env: { ...process.env, CAW_PORT: PORT },
        stdio: 'inherit',
      })
    : spawn('npx', testArgs, {
        cwd: E2E_DIR,
        env: { ...process.env, CAW_PORT: PORT },
        stdio: 'inherit',
      })

  await new Promise((resolve) => {
    result.on('close', (code) => {
      cleanup()
      process.exit(code ?? 1)
    })
  })
} catch (err) {
  console.error(err.message)
  cleanup()
  process.exit(1)
}
