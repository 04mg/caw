import { useState, useEffect, useCallback, useRef, type ElementType } from 'react'
import { Dialog, DialogContent, DialogTitle, DialogClose } from '@/components/dialog'
import { Slider } from '@/components/slider'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/select'

import { Palette, Bot, Terminal, Check, Moon, Sun, Monitor, ChartSpline, ArrowLeft, LogIn, ExternalLink, Loader2, Folder, Settings as SettingsIcon, X, Bell, Mic, Download, HardDrive, Globe, Trash2, Minus, RefreshCw, Keyboard, PawPrint } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, Claude, Codex, GithubCopilot, OpenRouter } from '@lobehub/icons'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { getAgentCmdOverrides, setAgentCmdOverride, setDefaultNewAgent as setPrefDefaultNewAgent, setDisabledAgents as setPrefDisabledAgents, setDefaultShell as setPrefDefaultShell, setParkedTerminals as setPrefParkedTerminals, loadPrefs, getHotkey, setHotkey as setPrefHotkey, resetHotkey as resetPrefHotkey, resetAllHotkeys as resetAllPrefHotkeys, DEFAULT_HOTKEYS, HOTKEY_LABELS, DEFAULT_PARKED_TERMINALS } from '@/features/prefs/stores/prefsStore'
import { getDeviceId, getDeviceName } from '@/features/devices/services/device'
import { setAllTerminalFontSizes, setAllTerminalThemes } from '@/features/terminal/services/terminalRegistry'
import { isVoiceSupported } from '@/features/voice-mode/hooks/useVoiceMode'
import {
	getVoiceMode,
	setVoiceMode,
	fetchLanguages,
	fetchModels,
	downloadModel,
	isModelCached,
	deleteModel,
	isKrokoSupported,
	getKrokoLanguage,
	setKrokoLanguage,
	type KrokoModel,
	type KrokoLanguage,
} from '@/features/voice-mode/services/krokoAsr'
import { SettingsItem } from './SettingsItem'
import { HotkeyRecorder } from './HotkeyRecorder'
import { SaveToast } from './SaveToast'
import { PetsSettingsPanel } from './PetsSettingsPanel'
import cawLogoSvg from '@/assets/app-logo.svg'

const EMOJI_MAP: Record<string, string> = {
  feat: '✨',
  fix: '🐛',
  chore: '🔧',
  docs: '📝',
  refactor: '♻️',
  style: '💄',
  release: '',
}

function parseChangelog(body: string): string[] {
  return body
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => {
      if (!line) return false
      if (line.startsWith('**Full Changelog**')) return false
      if (line.startsWith('* release:')) return false
      if (line.startsWith('* ')) return true
      return false
    })
    .map((line) => {
      const match = line.match(/^\*\s*(\w+):\s*(.+)/)
      if (!match) return line.replace(/^\*\s*/, '')
      const [, type, rest] = match
      const emoji = EMOJI_MAP[type] || ''
      const prefix = emoji ? `${emoji} ` : ''
      const cleaned = rest.replace(/ by @\S+.*$/, '').trim()
      return prefix + cleaned
    })
    .filter(Boolean)
}


interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialSection?: string
}

type Section = 'appearance' | 'agents' | 'terminal' | 'workspaces' | 'limits' | 'notifications' | 'voice' | 'updates' | 'hotkeys' | 'pets'

export function SettingsDialog({ open, onOpenChange, initialSection }: SettingsDialogProps) {
  const [activeSection, setActiveSection] = useState<Section>('updates')
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768)
  const [dialogSize, setDialogSize] = useState({ w: 660, h: 590 })
  const [mobileSectionSelected, setMobileSectionSelected] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')
  const [terminalTheme, setTerminalTheme] = useState<'dark' | 'light'>('dark')
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])
  const [fontSize, setFontSize] = useState(13)
  const [shellPath, setShellPath] = useState('')
  const [parkedLimit, setParkedLimit] = useState(DEFAULT_PARKED_TERMINALS)
  const [scrollSensitivity, setScrollSensitivity] = useState(0.02)
  const [scrollFriction, setScrollFriction] = useState(0.85)
  const [scrollVelocityThreshold, setScrollVelocityThreshold] = useState(0.05)
  const [scrollGrace, setScrollGrace] = useState(1200)
  const [antigravityKey, setAntigravityKey] = useState('')
  const [opencodeCookie, setOpencodeCookie] = useState('')
  const [opencodeWorkspace, setOpencodeWorkspace] = useState('')
  const [ollamaCookie, setOllamaCookie] = useState('')
  const [openrouterApiKey, setOpenrouterApiKey] = useState('')
  const [claudeAccessToken, setClaudeAccessToken] = useState('')
  const [codexAccessToken, setCodexAccessToken] = useState('')
  const [copilotToken, setCopilotToken] = useState('')
  const [copilotEnterpriseHost, setCopilotEnterpriseHost] = useState('')
  const [copilotDeviceFlow, setCopilotDeviceFlow] = useState<'idle' | 'waiting' | 'polling' | 'done' | 'error'>('idle')
  const [copilotDeviceCode, setCopilotDeviceCode] = useState('')
  const [copilotUserCode, setCopilotUserCode] = useState('')
  const [copilotVerificationURI, setCopilotVerificationURI] = useState('')
  const [copilotInterval, setCopilotInterval] = useState(5)
  const copilotIntervalRef = useRef(5)
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const [copilotDeviceError, setCopilotDeviceError] = useState('')
  const [selectedLimitProvider, setSelectedLimitProvider] = useState<'claude' | 'codex' | 'copilot' | 'antigravity' | 'opencode' | 'ollama' | 'openrouter'>('claude')
  const [limitStep, setLimitStep] = useState<1 | 2>(1)
  const [agentStep, setAgentStep] = useState<1 | 2>(1)
  const [selectedAgentId, setSelectedAgentId] = useState<string>('')
  const [agentCmdDraft, setAgentCmdDraft] = useState<string>('')
  const [agyInstalled, setAgyInstalled] = useState(true)
  const [claudeInstalled, setClaudeInstalled] = useState(true)
  const [codexInstalled, setCodexInstalled] = useState(true)
  const [quotas, setQuotas] = useState<Record<string, { error?: string }> | null>(null)
  const [defaultNewAgent, setDefaultNewAgent] = useState('none')
  const [availableAgents, setAvailableAgents] = useState<any[]>([])
  const [saveStatus, setSaveStatus] = useState<'idle' | 'success' | 'error'>('idle')
  const saveTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)

  const [hotkeyRecording, setHotkeyRecording] = useState<string | null>(null)
  const [hotkeyError, setHotkeyError] = useState('')

  const [pushEnabled, setPushEnabled] = useState(false)
  const [pushNeedsInput, setPushNeedsInput] = useState(true)
  const [pushFinished, setPushFinished] = useState(true)
  const [pushPermission, setPushPermission] = useState<NotificationPermission | 'unsupported'>('default')
  const [pushSupported] = useState(() => {
    if (typeof navigator === 'undefined' || !('serviceWorker' in navigator)) return false
    // Safari (incl. iOS 16.4+) exposes pushManager on ServiceWorkerRegistration,
    // not on window. iOS requires the app to be installed as a PWA (standalone).
    const hasPushManager = 'PushManager' in window || 'pushManager' in ServiceWorkerRegistration.prototype
    return hasPushManager
  })
  const [pushIOSPWA] = useState(() => {
    if (typeof navigator === 'undefined') return false
    const ua = navigator.userAgent || ''
    const isIOS = /iPad|iPhone|iPod/.test(ua) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
    const isStandalone = window.matchMedia('(display-mode: standalone)').matches || (navigator as any).standalone === true
    return isIOS && !isStandalone
  })
  const [pushSubscribed, setPushSubscribed] = useState(false)
  const [pushBusy, setPushBusy] = useState(false)
  const [pushError, setPushError] = useState('')
  const [soundEnabled, setSoundEnabled] = useState(true)
  const [voiceLanguage, setVoiceLanguage] = useState('')
  const [voiceMode, setVoiceModeState] = useState<'browser' | 'local'>('browser')
  const [krokoLanguages, setKrokoLanguages] = useState<KrokoLanguage[]>([])
  const [krokoModels, setKrokoModels] = useState<KrokoModel[]>([])
  const [krokoLanguage, setKrokoLanguageState] = useState('')
  const [krokoModelCache, setKrokoModelCache] = useState<Record<string, boolean>>({})
  const [krokoDownloading, setKrokoDownloading] = useState<string | null>(null)
  const [krokoDownloadProgress, setKrokoDownloadProgress] = useState<{ downloaded: number, total: number } | null>(null)
  const [krokoLoading, setKrokoLoading] = useState(true)
  const hasInstalledModel = Object.values(krokoModelCache).some(Boolean)
  const isSecureContext = typeof window !== 'undefined' && (window.isSecureContext || window.location.hostname === 'localhost')

  const [appVersion, setAppVersion] = useState('')
  const [updateState, setUpdateState] = useState<'idle' | 'checking' | 'available' | 'updating' | 'updated' | 'latest' | 'error'>('idle')
  const [updateLatestVersion, setUpdateLatestVersion] = useState('')
  const [updateMessage, setUpdateMessage] = useState('')
  const [changelog, setChangelog] = useState<{ tagName: string; body: string; htmlUrl: string } | null>(null)
  const [changelogLoading, setChangelogLoading] = useState(false)

  const loadQuotaSettings = useCallback(async () => {
    try {
      const res = await fetch('/api/quotas/settings')
      if (res.ok) {
        const data = (await res.json())?.data
        setAntigravityKey(data.antigravity?.apiKey || '')
        setOpencodeCookie(data.opencode?.cookie || '')
        setOpencodeWorkspace(data.opencode?.workspaceId || '')
        setOllamaCookie(data.ollama?.cookie || '')
        setOpenrouterApiKey(data.openrouter?.apiKey || '')
        setClaudeAccessToken(data.claude?.accessToken || '')
        setCodexAccessToken(data.codex?.accessToken || '')
        setCopilotToken(data.copilot?.token || '')
        setCopilotEnterpriseHost(data.copilot?.enterpriseHost || '')
        setAgyInstalled(data.antigravity?.installed !== 'false')
        setClaudeInstalled(data.claude?.installed !== 'false')
        setCodexInstalled(data.codex?.installed !== 'false')
      }
    } catch (e) {
      console.error('Failed to load quota settings', e)
    }
  }, [])

  const loadQuotas = useCallback(async () => {
    try {
      const res = await fetch('/api/quotas')
      if (res.ok) {
        const json = await res.json()
        setQuotas(json?.data)
      }
    } catch (e) {
      console.error('Failed to load quotas', e)
    }
  }, [])

  // Reset limitStep when activeSection changes or dialog closes
  useEffect(() => {
    setLimitStep(1)
    setAgentStep(1)
  }, [activeSection, open])

  // Track viewport for responsive layout
  useEffect(() => {
    const handleResize = () => setIsMobile(window.innerWidth < 768)
    window.addEventListener('resize', handleResize)
    return () => window.removeEventListener('resize', handleResize)
  }, [])

  // Desktop dialog scales with the viewport: it grows to fill the screen
  // (whichever side is the limiter), never exceeds the screen height, and
  // stays a bit smaller than the old fixed 720x650 at its base. Both width
  // and height get a flat ~100px reduction so the dialog feels shorter than
  // full-screen.
  useEffect(() => {
    if (isMobile) return
    const measure = () => {
      const baseW = 660
      const baseH = 590
      const scale = Math.min((window.innerWidth - 64) / baseW, (window.innerHeight - 96) / baseH, 1.2)
      setDialogSize({
        w: Math.max(400, Math.round(baseW * scale) - 100),
        h: Math.max(360, Math.round(baseH * scale) - 100),
      })
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [isMobile])

  // Reset mobile section selection when dialog closes
  useEffect(() => {
    if (!open) setMobileSectionSelected(false)
  }, [open])

  // Clear any pending save-status auto-hide timer when the dialog closes
  useEffect(() => {
    if (!open && saveTimerRef.current) {
      clearTimeout(saveTimerRef.current)
      saveTimerRef.current = undefined
      setSaveStatus('idle')
    }
  }, [open])

  // Load settings on open
  useEffect(() => {
    if (open) {
      if (initialSection) {
        setActiveSection(initialSection as Section)
      }

      const savedTheme = (localStorage.getItem('caw:theme') as 'light' | 'dark' | 'system') || 'system'
      setTheme(savedTheme)

      const savedTerminalTheme = (localStorage.getItem('caw:terminalTheme') as 'dark' | 'light') || 'dark'
      setTerminalTheme(savedTerminalTheme)

      loadPrefs().then((p) => {
        setDisabledAgents(p.disabledAgents)
        setDefaultNewAgent(p.defaultNewAgent)
        setShellPath(p.defaultShell)
        const v = p.parkedTerminals
        setParkedLimit(Number.isFinite(v) ? Math.max(0, Math.min(16, Math.floor(v))) : DEFAULT_PARKED_TERMINALS)
      })

      const savedFontSize = parseInt(localStorage.getItem('caw:terminalFontSize') || '13', 10)
      setFontSize(isNaN(savedFontSize) ? 13 : Math.max(8, Math.min(32, savedFontSize)))

      setScrollSensitivity(parseFloat(localStorage.getItem('caw:terminalScrollSensitivity') || '0.02'))
      setScrollFriction(parseFloat(localStorage.getItem('caw:terminalScrollFriction') || '0.85'))
      setScrollVelocityThreshold(parseFloat(localStorage.getItem('caw:terminalScrollVelocityThreshold') || '0.05'))
      setScrollGrace(parseInt(localStorage.getItem('caw:terminalScrollGrace') || '1200', 10))

      fetch('/api/agents')
        .then((res) => res.ok ? res.json() : Promise.resolve({ data: [] }))
        .then((json) => {
          const data = json?.data
          if (Array.isArray(data)) {
            setAvailableAgents(data)
          }
        })
        .catch(() => {})

      loadQuotaSettings()
      loadQuotas()

      // Load push notification state
      setSoundEnabled(localStorage.getItem('caw:soundEnabled') !== '0')
      setVoiceLanguage(localStorage.getItem('caw:voiceLanguage') || '')
      setVoiceModeState(getVoiceMode())
      setKrokoLanguageState(getKrokoLanguage())

      // Load Kroko models and languages
      if (isKrokoSupported()) {
        setKrokoLoading(true)
        Promise.all([fetchLanguages(), fetchModels()])
          .then(([langs, models]) => {
            setKrokoLanguages(langs)
            setKrokoModels(models)
            const lang = getKrokoLanguage()
            if (lang && langs.some((l) => l.iso === lang)) {
              setKrokoLanguageState(lang)
            } else if (langs.length > 0) {
              setKrokoLanguageState(langs[0].iso)
              setKrokoLanguage(langs[0].iso)
            }
            // Check cache status for all models
            Promise.all(models.map((m) => isModelCached(m.url).then((c) => [m.url, c] as const)))
              .then((results) => {
                const cacheMap: Record<string, boolean> = {}
                for (const [url, cached] of results) cacheMap[url] = cached
                setKrokoModelCache(cacheMap)
              })
              .catch(() => {})
          })
          .catch(() => {})
          .finally(() => setKrokoLoading(false))
      }

      if (pushSupported && 'Notification' in window) {
        setPushPermission(Notification.permission)
      } else if (!pushSupported && !pushIOSPWA) {
        setPushPermission('unsupported')
      }
      // Load per-device push prefs
      const deviceId = getDeviceId()
      fetch('/api/push/devices')
        .then((res) => res.ok ? res.json() : Promise.resolve({ data: [] }))
        .then((json) => {
          const devices = json?.data || json || []
          const myDevice = Array.isArray(devices) ? devices.find((d: any) => d.deviceId === deviceId) : null
          if (myDevice) {
            setPushEnabled(myDevice.enabled || false)
            setPushNeedsInput(myDevice.needsInput !== false)
            setPushFinished(myDevice.finished !== false)
          } else {
            setPushEnabled(false)
            setPushNeedsInput(true)
            setPushFinished(true)
          }
        })
        .catch(() => {})
      if (pushSupported && navigator.serviceWorker) {
        navigator.serviceWorker.ready.then(async (reg) => {
          try {
            const sub = await reg.pushManager.getSubscription()
            setPushSubscribed(!!sub)
          } catch {
            setPushSubscribed(false)
          }
        })
      }

      // Reset device login state
      setCopilotDeviceFlow('idle')
      setCopilotDeviceCode('')
      setCopilotUserCode('')
      setCopilotVerificationURI('')
      setCopilotInterval(5)
      copilotIntervalRef.current = 5
      setCopilotDeviceError('')

      fetch('/api/version')
        .then((res) => res.ok ? res.json() : Promise.resolve({ data: null }))
        .then((json) => {
          const v = json?.data?.version
          if (v) setAppVersion(v)
        })
        .catch(() => {})

      setChangelogLoading(true)
      setChangelog(null)
      fetch('/api/update/changelog')
        .then((res) => res.ok ? res.json() : Promise.resolve({ data: null }))
        .then((json) => {
          const c = json?.data
          if (c?.body) setChangelog(c)
        })
        .catch(() => {})
        .finally(() => setChangelogLoading(false))
    }
  }, [open, loadQuotaSettings, loadQuotas, initialSection, pushSupported, pushIOSPWA])

  const saveSettings = useCallback(async (
    agKey: string,
    ocCookie: string,
    ocWorkspace: string,
    olCookie: string,
    clToken: string,
    cdToken: string,
    cpToken: string,
    cpHost: string,
    orKey: string,
  ) => {
    try {
      const res = await fetch('/api/quotas/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          antigravity: { apiKey: agKey },
          opencode: { cookie: ocCookie, workspaceId: ocWorkspace },
          ollama: { cookie: olCookie },
          claude: { accessToken: clToken },
          codex: { accessToken: cdToken },
          copilot: { token: cpToken, enterpriseHost: cpHost },
          openrouter: { apiKey: orKey },
        }),
      })
      if (!res.ok) {
        console.error('Failed to save quota settings', res.status, await res.text().catch(() => ''))
      }
    } catch (e) {
      console.error('Failed to save quota settings', e)
    }
  }, [])

  const startCopilotDeviceLogin = async () => {
    try {
      setCopilotDeviceFlow('waiting')
      setCopilotDeviceError('')
      const res = await fetch('/api/quotas/copilot/device-codes', { method: 'POST' })
      if (!res.ok) {
        const json = await res.json().catch(() => null)
        throw new Error(json?.error?.message || 'Failed to initiate device login')
      }
      const data = (await res.json())?.data
      setCopilotDeviceCode(data.device_code)
      setCopilotUserCode(data.user_code)
      setCopilotVerificationURI(data.verification_uri)
      const interval = data.interval || 5
      setCopilotInterval(interval)
      copilotIntervalRef.current = interval
      setCopilotDeviceFlow('polling')
    } catch (e: any) {
      setCopilotDeviceError(e.message || 'Failed to start device login')
      setCopilotDeviceFlow('error')
    }
  }

  const pollCopilotDeviceToken = useCallback(async () => {
    if (!copilotDeviceCode) return
    try {
      const res = await fetch(`/api/quotas/copilot/device-codes/${encodeURIComponent(copilotDeviceCode)}`)
      if (!res.ok) {
        const json = await res.json().catch(() => null)
        throw new Error(json?.error?.message || 'Poll failed')
      }
      const data = (await res.json())?.data
      if (data.access_token) {
        setCopilotToken(data.access_token)
        setCopilotDeviceFlow('done')
        saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, data.access_token, copilotEnterpriseHost, openrouterApiKey)
      } else if (data.error === 'authorization_pending') {
        pollTimerRef.current = setTimeout(pollCopilotDeviceToken, copilotIntervalRef.current * 1000)
      } else if (data.error === 'slow_down') {
        copilotIntervalRef.current += 5
        setCopilotInterval(copilotIntervalRef.current)
        pollTimerRef.current = setTimeout(pollCopilotDeviceToken, copilotIntervalRef.current * 1000)
      } else if (data.error) {
        setCopilotDeviceError(data.error_description || data.error)
        setCopilotDeviceFlow('error')
      }
    } catch (e: any) {
      setCopilotDeviceError(e.message || 'Poll failed')
      setCopilotDeviceFlow('error')
    }
  }, [
    copilotDeviceCode,
    antigravityKey,
    opencodeCookie,
    opencodeWorkspace,
    ollamaCookie,
    claudeAccessToken,
    codexAccessToken,
    copilotEnterpriseHost,
    openrouterApiKey,
    saveSettings,
  ])

  useEffect(() => {
    if (copilotDeviceFlow !== 'polling' || !copilotDeviceCode) return
    pollTimerRef.current = setTimeout(pollCopilotDeviceToken, 1000)
    return () => {
      if (pollTimerRef.current) {
        clearTimeout(pollTimerRef.current)
      }
    }
  }, [copilotDeviceFlow, copilotDeviceCode, pollCopilotDeviceToken])

  const applyTheme = (newTheme: 'light' | 'dark' | 'system') => {
    setTheme(newTheme)
    const root = window.document.documentElement
    if (newTheme === 'system') {
      const systemTheme = window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
      if (systemTheme === 'light') {
        root.classList.add('light')
      } else {
        root.classList.remove('light')
      }
    } else if (newTheme === 'light') {
      root.classList.add('light')
    } else {
      root.classList.remove('light')
    }
    localStorage.setItem('caw:theme', newTheme)
  }

  // savePref persists a work-pref change and shows transient feedback
  // ("Saved" / "Save failed") that auto-hides shortly after.
  const savePref = async (fn: () => Promise<boolean>) => {
    const ok = await fn()
    setSaveStatus(ok ? 'success' : 'error')
    if (saveTimerRef.current) clearTimeout(saveTimerRef.current)
    saveTimerRef.current = setTimeout(() => setSaveStatus('idle'), 1500)
  }

  // PetsSettingsPanel reports its own save outcomes through this callback so
  // all settings sections share the same floating save toast.
  const handlePetsSaveStatus = (status: 'idle' | 'success' | 'error') => {
    setSaveStatus(status)
    if (status !== 'idle') {
      if (saveTimerRef.current) clearTimeout(saveTimerRef.current)
      saveTimerRef.current = setTimeout(() => setSaveStatus('idle'), 1500)
    }
  }

  const toggleAgent = (agentId: string) => {
    let nextDisabled: string[]
    if (disabledAgents.includes(agentId)) {
      nextDisabled = disabledAgents.filter((id) => id !== agentId)
    } else {
      nextDisabled = [...disabledAgents, agentId]
    }
    setDisabledAgents(nextDisabled)
    void savePref(() => setPrefDisabledAgents(nextDisabled))
  }

  const handleOpenChange = (newOpen: boolean) => {
    onOpenChange(newOpen)
    if (!newOpen) {
      window.dispatchEvent(new CustomEvent('caw:settings-updated'))
    }
  }

  const selectSection = (section: Section) => {
    setActiveSection(section)
    if (isMobile) setMobileSectionSelected(true)
  }

  const backToSections = () => {
    setMobileSectionSelected(false)
  }

  function urlBase64ToUint8Array(base64String: string): Uint8Array {
    const padding = '='.repeat((4 - base64String.length % 4) % 4)
    const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
    const rawData = window.atob(base64)
    const buffer = new ArrayBuffer(rawData.length)
    const outputArray = new Uint8Array(buffer)
    for (let i = 0; i < rawData.length; ++i) {
      outputArray[i] = rawData.charCodeAt(i)
    }
    return outputArray
  }

  const sections: { id: Section; label: string; icon: ElementType; category?: string }[] = [
    { id: 'updates', label: 'Updates', icon: RefreshCw },
    { id: 'appearance', label: 'Appearance', icon: Palette, category: 'Preferences' },
    { id: 'terminal', label: 'Terminal', icon: Terminal, category: 'Preferences' },
    { id: 'hotkeys', label: 'Hotkeys', icon: Keyboard, category: 'Preferences' },
    { id: 'voice', label: 'Voice', icon: Mic, category: 'Preferences' },
    { id: 'workspaces', label: 'Workspaces', icon: Folder, category: 'General' },
    { id: 'notifications', label: 'Notifications', icon: Bell, category: 'General' },
    { id: 'agents', label: 'Agents', icon: Bot, category: 'Integrations' },
    { id: 'limits', label: 'Limits', icon: ChartSpline, category: 'Integrations' },
    { id: 'pets', label: 'Pets', icon: PawPrint, category: 'Integrations' },
  ]

  const renderSectionContent = () => (
    <>
      {activeSection === 'appearance' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Appearance</h3>
                <p className="text-xs text-muted-foreground">Customize the layout theme and feel of your workspace.</p>
              </div>

              <div className="grid grid-cols-3 gap-3 mt-2">
                <button
                  onClick={() => applyTheme('light')}
                  className={`flex flex-col items-center justify-center p-3 rounded-lg border text-center transition-all ${
                    theme === 'light'
                      ? 'border-primary bg-accent/40 ring-1 ring-ring'
                      : 'border-border hover:bg-accent/20'
                  }`}
                >
                  <Sun className="h-5 w-5 mb-2 text-amber-500" />
                  <span className="text-xs font-medium">Light</span>
                </button>
                <button
                  onClick={() => applyTheme('dark')}
                  className={`flex flex-col items-center justify-center p-3 rounded-lg border text-center transition-all ${
                    theme === 'dark'
                      ? 'border-primary bg-accent/40 ring-1 ring-ring'
                      : 'border-border hover:bg-accent/20'
                  }`}
                >
                  <Moon className="h-5 w-5 mb-2 text-indigo-400" />
                  <span className="text-xs font-medium">Dark</span>
                </button>
                <button
                  onClick={() => applyTheme('system')}
                  className={`flex flex-col items-center justify-center p-3 rounded-lg border text-center transition-all ${
                    theme === 'system'
                      ? 'border-primary bg-accent/40 ring-1 ring-ring'
                      : 'border-border hover:bg-accent/20'
                  }`}
                >
                  <Monitor className="h-5 w-5 mb-2 text-muted-foreground" />
                  <span className="text-xs font-medium">System</span>
                </button>
              </div>

              <div className="pt-4 mt-2">
                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium">Terminal Theme</label>
                  <p className="text-[10px] text-muted-foreground">Match the terminal background with the rest of the UI.</p>
                  <div className="flex gap-2 mt-1">
                    <button
                      onClick={() => {
                        setTerminalTheme('dark')
                        setAllTerminalThemes('dark')
                      }}
                      className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs font-medium transition-all ${
                        terminalTheme === 'dark'
                          ? 'border-primary bg-accent/40 ring-1 ring-ring'
                          : 'border-border hover:bg-accent/20'
                      }`}
                    >
                      <Moon className="h-3.5 w-3.5 text-indigo-400" />
                      Dark
                    </button>
                    <button
                      onClick={() => {
                        setTerminalTheme('light')
                        setAllTerminalThemes('light')
                      }}
                      className={`flex items-center gap-2 px-3 py-1.5 rounded-lg border text-xs font-medium transition-all ${
                        terminalTheme === 'light'
                          ? 'border-primary bg-accent/40 ring-1 ring-ring'
                          : 'border-border hover:bg-accent/20'
                      }`}
                    >
                      <Sun className="h-3.5 w-3.5 text-amber-500" />
                      Light
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeSection === 'agents' && agentStep === 1 && (
            <div className="flex flex-col gap-4 animate-in fade-in duration-200">
              <div>
                <h3 className="text-sm font-medium mb-1">Agents</h3>
                <p className="text-xs text-muted-foreground">Configure the command each agent runs for launching in the terminal.</p>
              </div>

              <div className="flex flex-col gap-2.5 mt-2 pb-4">
                {Object.values(agentTypes)
                  .filter((agent) => agent.id !== 'terminal')
                  .map((agent) => {
                    const Icon = agent.icon
                    return (
                      <SettingsItem
                        key={agent.id}
                        icon={Icon}
                        label={agent.label}
                        testId={`settings-agent-${agent.id}`}
                        onClick={() => {
                          setSelectedAgentId(agent.id)
                          const existing = getAgentCmdOverrides()[agent.id]
                          setAgentCmdDraft((existing || agent.cmd).join(' '))
                          setAgentStep(2)
                        }}
                      />
                    )
                  })}
              </div>
            </div>
          )}

          {activeSection === 'agents' && agentStep === 2 && (
            <div className="flex flex-col gap-4 animate-in fade-in slide-in-from-right-2 duration-200">
              <div className="flex flex-col gap-2 shrink-0">
                <div>
                  <h3 className="text-sm font-semibold select-none">
                    {agentTypes[selectedAgentId]?.label || 'Agent'} Configuration
                  </h3>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Adjust the command that will be run when launching this agent. Use shell-style quoting for arguments containing spaces.
                  </p>
                </div>
              </div>

              <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                <div className="flex flex-col gap-1.5">
                  <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Default Command</label>
                  <p className="text-[10px] text-muted-foreground font-mono leading-normal">
                    {agentTypes[selectedAgentId]?.cmd.join(' ') || ''}
                  </p>
                </div>
                <div className="border-t border-border pt-3 flex flex-col gap-1.5">
                  <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Custom Command (Optional)</label>
                  <input
                    type="text"
                    value={agentCmdDraft}
                    onChange={(e) => setAgentCmdDraft(e.target.value)}
                    placeholder={agentTypes[selectedAgentId]?.cmd.join(' ') || 'e.g. claude --dangerously-skip-permissions'}
                    className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                  />
                  <p className="text-[10px] text-muted-foreground">Tokens are split on whitespace. Leave empty to restore the sane default.</p>
                  <div className="flex items-center gap-2 mt-1.5">
                    <button
                      onClick={() => {
                        const trimmed = agentCmdDraft.trim()
                        if (!trimmed) {
                          void savePref(() => setAgentCmdOverride(selectedAgentId, null))
                        } else {
                          void savePref(() => setAgentCmdOverride(selectedAgentId, trimmed.split(/\s+/)))
                        }
                        setAgentStep(1)
                      }}
                      className="px-3 py-1.5 rounded-lg border border-border bg-background text-xs font-medium text-foreground hover:bg-accent/30 transition-all cursor-pointer"
                    >
                      Save
                    </button>
                    <button
                      onClick={() => {
                        setAgentCmdOverride(selectedAgentId, null)
                        setAgentCmdDraft(agentTypes[selectedAgentId]?.cmd.join(' ') || '')
                      }}
                      className="px-3 py-1.5 rounded-lg border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-all cursor-pointer"
                    >
                      Reset to Default
                    </button>
                  </div>
                </div>
              </div>

              <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                <div className="flex items-center justify-between">
                  <div className="flex flex-col gap-0.5">
                    <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Enable Agent</label>
                    <p className="text-[10px] text-muted-foreground">Show in terminal launcher and command palette.</p>
                  </div>
                  <button
                    onClick={() => toggleAgent(selectedAgentId)}
                    data-testid={`agent-toggle-${selectedAgentId}`}
                    className={`relative h-5 w-9 rounded-full transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring ${
                      !disabledAgents.includes(selectedAgentId) ? 'bg-primary' : 'bg-muted-foreground/30'
                    }`}
                  >
                    <span
                      className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-background transition-transform ${
                        !disabledAgents.includes(selectedAgentId) ? 'translate-x-4' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </div>
              </div>
            </div>
          )}

          {activeSection === 'terminal' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Terminal</h3>
                <p className="text-xs text-muted-foreground">Configure terminal appearance and default shell.</p>
              </div>

              <div className="flex flex-col gap-5 mt-2">
                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium">Font Size</label>
                  <div className="flex items-center gap-3">
                    <Slider
                      min={8}
                      max={32}
                      step={1}
                      value={[fontSize]}
                      onValueChange={(val) => {
                        const nextVal = val[0]
                        setFontSize(nextVal)
                        localStorage.setItem('caw:terminalFontSize', String(nextVal))
                        setAllTerminalFontSizes(nextVal)
                      }}
                      className="flex-1"
                    />
                    <span className="text-xs font-mono text-muted-foreground w-8 text-right tabular-nums">{fontSize}</span>
                  </div>
                </div>

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium">Default Shell</label>
                  <div className="flex gap-2">
                    <input
                      type="text"
                      value={shellPath}
                      onChange={(e) => {
                        setShellPath(e.target.value)
                        void savePref(() => setPrefDefaultShell(e.target.value))
                      }}
                      placeholder="Auto (system default)"
                      className="flex-1 px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors"
                    />
                    {shellPath && (
                      <button
                        onClick={() => {
                          setShellPath('')
                          void savePref(() => setPrefDefaultShell(''))
                        }}
                        className="px-2 py-1.5 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors"
                      >
                        Reset
                      </button>
                    )}
                  </div>
                  <p className="text-[10px] text-muted-foreground">Path to the default shell binary (e.g. /bin/zsh, pwsh.exe). Leave empty to use the system default.</p>
                </div>

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium">Background Terminals</label>
                  <div className="flex gap-2">
                    <input
                      type="number"
                      min={0}
                      max={16}
                      value={parkedLimit}
                      className="no-spinner flex-1 px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground outline-none focus:border-ring transition-colors"
                      onChange={(e) => {
                        const v = Math.max(0, Math.min(16, Math.floor(Number(e.target.value) || 0)))
                        setParkedLimit(v)
                        void savePref(() => setPrefParkedTerminals(v))
                      }}
                    />
                    <button
                      onClick={() => {
                        setParkedLimit(DEFAULT_PARKED_TERMINALS)
                        void savePref(() => setPrefParkedTerminals(DEFAULT_PARKED_TERMINALS))
                      }}
                      className="px-2 py-1.5 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors"
                    >
                      Reset
                    </button>
                  </div>
                  <p className="text-[10px] text-muted-foreground">Number of recently-used terminals kept mounted in the background so switching back to them is instant. Higher values use more memory. Set to 0 to disable.</p>                </div>

                <div className="pt-4 mt-2 border-t border-border">
                  <div className="flex flex-col gap-1 mb-3">
                    <label className="text-xs font-medium">Touch Scroll</label>
                    <p className="text-[10px] text-muted-foreground">Tune mobile touch scroll behavior for the terminal. Fractional line deltas accumulate and only fire when a whole line is reached, so small values are safe. Values apply on the next terminal interaction.</p>
                  </div>

                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-2">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Scroll Sensitivity</label>
                      <div className="flex items-center gap-3">
                        <Slider
                          min={0.005}
                          max={0.1}
                          step={0.001}
                          value={[scrollSensitivity]}
                          onValueChange={(val) => {
                            const nextVal = val[0]
                            setScrollSensitivity(nextVal)
                            localStorage.setItem('caw:terminalScrollSensitivity', String(nextVal))
                          }}
                          className="flex-1"
                        />
                        <span className="text-xs font-mono text-muted-foreground w-12 text-right tabular-nums">{scrollSensitivity.toFixed(3)}</span>
                      </div>
                      <p className="text-[10px] text-muted-foreground">Lines scrolled per pixel of drag. 0.02 = 1 line per 50 px. Higher is more sensitive.</p>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Momentum Friction</label>
                      <div className="flex items-center gap-3">
                        <Slider
                          min={0.5}
                          max={0.99}
                          step={0.01}
                          value={[scrollFriction]}
                          onValueChange={(val) => {
                            const nextVal = val[0]
                            setScrollFriction(nextVal)
                            localStorage.setItem('caw:terminalScrollFriction', String(nextVal))
                          }}
                          className="flex-1"
                        />
                        <span className="text-xs font-mono text-muted-foreground w-12 text-right tabular-nums">{scrollFriction.toFixed(2)}</span>
                      </div>
                      <p className="text-[10px] text-muted-foreground">Velocity retained per frame after releasing. Higher coasts longer.</p>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Velocity Threshold</label>
                      <div className="flex items-center gap-3">
                        <Slider
                          min={0.01}
                          max={0.2}
                          step={0.005}
                          value={[scrollVelocityThreshold]}
                          onValueChange={(val) => {
                            const nextVal = val[0]
                            setScrollVelocityThreshold(nextVal)
                            localStorage.setItem('caw:terminalScrollVelocityThreshold', String(nextVal))
                          }}
                          className="flex-1"
                        />
                        <span className="text-xs font-mono text-muted-foreground w-12 text-right tabular-nums">{scrollVelocityThreshold.toFixed(3)}</span>
                      </div>
                      <p className="text-[10px] text-muted-foreground">Minimum flick velocity (px/ms) to start momentum. Higher ignores tiny touches.</p>
                    </div>

                    <div className="flex flex-col gap-2">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Grace Period (ms)</label>
                      <div className="flex items-center gap-3">
                        <Slider
                          min={200}
                          max={3000}
                          step={100}
                          value={[scrollGrace]}
                          onValueChange={(val) => {
                            const nextVal = val[0]
                            setScrollGrace(nextVal)
                            localStorage.setItem('caw:terminalScrollGrace', String(nextVal))
                          }}
                          className="flex-1"
                        />
                        <span className="text-xs font-mono text-muted-foreground w-12 text-right tabular-nums">{scrollGrace}</span>
                      </div>
                      <p className="text-[10px] text-muted-foreground">How long after scrolling before auto-follow resumes. Higher keeps your scroll position longer.</p>
                    </div>

                    <button
                      onClick={() => {
                        setScrollSensitivity(0.02)
                        setScrollFriction(0.85)
                        setScrollVelocityThreshold(0.05)
                        setScrollGrace(1200)
                        localStorage.removeItem('caw:terminalScrollSensitivity')
                        localStorage.removeItem('caw:terminalScrollFriction')
                        localStorage.removeItem('caw:terminalScrollVelocityThreshold')
                        localStorage.removeItem('caw:terminalScrollGrace')
                      }}
                      className="self-start px-2.5 py-1.5 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors cursor-pointer"
                    >
                      Reset to Defaults
                    </button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {activeSection === 'hotkeys' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Hotkeys</h3>
                <p className="text-xs text-muted-foreground">Customize keyboard shortcuts for common actions.</p>
              </div>

              <div className="flex flex-col mt-1">
                {Object.entries(HOTKEY_LABELS).map(([action, label]) => {
                  const current = getHotkey(action)
                  const isRecording = hotkeyRecording === action
                  return (
                    <div key={action} className="flex items-center justify-between gap-2 py-1.5 border-b border-border last:border-0">
                      <span className="text-xs font-medium text-foreground">{label}</span>
                      <div className="flex items-center gap-1.5">
                        {isRecording ? (
                          <HotkeyRecorder
                            onSave={(combo) => {
                              const conflict = Object.entries(HOTKEY_LABELS).find(
                                ([a, _]) => a !== action && getHotkey(a) === combo
                              )
                              if (conflict) {
                                setHotkeyError(`Already used by "${HOTKEY_LABELS[conflict[0]]}"`)
                                return
                              }
                              setHotkeyError('')
                              void savePref(() => setPrefHotkey(action, combo))
                              setHotkeyRecording(null)
                            }}
                            onCancel={() => {
                              setHotkeyRecording(null)
                              setHotkeyError('')
                            }}
                          />
                        ) : (
                          <span
                            className="px-2.5 py-1 rounded-md border border-border text-xs font-mono text-muted-foreground cursor-pointer hover:border-primary hover:text-foreground transition-colors"
                            onClick={() => {
                              setHotkeyRecording(action)
                              setHotkeyError('')
                            }}
                          >
                            {current}
                          </span>
                        )}
                        {current !== DEFAULT_HOTKEYS[action] && !isRecording && (
                          <button
                            onClick={() => void savePref(() => resetPrefHotkey(action))}
                            className="text-[10px] text-muted-foreground hover:text-foreground underline transition-colors"
                          >
                            Reset
                          </button>
                        )}
                      </div>
                    </div>
                  )
                })}
                {hotkeyError && (
                  <p className="text-[10px] text-destructive mt-2">{hotkeyError}</p>
                )}
                <div className="flex justify-end mt-2">
                  <button
                    onClick={() => void savePref(() => resetAllPrefHotkeys())}
                    className="px-2.5 py-1.5 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors cursor-pointer"
                  >
                    Reset All to Defaults
                  </button>
                </div>
              </div>
            </div>
          )}

          {activeSection === 'pets' && <PetsSettingsPanel onSaveStatusChange={handlePetsSaveStatus} />}

          {activeSection === 'voice' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Voice</h3>
                <p className="text-xs text-muted-foreground">Configure voice input engine and language for speech recognition.</p>
              </div>

              <div className="flex flex-col gap-2 mt-1">
                <label className="text-xs font-medium">Voice Engine</label>
                <div className="flex rounded-lg border border-border overflow-hidden">
                  <button
                    onClick={() => {
                      setVoiceModeState('browser')
                      setVoiceMode('browser')
                    }}
                    className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs font-medium transition-colors ${
                      voiceMode === 'browser'
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-background text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    <Globe className="h-3.5 w-3.5" />
                    Browser
                  </button>
                  <button
                    onClick={() => {
                      if (!isKrokoSupported()) return
                      setVoiceModeState('local')
                      setVoiceMode('local')
                    }}
                    disabled={!isKrokoSupported()}
                    className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-2 text-xs font-medium transition-colors border-l border-border ${
                      voiceMode === 'local'
                        ? 'bg-primary text-primary-foreground'
                        : isKrokoSupported()
                          ? 'bg-background text-muted-foreground hover:text-foreground'
                          : 'bg-background text-muted-foreground/50 cursor-not-allowed'
                    }`}
                  >
                    <HardDrive className="h-3.5 w-3.5" />
                    Local (Private)
                  </button>
                </div>
                <p className="text-[10px] text-muted-foreground">
                  {voiceMode === 'browser'
                    ? 'Uses the browser\'s built-in speech recognition. Requires Chrome, Edge, or Safari.'
                    : 'Runs speech recognition locally in your browser. Models are downloaded and cached on your device.'}
                </p>
              </div>

              {voiceMode === 'browser' && (
                <>
                  {isVoiceSupported() ? (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-emerald-500/30 bg-emerald-500/5">
                      <div className="h-2 w-2 rounded-full bg-emerald-500 shrink-0" />
                      <span className="text-xs text-emerald-600 dark:text-emerald-400 font-medium">Voice Mode is supported in this browser</span>
                    </div>
                  ) : (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-red-500/30 bg-red-500/5">
                      <div className="h-2 w-2 rounded-full bg-red-500 shrink-0" />
                      <span className="text-xs text-red-600 dark:text-red-400 font-medium">Voice Mode is not supported in this browser. Use Chrome, Edge, or Safari for speech recognition.</span>
                    </div>
                  )}

                  <div className="flex flex-col gap-2">
                    <label className="text-xs font-medium">Speech Language</label>
                    <Select
                      value={voiceLanguage}
                      onValueChange={(value) => {
                        setVoiceLanguage(value)
                        if (value) {
                          localStorage.setItem('caw:voiceLanguage', value)
                        } else {
                          localStorage.removeItem('caw:voiceLanguage')
                        }
                      }}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="System Default" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="">System Default</SelectItem>
                        <SelectItem value="en-US">English (US)</SelectItem>
                        <SelectItem value="en-GB">English (UK)</SelectItem>
                        <SelectItem value="es-ES">Spanish</SelectItem>
                        <SelectItem value="fr-FR">French</SelectItem>
                        <SelectItem value="de-DE">German</SelectItem>
                        <SelectItem value="ja-JP">Japanese</SelectItem>
                        <SelectItem value="ko-KR">Korean</SelectItem>
                        <SelectItem value="zh-CN">Chinese (Simplified)</SelectItem>
                        <SelectItem value="pt-BR">Portuguese (Brazil)</SelectItem>
                        <SelectItem value="it-IT">Italian</SelectItem>
                        <SelectItem value="nl-NL">Dutch</SelectItem>
                        <SelectItem value="ru-RU">Russian</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-[10px] text-muted-foreground">Language used for speech recognition. "System Default" uses your browser's default language.</p>
                  </div>
                </>
              )}

              {voiceMode === 'local' && (
                <>
                  {!isKrokoSupported() ? (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-red-500/30 bg-red-500/5">
                      <div className="h-2 w-2 rounded-full bg-red-500 shrink-0" />
                      <span className="text-xs text-red-600 dark:text-red-400 font-medium">Local voice mode is not supported in this browser. Cache API is required.</span>
                    </div>
                  ) : krokoLoading ? (
                    <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-border">
                      <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
                      <span className="text-xs text-muted-foreground">Loading available models...</span>
                    </div>
                  ) : (
                    <>
                      <div className="flex flex-col gap-2">
                        <label className="text-xs font-medium">Model Language</label>
                        <Select
                          value={krokoLanguage}
                          onValueChange={(value) => {
                            setKrokoLanguageState(value)
                            setKrokoLanguage(value)
                          }}
                        >
                          <SelectTrigger className="w-full">
                            <SelectValue placeholder="Select language" />
                          </SelectTrigger>
                          <SelectContent>
                            {krokoLanguages.map((lang) => (
                              <SelectItem key={lang.iso} value={lang.iso}>{lang.name}</SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <p className="text-[10px] text-muted-foreground">Language for the local speech recognition model.</p>
                      </div>

                      <div className="flex flex-col gap-2">
                        <label className="text-xs font-medium">Available Models</label>
                        <div className="flex flex-col gap-2">
                          {krokoModels
                            .filter((m) => m.language_iso === krokoLanguage || (krokoModelCache[m.url] && hasInstalledModel))
                            .map((model) => {
                              const sizeMB = Math.round(model.file_size / 1000 / 1000)
                              const isDownloaded = krokoModelCache[model.url] || false
                              const isDownloading = krokoDownloading === model.url
                              const isDisabled = hasInstalledModel && !isDownloaded
                              const isOtherLanguage = model.language_iso !== krokoLanguage
                              return (
                                <div
                                  key={model.model_id}
                                  className={`flex items-center justify-between px-3 py-2 rounded-lg border border-border bg-background ${isDisabled ? 'opacity-40' : ''}`}
                                >
                                  <div className="flex flex-col gap-0.5">
                                    <span className="text-xs font-medium">{model.name}</span>
                                    <div className="flex items-center gap-1.5">
                                      <span className="text-[10px] text-muted-foreground">{sizeMB}MB</span>
                                      {isDownloaded && isOtherLanguage && (
                                        <span className="text-[10px] text-muted-foreground">· {krokoLanguages.find((l) => l.iso === model.language_iso)?.name || model.language_iso}</span>
                                      )}
                                    </div>
                                  </div>
                                  <div className="flex items-center gap-2">
                                    {isDownloaded ? (
                                      <>
                                        <div className="flex items-center gap-1">
                                          <div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
                                          <span className="text-[10px] text-emerald-600 dark:text-emerald-400">Downloaded</span>
                                        </div>
                                        <button
                                          onClick={async () => {
                                            await deleteModel()
                                            setKrokoModelCache((prev) => ({ ...prev, [model.url]: false }))
                                          }}
                                          className="p-1 rounded hover:bg-accent/50 transition-colors text-muted-foreground hover:text-foreground cursor-pointer"
                                          title="Remove model"
                                        >
                                          <Trash2 className="h-3 w-3" />
                                        </button>
                                      </>
                                    ) : (
                                      <button
                                        onClick={async () => {
                                          setKrokoDownloading(model.url)
                                          setKrokoDownloadProgress(null)
                                          try {
                                            await downloadModel(model.url, (downloaded, total) => {
                                              setKrokoDownloadProgress({ downloaded, total })
                                            })
                                            setKrokoModelCache((prev) => ({ ...prev, [model.url]: true }))
                                          } catch {
                                            setKrokoDownloadProgress(null)
                                          } finally {
                                            setKrokoDownloading(null)
                                            setKrokoDownloadProgress(null)
                                          }
                                        }}
                                        disabled={isDownloading || isDisabled}
                                        className="flex items-center gap-1 px-2 py-1 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors disabled:opacity-50 cursor-pointer"
                                      >
                                        {isDownloading ? (
                                          <Loader2 className="h-3 w-3 animate-spin" />
                                        ) : (
                                          <Download className="h-3 w-3" />
                                        )}
                                        {isDownloading && krokoDownloadProgress
                                          ? `${krokoDownloadProgress.downloaded.toFixed(1)} / ${krokoDownloadProgress.total.toFixed(1)} MB`
                                          : isDownloading
                                            ? 'Downloading...'
                                            : 'Download'}
                                      </button>
                                    )}
                                  </div>
                                </div>
                              )
                            })}
                          {krokoModels.filter((m) => m.language_iso === krokoLanguage || (krokoModelCache[m.url] && hasInstalledModel)).length === 0 && (
                            <p className="text-xs text-muted-foreground">No models available for this language.</p>
                          )}
                        </div>
                      </div>
                    </>
                  )}
                </>
              )}
            </div>
          )}

          {activeSection === 'workspaces' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Workspaces</h3>
                <p className="text-xs text-muted-foreground">Configure what opens by default when creating a new workspace.</p>
              </div>

              <div className="flex flex-col gap-2 mt-2">
                <label className="text-xs font-medium">On Workspace Created</label>
                <p className="text-[10px] text-muted-foreground">Choose what gets launched as the first tab when a new workspace is created.</p>
                <div className="flex flex-col gap-1.5 mt-1">
                  <button
                    onClick={() => {
                      setDefaultNewAgent('none')
                      void savePref(() => setPrefDefaultNewAgent('none'))
                    }}
                    className={`flex items-center gap-2.5 px-3 py-2 rounded-lg border text-xs font-medium transition-all ${
                      defaultNewAgent === 'none'
                        ? 'border-primary ring-1 ring-ring bg-primary/10'
                        : 'border-border hover:bg-accent/20'
                    }`}
                  >
                    <Minus className="h-4 w-4" />
                    <span className="flex-1 text-left">Do nothing</span>
                    {defaultNewAgent === 'none' && <Check className="h-3.5 w-3.5 text-primary" />}
                  </button>
                  <button
                    onClick={() => {
                      setDefaultNewAgent('terminal')
                      void savePref(() => setPrefDefaultNewAgent('terminal'))
                    }}
                    className={`flex items-center gap-2.5 px-3 py-2 rounded-lg border text-xs font-medium transition-all ${
                      defaultNewAgent === 'terminal'
                        ? 'border-primary ring-1 ring-ring bg-primary/10'
                        : 'border-border hover:bg-accent/20'
                    }`}
                  >
                    <Terminal className="h-4 w-4" />
                    <span className="flex-1 text-left">New Terminal</span>
                    {defaultNewAgent === 'terminal' && <Check className="h-3.5 w-3.5 text-primary" />}
                  </button>
                  {(() => {
                    return availableAgents
                      .filter((a) => !disabledAgents.includes(a.id))
                      .map((agentInfo) => {
                        const agent = agentTypes[agentInfo.id]
                        if (!agent) return null
                        const Icon = agent.icon
                        return (
                          <button
                            key={agentInfo.id}
                            onClick={() => {
                              setDefaultNewAgent(agentInfo.id)
                              void savePref(() => setPrefDefaultNewAgent(agentInfo.id))
                            }}
                            className={`flex items-center gap-2.5 px-3 py-2 rounded-lg border text-xs font-medium transition-all ${
                              defaultNewAgent === agentInfo.id
                                ? 'border-primary ring-1 ring-ring bg-primary/10'
                                : 'border-border hover:bg-accent/20'
                            }`}
                          >
                            <Icon className="h-4 w-4" />
                            <span className="flex-1 text-left">{agentInfo.label}</span>
                            {defaultNewAgent === agentInfo.id && <Check className="h-3.5 w-3.5 text-primary" />}
                          </button>
                        )
                      })
                  })()}
                </div>
              </div>
            </div>
          )}

          {activeSection === 'limits' && limitStep === 1 && (
            <div className="flex flex-col gap-4 animate-in fade-in duration-200">
              <div>
                <h3 className="text-sm font-semibold mb-1">Limits</h3>
                <p className="text-xs text-muted-foreground">Select a provider to configure credentials and view usage limits.</p>
              </div>

              <div className="grid grid-cols-1 gap-2.5 mt-2 pb-4">
                {[
                  { id: 'claude', label: 'Claude', icon: Claude.Color, show: claudeInstalled },
                  { id: 'codex', label: 'Codex', icon: Codex.Color, show: codexInstalled },
                  { id: 'copilot', label: 'GitHub Copilot', icon: GithubCopilot, show: true },
                  { id: 'antigravity', label: 'Antigravity', icon: Antigravity.Color, show: agyInstalled },
                  { id: 'opencode', label: 'OpenCode Go', icon: OpenCode, show: true },
                  { id: 'ollama', label: 'Ollama', icon: Ollama, show: true },
                  { id: 'openrouter', label: 'OpenRouter', icon: OpenRouter, show: true },
                ].filter(p => p.show).map((prov) => {
                  const Icon = prov.icon
                  return (
                    <SettingsItem
                      key={prov.id}
                      icon={Icon}
                      label={prov.label}
                      testId={`settings-provider-${prov.id}`}
                      onClick={() => {
                        setSelectedLimitProvider(prov.id as any)
                        setLimitStep(2)
                      }}
                    />
                  )
                })}
              </div>
            </div>
          )}

          {activeSection === 'limits' && limitStep === 2 && (
            <div className="flex flex-col gap-4 animate-in fade-in slide-in-from-right-2 duration-200">
              <div className="flex flex-col gap-2 shrink-0">
                <div>
                  <h3 className="text-sm font-semibold select-none">
                    {selectedLimitProvider === 'claude' && 'Claude Configuration'}
                    {selectedLimitProvider === 'codex' && 'Codex Configuration'}
                    {selectedLimitProvider === 'copilot' && 'GitHub Copilot Configuration'}
                    {selectedLimitProvider === 'antigravity' && 'Antigravity Configuration'}
                    {selectedLimitProvider === 'opencode' && 'OpenCode Go Configuration'}
                    {selectedLimitProvider === 'ollama' && 'Ollama Configuration'}
                    {selectedLimitProvider === 'openrouter' && 'OpenRouter Configuration'}
                  </h3>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Specify the details needed to authenticate limits tracking for this provider.
                  </p>
                </div>
              </div>

              {quotas?.[selectedLimitProvider]?.error && (
                <div className="px-4 py-2 rounded-lg border border-red-400/30 bg-red-500/10 text-xs text-red-400 shrink-0">
                  Error: {quotas[selectedLimitProvider].error}
                </div>
              )}

              <div className="flex flex-col gap-3 pb-4">
                {selectedLimitProvider === 'claude' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[10px] text-muted-foreground leading-normal">
                        Usage limits are auto-resolved from <code>~/.claude/.credentials.json</code>. Optionally provide an OAuth access token override.
                      </p>
                      <label className="text-[10px] font-semibold text-muted-foreground mt-1.5 uppercase tracking-wider">OAuth Access Token (Optional)</label>
                      <input
                        type="password"
                        value={claudeAccessToken}
                        onChange={(e) => {
                          const val = e.target.value
                          setClaudeAccessToken(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, val, codexAccessToken, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                        }}
                        placeholder="Enter Claude OAuth access token..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'codex' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[10px] text-muted-foreground leading-normal">
                        Usage limits are auto-resolved from <code>~/.codex/auth.json</code>. Optionally provide an OAuth access token override.
                      </p>
                      <label className="text-[10px] font-semibold text-muted-foreground mt-1.5 uppercase tracking-wider">OAuth Access Token (Optional)</label>
                      <input
                        type="password"
                        value={codexAccessToken}
                        onChange={(e) => {
                          const val = e.target.value
                          setCodexAccessToken(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, val, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                        }}
                        placeholder="Enter Codex OAuth access token..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'copilot' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <p className="text-[10px] text-muted-foreground leading-normal">
                      Login with GitHub to automatically fetch a token, or manually paste one below.
                    </p>

                    {copilotDeviceFlow === 'idle' || copilotDeviceFlow === 'error' ? (
                      <div className="flex flex-col gap-2">
                        {copilotDeviceFlow === 'error' && (
                          <div className="px-3 py-2 rounded-lg border border-red-400/30 bg-red-500/10 text-xs text-red-400">
                            {copilotDeviceError}
                          </div>
                        )}
                        <button
                          onClick={startCopilotDeviceLogin}
                          className="flex items-center justify-center gap-2 w-full px-3 py-2 rounded-lg border border-border bg-background text-xs font-medium text-foreground hover:bg-accent/30 transition-all cursor-pointer"
                        >
                          <LogIn className="h-3.5 w-3.5" />
                          Login with GitHub
                        </button>
                      </div>
                    ) : copilotDeviceFlow === 'waiting' ? (
                      <div className="flex items-center justify-center gap-2 py-3">
                        <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                        <span className="text-xs text-muted-foreground">Starting device login...</span>
                      </div>
                    ) : copilotDeviceFlow === 'polling' ? (
                      <div className="flex flex-col gap-3 p-3 rounded-lg border border-border bg-muted/20">
                        <div className="flex items-center gap-2">
                          <Loader2 className="h-4 w-4 animate-spin text-primary shrink-0" />
                          <span className="text-xs font-medium text-foreground">Waiting for authentication...</span>
                        </div>
                        <div className="flex flex-col gap-1.5">
                          <p className="text-[10px] text-muted-foreground">
                            1. Open <strong>GitHub Device Verification</strong> in your browser:
                          </p>
                          <a
                            href={copilotVerificationURI}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-1.5 text-xs font-mono text-primary underline underline-offset-2 hover:text-primary/80"
                          >
                            {copilotVerificationURI}
                            <ExternalLink className="h-3 w-3" />
                          </a>
                        </div>
                        <div className="flex flex-col gap-1.5">
                          <p className="text-[10px] text-muted-foreground">
                            2. Enter this code:
                          </p>
                          <div className="flex items-center gap-2">
                            <code className="px-3 py-1.5 rounded-md bg-background border border-border text-sm font-bold tracking-widest select-all">
                              {copilotUserCode}
                            </code>
                            <button
                              onClick={() => navigator.clipboard.writeText(copilotUserCode)}
                              className="px-2 py-1.5 rounded-md border border-border text-[10px] text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-all cursor-pointer"
                            >
                              Copy
                            </button>
                          </div>
                        </div>
                        <p className="text-[10px] text-muted-foreground italic">
                          Polling every {copilotInterval}s...
                        </p>
                      </div>
                    ) : copilotDeviceFlow === 'done' ? (
                      <div className="flex items-center gap-2 px-3 py-2 rounded-lg border border-emerald-400/30 bg-emerald-500/10 text-xs text-emerald-400">
                        <Check className="h-3.5 w-3.5 shrink-0" />
                        Token obtained and saved successfully!
                      </div>
                    ) : null}

                    <div className="border-t border-border pt-3">
                      <div className="flex flex-col gap-1.5">
                        <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">GitHub OAuth Token</label>
                        <input
                          type="password"
                          value={copilotToken}
                          onChange={(e) => {
                            const val = e.target.value
                            setCopilotToken(val)
                            setCopilotDeviceFlow('idle')
                            saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, val, copilotEnterpriseHost, openrouterApiKey)
                          }}
                          placeholder="gho_..."
                          className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5 mt-3">
                        <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Enterprise Host (Optional)</label>
                        <input
                          type="text"
                          value={copilotEnterpriseHost}
                          onChange={(e) => {
                            const val = e.target.value
                            setCopilotEnterpriseHost(val)
                            saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, val, openrouterApiKey)
                          }}
                          placeholder="e.g. octocorp.ghe.com"
                          className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                        />
                      </div>
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'antigravity' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[10px] text-muted-foreground leading-normal">
                        Usage limits are automatically resolved from your local <code>agy</code> CLI process. Optionally configure a Google OAuth Refresh Token as a manual fallback.
                      </p>
                      <label className="text-[10px] font-semibold text-muted-foreground mt-1.5 uppercase tracking-wider">Refresh Token / Access Token (Optional)</label>
                      <input
                        type="password"
                        value={antigravityKey}
                        onChange={(e) => {
                          const val = e.target.value
                          setAntigravityKey(val)
                          saveSettings(val, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                        }}
                        placeholder="Enter Antigravity refresh token or access token..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'opencode' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <p className="text-[10px] text-muted-foreground leading-normal">
                      Provide your OpenCode Go authentication details to fetch limits for your workspace.
                    </p>
                    <div className="grid grid-cols-2 gap-4">
                      <div className="flex flex-col gap-1.5">
                        <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Auth Cookie</label>
                        <input
                          type="password"
                          value={opencodeCookie}
                          onChange={(e) => {
                            const val = e.target.value
                            setOpencodeCookie(val)
                            saveSettings(antigravityKey, val, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                          }}
                          placeholder="auth cookie value..."
                          className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                        />
                      </div>
                      <div className="flex flex-col gap-1.5">
                        <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Workspace ID</label>
                        <input
                          type="text"
                          value={opencodeWorkspace}
                          onChange={(e) => {
                            const val = e.target.value
                            setOpencodeWorkspace(val)
                            saveSettings(antigravityKey, opencodeCookie, val, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                          }}
                          placeholder="e.g. wrk_01KVB2..."
                          className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                        />
                      </div>
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'ollama' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[10px] text-muted-foreground leading-normal">
                        Provide your Ollama session cookie to retrieve usage limits from your account.
                      </p>
                      <label className="text-[10px] font-semibold text-muted-foreground mt-1.5 uppercase tracking-wider">__Secure-session Cookie</label>
                      <input
                        type="password"
                        value={ollamaCookie}
                        onChange={(e) => {
                          const val = e.target.value
                          setOllamaCookie(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, val, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost, openrouterApiKey)
                        }}
                        placeholder="Enter __Secure-session cookie..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}

                {selectedLimitProvider === 'openrouter' && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex flex-col gap-1.5">
                      <p className="text-[10px] text-muted-foreground leading-normal">
                        Provide your OpenRouter API key to fetch credit usage and remaining limits for your key.
                      </p>
                      <label className="text-[10px] font-semibold text-muted-foreground mt-1.5 uppercase tracking-wider">API Key</label>
                      <input
                        type="password"
                        value={openrouterApiKey}
                        onChange={(e) => {
                          const val = e.target.value
                          setOpenrouterApiKey(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost, val)
                        }}
                        placeholder="sk-or-v1-..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}

          {activeSection === 'notifications' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Notifications</h3>
                <p className="text-xs text-muted-foreground">Configure how you receive notifications when agents need input or finish tasks.</p>
              </div>

              {/* In-app sounds */}
              <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                <div className="flex items-center justify-between">
                  <div className="flex flex-col gap-0.5">
                    <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Notification Sounds</label>
                    <p className="text-[10px] text-muted-foreground">Play a sound when an agent needs input or finishes.</p>
                  </div>
                  <button
                    onClick={() => {
                      const next = !soundEnabled
                      setSoundEnabled(next)
                      localStorage.setItem('caw:soundEnabled', next ? '1' : '0')
                    }}
                    className={`relative h-5 w-9 rounded-full transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring shrink-0 ${
                      soundEnabled ? 'bg-primary' : 'bg-muted-foreground/30'
                    }`}
                  >
                    <span
                      className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-background transition-transform ${
                        soundEnabled ? 'translate-x-4' : 'translate-x-0'
                      }`}
                    />
                  </button>
                </div>
              </div>

              <div className="pt-4 mt-2">
                <div className="flex flex-col gap-2 mb-3">
                  <label className="text-xs font-medium">Web Push Notifications</label>
                  <p className="text-[10px] text-muted-foreground">Get notified even when Caw is not in focus. Requires browser notification permission.</p>
                </div>

                {!pushSupported && !pushIOSPWA && (
                  <div className="px-3 py-2 rounded-lg border border-amber-400/30 bg-amber-500/10 text-xs text-amber-400">
                    Web Push is not supported in this browser.
                  </div>
                )}

                {pushIOSPWA && (
                  <div className="px-3 py-2 rounded-lg border border-amber-400/30 bg-amber-500/10 text-xs text-amber-400">
                    On iOS, Web Push requires Caw to be installed as a PWA. Tap the Share button and choose <strong>Add to Home Screen</strong>, then open Caw from the Home Screen icon to enable push notifications.
                  </div>
                )}

                {pushSupported && !isSecureContext && (
                  <div className="px-3 py-2 rounded-lg border border-amber-400/30 bg-amber-500/10 text-xs text-amber-400">
                    Push notifications require a secure context (HTTPS). They work on <code>localhost</code> but not on a remote IP over HTTP.
                  </div>
                )}

                {pushSupported && pushPermission === 'denied' && (
                  <div className="px-3 py-2 rounded-lg border border-red-400/30 bg-red-500/10 text-xs text-red-400">
                    Notification permission has been blocked. Please reset it in your browser settings.
                  </div>
                )}

                {pushError && (
                  <div className="px-3 py-2 rounded-lg border border-red-400/30 bg-red-500/10 text-xs text-red-400">
                    {pushError}
                  </div>
                )}

                {pushSupported && (
                  <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0">
                    <div className="flex items-center justify-between">
                      <div className="flex flex-col gap-0.5">
                        <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Enable Push Notifications</label>
                        <p className="text-[10px] text-muted-foreground">
                          {pushEnabled && pushSubscribed ? 'Active — this browser will receive push notifications.' : 'Enable to subscribe this browser.'}
                        </p>
                      </div>
                      <button
                        onClick={async () => {
                          setPushError('')
                          if (pushEnabled && pushSubscribed) {
                            // ===== DISABLE BRANCH =====
                            setPushBusy(true)
                            try {
                              const reg = await navigator.serviceWorker.ready
                              const sub = await reg.pushManager.getSubscription()
                              if (sub) {
                                await sub.unsubscribe()
                                await fetch('/api/push/subscribe', {
                                  method: 'DELETE',
                                  headers: { 'Content-Type': 'application/json' },
                                  body: JSON.stringify({ endpoint: sub.endpoint }),
                                })
                              }
                              setPushSubscribed(false)
                              setPushEnabled(false)
                              const deviceId = getDeviceId()
                              await fetch(`/api/push/devices/${encodeURIComponent(deviceId)}`, {
                                method: 'PUT',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ enabled: false }),
                              })
                            } catch (e: any) {
                              setPushError(e.message || 'Failed to disable push')
                            }
                            setPushBusy(false)
                          } else {
                            // ===== ENABLE / RE-SUBSCRIBE BRANCH =====
                            setPushBusy(true)
                            try {
                              if (Notification.permission === 'denied') {
                                setPushError('Notification permission is blocked in browser settings.')
                                setPushBusy(false)
                                return
                              }
                              if (Notification.permission !== 'granted') {
                                const perm = await Notification.requestPermission()
                                setPushPermission(perm)
                                if (perm !== 'granted') {
                                  setPushError('Notification permission was not granted.')
                                  setPushBusy(false)
                                  return
                                }
                              }
                              const res = await fetch('/api/push/vapid-public-key')
                              const vapidData = (await res.json())?.data
                              const vapidKey = vapidData?.publicKey
                              if (!vapidKey) {
                                setPushError('Failed to fetch VAPID public key.')
                                setPushBusy(false)
                                return
                              }
                              const reg = await navigator.serviceWorker.ready
                              const existing = await reg.pushManager.getSubscription()
                              if (existing) {
                                await existing.unsubscribe()
                                await fetch('/api/push/subscribe', {
                                  method: 'DELETE',
                                  headers: { 'Content-Type': 'application/json' },
                                  body: JSON.stringify({ endpoint: existing.endpoint }),
                                }).catch(() => {})
                              }
                              const sub = await reg.pushManager.subscribe({
                                userVisibleOnly: true,
                                applicationServerKey: urlBase64ToUint8Array(vapidKey) as BufferSource,
                              })
                              const subJSON = sub.toJSON()
                              if (!subJSON.endpoint || !subJSON.keys?.p256dh || !subJSON.keys?.auth) {
                                setPushError('Subscription created but missing keys. Browser may not support push fully.')
                                setPushBusy(false)
                                return
                              }
                              const deviceId = getDeviceId()
                              const deviceName = getDeviceName()
                              const subscribeRes = await fetch('/api/push/subscribe', {
                                method: 'POST',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({
                                  endpoint: subJSON.endpoint,
                                  keys: subJSON.keys,
                                  deviceId,
                                  deviceName,
                                }),
                              })
                              if (!subscribeRes.ok) {
                                setPushError('Failed to register subscription with server.')
                                setPushBusy(false)
                                return
                              }
                              setPushSubscribed(true)
                              setPushEnabled(true)
                              await fetch(`/api/push/devices/${encodeURIComponent(deviceId)}`, {
                                method: 'PUT',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify({ enabled: true }),
                              })
                            } catch (e: any) {
                              setPushError(e.message || 'Failed to enable push')
                            }
                            setPushBusy(false)
                          }
                        }}
                        disabled={pushBusy}
                        className={`relative h-5 w-9 rounded-full transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring shrink-0 ${
                          pushEnabled && pushSubscribed ? 'bg-primary' : 'bg-muted-foreground/30'
                        } ${pushBusy ? 'opacity-50 cursor-wait' : ''}`}
                      >
                        <span
                          className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-background transition-transform ${
                            pushEnabled && pushSubscribed ? 'translate-x-4' : 'translate-x-0'
                          }`}
                        />
                      </button>
                    </div>
                  </div>
                )}

                {/* Per-event toggles */}
                {pushEnabled && pushSubscribed && pushSupported && (
                  <>
                    <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0 mt-2">
                      <div className="flex items-center justify-between">
                        <div className="flex flex-col gap-0.5">
                          <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Notify on Needs Input</label>
                          <p className="text-[10px] text-muted-foreground">Send a push when an agent is waiting for your input.</p>
                        </div>
                        <button
                          onClick={() => {
                            const next = !pushNeedsInput
                            setPushNeedsInput(next)
                            const deviceId = getDeviceId()
                            fetch(`/api/push/devices/${encodeURIComponent(deviceId)}`, {
                              method: 'PUT',
                              headers: { 'Content-Type': 'application/json' },
                              body: JSON.stringify({ needsInput: next }),
                            })
                          }}
                          className={`relative h-5 w-9 rounded-full transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring shrink-0 ${
                            pushNeedsInput ? 'bg-primary' : 'bg-muted-foreground/30'
                          }`}
                        >
                          <span
                            className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-background transition-transform ${
                              pushNeedsInput ? 'translate-x-4' : 'translate-x-0'
                            }`}
                          />
                        </button>
                      </div>
                    </div>

                    <div className="flex flex-col gap-3 p-4 rounded-xl border border-border bg-secondary/10 shrink-0 mt-2">
                      <div className="flex items-center justify-between">
                        <div className="flex flex-col gap-0.5">
                          <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Notify on Finished</label>
                          <p className="text-[10px] text-muted-foreground">Send a push when an agent finishes its task.</p>
                        </div>
                        <button
                          onClick={() => {
                            const next = !pushFinished
                            setPushFinished(next)
                            const deviceId = getDeviceId()
                            fetch(`/api/push/devices/${encodeURIComponent(deviceId)}`, {
                              method: 'PUT',
                              headers: { 'Content-Type': 'application/json' },
                              body: JSON.stringify({ finished: next }),
                            })
                          }}
                          className={`relative h-5 w-9 rounded-full transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring shrink-0 ${
                            pushFinished ? 'bg-primary' : 'bg-muted-foreground/30'
                          }`}
                        >
                          <span
                            className={`absolute top-0.5 left-0.5 h-4 w-4 rounded-full bg-background transition-transform ${
                              pushFinished ? 'translate-x-4' : 'translate-x-0'
                            }`}
                          />
                        </button>
                      </div>
                    </div>
                  </>
                )}
              </div>
            </div>
          )}

          {activeSection === 'updates' && (
            <div className="flex flex-col items-center justify-center gap-6 flex-1 p-6">
              {changelogLoading ? (
                <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
              ) : (
                <>
                  <img src={cawLogoSvg} alt="Caw" className="h-24 w-24" />
                  <div className="flex flex-col items-center gap-1">
                    <h3 className="text-2xl font-semibold">{appVersion ? `Caw v${appVersion}` : 'Caw'}</h3>
                  </div>
                  {changelog && (
                    <div className="w-full max-w-md rounded-xl border border-border bg-secondary/10 p-4">
                      <h4 className="text-sm font-semibold mb-3">What's Changed</h4>
                      <ul className="space-y-1.5 max-h-[200px] overflow-y-auto pr-1">
                        {parseChangelog(changelog.body).map((item, i) => (
                          <li key={i} className="text-xs text-muted-foreground leading-relaxed">{item}</li>
                        ))}
                      </ul>
                      <a
                        href={changelog.htmlUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="inline-flex items-center gap-1 mt-3 text-xs text-primary hover:underline"
                      >
                        See full changelog
                        <ExternalLink className="h-3 w-3" />
                      </a>
                    </div>
                  )}
                  <div className="flex flex-col items-center gap-3">
                    <button
                      onClick={async () => {
                        if (updateState === 'updating' || updateState === 'checking' || updateState === 'updated') return
                        if (updateState === 'available') {
                          setUpdateState('updating')
                          setUpdateMessage('')
                          try {
                            const res = await fetch('/api/update/apply', { method: 'POST' })
                            const data = (await res.json())?.data
                            if (data?.updated) {
                              setUpdateState('updated')
                              setUpdateLatestVersion(data.latestVersion)
                              setUpdateMessage(data.message || 'Updated. Restart Caw to apply.')
                            } else {
                              setUpdateState('latest')
                              setUpdateMessage(data?.message || "You're on the latest version.")
                            }
                          } catch {
                            setUpdateState('error')
                            setUpdateMessage('Failed to update. Try again later.')
                          }
                        } else {
                          setUpdateState('checking')
                          setUpdateMessage('')
                          try {
                            const res = await fetch('/api/update/check', { method: 'POST' })
                            const data = (await res.json())?.data
                            if (data?.updateAvailable) {
                              setUpdateState('available')
                              setUpdateLatestVersion(data.latestVersion)
                              setUpdateMessage(`Update ${data.latestVersion} available`)
                            } else {
                              setUpdateState('latest')
                              setUpdateMessage("You're on the latest version.")
                            }
                          } catch {
                            setUpdateState('error')
                            setUpdateMessage('Failed to check for updates. Check your internet connection.')
                          }
                        }
                      }}
                      disabled={updateState === 'checking' || updateState === 'updating' || updateState === 'updated'}
                      className="inline-flex items-center gap-2 px-4 py-2 rounded-lg bg-primary text-primary-foreground text-sm font-medium hover:opacity-90 transition-opacity cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      {(updateState === 'checking' || updateState === 'updating') && (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      )}
                      {updateState === 'idle' && 'Check for updates'}
                      {updateState === 'checking' && 'Checking...'}
                      {updateState === 'available' && `Update to ${updateLatestVersion}`}
                      {updateState === 'updating' && 'Updating...'}
                      {updateState === 'updated' && 'Restart Caw'}
                      {updateState === 'latest' && 'Check for updates'}
                      {updateState === 'error' && 'Try again'}
                    </button>
                    {updateMessage && (
                      <p className={`text-xs ${updateState === 'error' ? 'text-red-400' : updateState === 'updated' ? 'text-emerald-400' : 'text-muted-foreground'}`}>
                        {updateMessage}
                      </p>
                    )}
                  </div>
                </>
              )}
            </div>
          )}
    </>
  )

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        hideClose={isMobile}
        data-testid="settings-dialog"
        style={
          isMobile
            ? undefined
            : { width: dialogSize.w, height: dialogSize.h, maxWidth: 'none', maxHeight: 'none' }
        }
        className={`p-0 flex flex-row overflow-hidden bg-background ${
          isMobile
            ? 'w-full h-full max-w-none max-h-none rounded-none fixed inset-0 translate-x-0 translate-y-0 left-0 top-0 border-0'
            : 'max-w-none max-h-none border border-border sm:rounded-lg'
        }`}
      >
        {isMobile ? (
          <>
            {/* Mobile: two-step layout */}
            {!mobileSectionSelected ? (
              <div className="w-full flex flex-col select-none [&_[data-radix-collection-wrapper]:has(>button[data-radix-dialog-close])]:hidden">
                <DialogTitle className="text-sm font-semibold text-foreground px-4 h-[44px] border-b border-border flex items-center gap-2">
                  <SettingsIcon className="h-4 w-4" />
                  <span>Settings</span>
                  <DialogClose className="ml-auto p-1 -mr-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer">
                    <X className="h-4 w-4" />
                  </DialogClose>
                </DialogTitle>
                <div className="flex-1 flex flex-col p-3 gap-1.5">
                  {(() => {
                    let lastCategory: string | undefined
                    let hasRenderedCategory = false
                    const out: any[] = []
                    for (const s of sections) {
                      const Icon = s.icon
                      if (s.category !== undefined && s.category !== lastCategory) {
                        out.push(
                          <div
                            key={`cat-${s.category}`}
                            className={`text-[10px] font-semibold text-muted-foreground uppercase tracking-wider px-3 pt-3 pb-1${hasRenderedCategory ? ' mt-2 border-t border-border' : ''}`}
                          >
                            {s.category}
                          </div>
                        )
                        hasRenderedCategory = true
                        lastCategory = s.category
                      }
                      out.push(
                        <button
                          key={s.id}
                          onClick={() => selectSection(s.id)}
                          data-testid={`settings-section-${s.id}`}
                          className="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium text-foreground hover:bg-accent/40 transition-all"
                        >
                          <Icon className="h-4 w-4" />
                          {s.label}
                        </button>
                      )
                    }
                    return out
                  })()}
                </div>
              </div>
            ) : (
              <div className="relative w-full flex flex-col">
                <div className="flex items-center gap-2 px-4 h-[44px] shrink-0 border-b border-border">
                  <button
                    onClick={() => {
                      if ((activeSection === 'agents' && agentStep === 2) || (activeSection === 'limits' && limitStep === 2)) {
                        setAgentStep(1)
                        setLimitStep(1)
                      } else {
                        backToSections()
                      }
                    }}
                    className="p-1 -ml-1 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                    title="Back"
                  >
                    <ArrowLeft className="h-4 w-4 shrink-0" />
                  </button>
                  <DialogTitle className="text-sm font-semibold text-foreground">
                    {activeSection === 'agents' && agentStep === 2
                      ? (agentTypes[selectedAgentId]?.label || 'Agent') + ' Configuration'
                      : activeSection === 'limits' && limitStep === 2
                        ? ({ claude: 'Claude', codex: 'Codex', copilot: 'GitHub Copilot', antigravity: 'Antigravity', opencode: 'OpenCode Go', ollama: 'Ollama', openrouter: 'OpenRouter' }[selectedLimitProvider] || 'Provider') + ' Configuration'
                        : sections.find((s) => s.id === activeSection)?.label || 'Settings'}
                  </DialogTitle>
                  <DialogClose className="ml-auto p-1 -mr-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-accent transition-colors cursor-pointer">
                    <X className="h-4 w-4" />
                  </DialogClose>
                </div>
                <div className="flex-1 flex flex-col p-5 overflow-y-auto thin-scroll" style={{ scrollbarWidth: 'thin' }}>
                  {renderSectionContent()}
                </div>
                <SaveToast status={saveStatus} />
              </div>
            )}
          </>
        ) : (
          <>
            <div className="w-[180px] border-r border-border bg-muted/20 flex flex-col p-3 gap-1.5 shrink-0 select-none overflow-y-auto thin-scroll">
              <DialogTitle className="text-[10px] font-semibold text-muted-foreground px-2.5 py-1 uppercase tracking-wider">
                Settings
              </DialogTitle>
              {(() => {
                let lastCategory: string | undefined
                let hasRenderedCategory = true
                const out: any[] = []
                for (const s of sections) {
                  const Icon = s.icon
                  if (s.category !== undefined && s.category !== lastCategory) {
                    out.push(
                      <div
                        key={`cat-${s.category}`}
                        className={`text-[10px] font-semibold text-muted-foreground uppercase tracking-wider px-2.5 pt-3 pb-1${hasRenderedCategory ? ' mt-2 border-t border-border' : ''}`}
                      >
                        {s.category}
                      </div>
                    )
                    hasRenderedCategory = true
                    lastCategory = s.category
                  }
                  out.push(
                    <button
                      key={s.id}
                      onClick={() => setActiveSection(s.id)}
                      data-testid={`settings-section-${s.id}`}
                      className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all ${
                        activeSection === s.id
                          ? 'bg-accent text-accent-foreground shadow-sm'
                          : 'text-muted-foreground hover:text-foreground hover:bg-accent/30'
                      }`}
                    >
                      <Icon className="h-3.5 w-3.5" />
                      {s.label}
                    </button>
                  )
                }
                return out
              })()}
            </div>

            <div className="relative flex-1 flex flex-col">
              {((activeSection === 'agents' && agentStep === 2) || (activeSection === 'limits' && limitStep === 2)) && (
                <div className="flex items-center gap-2 px-4 h-[40px] shrink-0 border-b border-border">
                  <button
                    onClick={() => {
                      if (activeSection === 'agents') setAgentStep(1)
                      else setLimitStep(1)
                    }}
                    className="p-1 -ml-1 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                    title="Back"
                  >
                    <ArrowLeft className="h-4 w-4 shrink-0" />
                  </button>
                  <span className="text-sm font-semibold text-foreground select-none">
                    {activeSection === 'agents' && agentStep === 2
                      ? (agentTypes[selectedAgentId]?.label || 'Agent') + ' Configuration'
                      : ({ claude: 'Claude', codex: 'Codex', copilot: 'GitHub Copilot', antigravity: 'Antigravity', opencode: 'OpenCode Go', ollama: 'Ollama', openrouter: 'OpenRouter' }[selectedLimitProvider] || 'Provider') + ' Configuration'}
                  </span>
                </div>
              )}
              <div className="flex-1 flex flex-col p-5 overflow-y-auto thin-scroll" style={{ scrollbarWidth: 'thin' }}>
                {renderSectionContent()}
              </div>
              <SaveToast status={saveStatus} />
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
