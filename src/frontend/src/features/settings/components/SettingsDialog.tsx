import { useState, useEffect, useCallback, useRef, type ElementType } from 'react'
import { Dialog, DialogContent, DialogTitle } from '@/components/dialog'
import { Slider } from '@/components/slider'

import { Palette, Bot, Terminal, Check, Moon, Sun, Monitor, ChartSpline, ArrowLeft, LogIn, ExternalLink, Loader2, FolderKanban, Settings as SettingsIcon } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, Claude, Codex, GithubCopilot, OpenRouter } from '@lobehub/icons'
import { agentTypes, getAgentCmdOverrides, setAgentCmdOverride } from '@/features/agents/services/agentTypes'
import { setAllTerminalFontSizes, setAllTerminalThemes } from '@/features/terminal/services/terminalRegistry'
import { SettingsItem } from './SettingsItem'


interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialSection?: string
}

type Section = 'appearance' | 'agents' | 'terminal' | 'workspaces' | 'limits'

export function SettingsDialog({ open, onOpenChange, initialSection }: SettingsDialogProps) {
  const [activeSection, setActiveSection] = useState<Section>('appearance')
  const [isMobile, setIsMobile] = useState(window.innerWidth < 768)
  const [mobileSectionSelected, setMobileSectionSelected] = useState(false)
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')
  const [terminalTheme, setTerminalTheme] = useState<'dark' | 'light'>('dark')
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])
  const [fontSize, setFontSize] = useState(13)
  const [shellPath, setShellPath] = useState('')
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
  const [defaultNewAgent, setDefaultNewAgent] = useState('terminal')
  const [availableAgents, setAvailableAgents] = useState<any[]>([])

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

  // Reset mobile section selection when dialog closes
  useEffect(() => {
    if (!open) setMobileSectionSelected(false)
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

      const savedDisabled = localStorage.getItem('caw:disabledAgents')
      if (savedDisabled) {
        try {
          setDisabledAgents(JSON.parse(savedDisabled))
        } catch {
          setDisabledAgents([])
        }
      } else {
        setDisabledAgents([])
      }

      const savedFontSize = parseInt(localStorage.getItem('caw:terminalFontSize') || '13', 10)
      setFontSize(isNaN(savedFontSize) ? 13 : Math.max(8, Math.min(32, savedFontSize)))

      setShellPath(localStorage.getItem('caw:defaultShell') || '')

      setDefaultNewAgent(localStorage.getItem('caw:defaultNewAgent') || 'terminal')

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

      // Reset device login state
      setCopilotDeviceFlow('idle')
      setCopilotDeviceCode('')
      setCopilotUserCode('')
      setCopilotVerificationURI('')
      setCopilotInterval(5)
      copilotIntervalRef.current = 5
      setCopilotDeviceError('')
    }
  }, [open, loadQuotaSettings, loadQuotas])

  const saveSettings = async (
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
      await fetch('/api/quotas/settings', {
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
    } catch (e) {
      console.error('Failed to save quota settings', e)
    }
  }

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
  }, [copilotDeviceCode])

  useEffect(() => {
    if (copilotDeviceFlow !== 'polling' || !copilotDeviceCode) return
    pollTimerRef.current = setTimeout(pollCopilotDeviceToken, 1000)
    return () => {
      if (pollTimerRef.current) {
        clearTimeout(pollTimerRef.current)
      }
    }
  }, [copilotDeviceFlow, pollCopilotDeviceToken])

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

  const toggleAgent = (agentId: string) => {
    let nextDisabled: string[]
    if (disabledAgents.includes(agentId)) {
      nextDisabled = disabledAgents.filter((id) => id !== agentId)
    } else {
      nextDisabled = [...disabledAgents, agentId]
    }
    setDisabledAgents(nextDisabled)
    localStorage.setItem('caw:disabledAgents', JSON.stringify(nextDisabled))
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

  const sections: { id: Section; label: string; icon: ElementType }[] = [
    { id: 'appearance', label: 'Appearance', icon: Palette },
    { id: 'terminal', label: 'Terminal', icon: Terminal },
    { id: 'workspaces', label: 'Workspaces', icon: FolderKanban },
    { id: 'agents', label: 'Agents', icon: Bot },
    { id: 'limits', label: 'Limits', icon: ChartSpline },
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

              <div className="border-t border-border pt-4 mt-2">
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
                  <button
                    onClick={() => setAgentStep(1)}
                    className="p-1 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring"
                    title="Back to Agents"
                  >
                    <ArrowLeft className="h-4 w-4 shrink-0" />
                  </button>
                </div>
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
                          setAgentCmdOverride(selectedAgentId, null)
                        } else {
                          setAgentCmdOverride(selectedAgentId, trimmed.split(/\s+/))
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
                        localStorage.setItem('caw:defaultShell', e.target.value)
                      }}
                      placeholder="Auto (system default)"
                      className="flex-1 px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors"
                    />
                    {shellPath && (
                      <button
                        onClick={() => {
                          setShellPath('')
                          localStorage.removeItem('caw:defaultShell')
                        }}
                        className="px-2 py-1.5 rounded-md border border-border text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors"
                      >
                        Reset
                      </button>
                    )}
                  </div>
                  <p className="text-[10px] text-muted-foreground">Path to the default shell binary (e.g. /bin/zsh, pwsh.exe). Leave empty to use the system default.</p>
                </div>
              </div>
            </div>
          )}

          {activeSection === 'workspaces' && (
            <div className="flex flex-col gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Workspaces</h3>
                <p className="text-xs text-muted-foreground">Configure what opens by default when creating a new workspace.</p>
              </div>

              <div className="flex flex-col gap-2 mt-2">
                <label className="text-xs font-medium">Default Agent / Terminal</label>
                <p className="text-[10px] text-muted-foreground">Choose what gets launched as the first tab when a new workspace is created.</p>
                <div className="flex flex-col gap-1.5 mt-1">
                  <button
                    onClick={() => {
                      setDefaultNewAgent('terminal')
                      localStorage.setItem('caw:defaultNewAgent', 'terminal')
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
                    const savedDisabled = localStorage.getItem('caw:disabledAgents')
                    let disabledList: string[] = []
                    if (savedDisabled) {
                      try { disabledList = JSON.parse(savedDisabled) } catch {}
                    }
                    return availableAgents
                      .filter((a) => !disabledList.includes(a.id))
                      .map((agentInfo) => {
                        const agent = agentTypes[agentInfo.id]
                        if (!agent) return null
                        const Icon = agent.icon
                        return (
                          <button
                            key={agentInfo.id}
                            onClick={() => {
                              setDefaultNewAgent(agentInfo.id)
                              localStorage.setItem('caw:defaultNewAgent', agentInfo.id)
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
                  <button
                    onClick={() => setLimitStep(1)}
                    className="p-1 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors cursor-pointer outline-none focus:ring-1 focus:ring-ring"
                    title="Back to Providers"
                  >
                    <ArrowLeft className="h-4 w-4 shrink-0" />
                  </button>
                </div>
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
    </>
  )

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className={`p-0 flex flex-row overflow-hidden bg-background border border-border sm:rounded-lg ${
        isMobile
          ? 'w-full h-full max-w-none max-h-none rounded-none fixed inset-0 translate-x-0 translate-y-0 left-0 top-0'
          : 'w-[600px] h-[400px] max-w-none max-h-none'
      }`}>
        {isMobile ? (
          <>
            {/* Mobile: two-step layout */}
            {!mobileSectionSelected ? (
              <div className="w-full flex flex-col select-none">
                <DialogTitle className="text-sm font-semibold text-foreground px-4 py-3 border-b border-border flex items-center gap-2">
                  <SettingsIcon className="h-4 w-4" />
                  Settings
                </DialogTitle>
                <div className="flex-1 flex flex-col p-3 gap-1.5">
                  {sections.map((s) => {
                    const Icon = s.icon
                    return (
                      <button
                        key={s.id}
                        onClick={() => selectSection(s.id)}
                        className="flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium text-foreground hover:bg-accent/40 transition-all"
                      >
                        <Icon className="h-4 w-4" />
                        {s.label}
                      </button>
                    )
                  })}
                </div>
              </div>
            ) : (
              <div className="w-full flex flex-col">
                <div className="flex items-center gap-2 px-3 py-3 border-b border-border shrink-0">
                  <button
                    onClick={backToSections}
                    className="p-1 rounded-md hover:bg-accent text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
                    title="Back to Settings"
                  >
                    <ArrowLeft className="h-4 w-4 shrink-0" />
                  </button>
                  <DialogTitle className="text-sm font-semibold text-foreground">
                    {sections.find((s) => s.id === activeSection)?.label || 'Settings'}
                  </DialogTitle>
                </div>
                <div className="flex-1 flex flex-col p-5 overflow-y-auto thin-scroll" style={{ scrollbarWidth: 'thin' }}>
                  {renderSectionContent()}
                </div>
              </div>
            )}
          </>
        ) : (
          <>
            <div className="w-[180px] border-r border-border bg-muted/20 flex flex-col p-3 gap-1.5 shrink-0 select-none">
              <DialogTitle className="text-xs font-semibold text-muted-foreground px-2 py-1 mb-2">
                Settings
              </DialogTitle>
              {sections.map((s) => {
                const Icon = s.icon
                return (
                  <button
                    key={s.id}
                    onClick={() => setActiveSection(s.id)}
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
              })}
            </div>

            <div className="flex-1 flex flex-col p-5 overflow-y-auto thin-scroll" style={{ scrollbarWidth: 'thin' }}>
              {renderSectionContent()}
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
