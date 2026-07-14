import { execSync } from 'child_process'
import * as fs from 'fs'
import * as os from 'os'
import * as path from 'path'

let counter = 0

export function createTempGitRepo(): string {
  counter++
  const base = path.join(os.tmpdir(), 'caw-e2e')
  if (!fs.existsSync(base)) {
    fs.mkdirSync(base, { recursive: true })
  }
  const dir = path.join(base, `ws-${process.pid}-${counter}`)

  fs.mkdirSync(dir, { recursive: true })

  git(dir, 'init')
  git(dir, ['config', 'user.email', 'test@caw.local'])
  git(dir, ['config', 'user.name', 'Caw E2E'])

  fs.writeFileSync(path.join(dir, 'README.md'), '# Caw E2E Test Repo\n')
  git(dir, ['add', '-A'])
  git(dir, ['commit', '-m', 'init'])

  return dir
}

export function cleanupWorkspace(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true })
  } catch {
    // best-effort
  }
}

function git(cwd: string, args: string | string[]): void {
  const arr = Array.isArray(args) ? args : args.split(' ')
  execSync(`git ${arr.join(' ')}`, { cwd, stdio: 'ignore' })
}