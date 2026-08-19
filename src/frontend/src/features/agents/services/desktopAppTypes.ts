import { Globe, Monitor, Gamepad2, Cpu, Terminal } from 'lucide-react'
import { type DesktopAppType } from '../types'

// desktopAppTypes is the client-side registry of graphical applications
// that can be launched as xpra desktop sessions. It mirrors the backend
// ListDesktopApps registry; the New Tab menu fetches the backend list
// (filtered by availability) and uses this map to resolve the icon and
// any extra env. User-defined apps (from prefs.desktopApps) are merged in
// at render time and fall back to the Globe icon.
export const desktopAppTypes: Record<string, DesktopAppType> = {
  browser: {
    id: 'browser',
    label: 'Browser',
    cmd: ['firefox-esr', '--new-window'],
    icon: Globe,
  },
  xterm: {
    id: 'xterm',
    label: 'XTerm',
    cmd: ['xterm'],
    icon: Terminal,
  },
  zcode: {
    id: 'zcode',
    label: 'ZCode',
    cmd: ['zcode'],
    icon: Monitor,
  },
  'deepseek-harness': {
    id: 'deepseek-harness',
    label: 'DeepSeek Harness',
    cmd: ['deepseek-harness'],
    icon: Cpu,
  },
  unity: {
    id: 'unity',
    label: 'Unity',
    cmd: ['unity-editor'],
    icon: Gamepad2,
  },
}