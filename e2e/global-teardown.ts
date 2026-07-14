import * as fs from 'fs'
import * as os from 'os'
import * as path from 'path'

export default async function globalTeardown() {
  const base = path.join(os.tmpdir(), 'caw-e2e-data')
  if (fs.existsSync(base)) {
    try {
      fs.rmSync(base, { recursive: true, force: true })
    } catch {
      // best-effort — file locks may still be held
    }
  }
  const wsBase = path.join(os.tmpdir(), 'caw-e2e')
  if (fs.existsSync(wsBase)) {
    try {
      fs.rmSync(wsBase, { recursive: true, force: true })
    } catch {
      // best-effort
    }
  }
}