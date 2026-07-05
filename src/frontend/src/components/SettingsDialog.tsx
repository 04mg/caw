import { useState, useEffect, useCallback } from 'react'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Slider } from '@/components/ui/slider'
import { Monitor, Bot, Terminal, Check, Moon, Sun, Key } from 'lucide-react'
import { Antigravity, OpenCode } from '@lobehub/icons'
import { agentTypes } from '@/lib/agentTypes'
import { setAllTerminalFontSizes } from '@/lib/terminalRegistry'
import { cn } from '@/lib/utils'

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type Section = 'appearance' | 'agents' | 'terminal' | 'limits'

export function SettingsDialog({ open, onOpenChange }: SettingsDialogProps) {
  const [activeSection, setActiveSection] = useState<Section>('appearance')
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])
  const [fontSize, setFontSize] = useState(13)
  const [shellPath, setShellPath] = useState('')
  const [antigravityKey, setAntigravityKey] = useState('')
  const [opencodeCookie, setOpencodeCookie] = useState('')
  const [opencodeWorkspace, setOpencodeWorkspace] = useState('')
  const [selectedLimitProvider, setSelectedLimitProvider] = useState<'antigravity' | 'opencode'>('antigravity')

  const loadQuotaSettings = useCallback(async () => {
    try {
      const res = await fetch('/api/quotas/settings')
      if (res.ok) {
        const data = await res.json()
        setAntigravityKey(data.antigravity?.apiKey || '')
        setOpencodeCookie(data.opencode?.cookie || '')
        setOpencodeWorkspace(data.opencode?.workspaceId || '')
      }
    } catch (e) {
      console.error('Failed to load quota settings', e)
    }
  }, [])

  // Load settings on open
  useEffect(() => {
    if (open) {
      const savedTheme = (localStorage.getItem('caw:theme') as 'light' | 'dark' | 'system') || 'system'
      setTheme(savedTheme)

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

      loadQuotaSettings()
    }
  }, [open, loadQuotaSettings])

  const saveSettings = async (agKey: string, ocCookie: string, ocWorkspace: string) => {
    try {
      await fetch('/api/quotas/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          antigravity: { apiKey: agKey },
          opencode: { cookie: ocCookie, workspaceId: ocWorkspace },
        }),
      })
    } catch (e) {
      console.error('Failed to save quota settings', e)
    }
  }

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

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="w-[600px] h-[400px] max-w-none max-h-none p-0 flex flex-row overflow-hidden bg-background border border-border sm:rounded-lg">
        {/* Sidebar */}
        <div className="w-[180px] border-r border-border bg-muted/20 flex flex-col p-3 gap-1.5 shrink-0 select-none">
          <DialogTitle className="text-xs font-semibold text-muted-foreground px-2 py-1 mb-2">
            Settings
          </DialogTitle>
          <button
            onClick={() => setActiveSection('appearance')}
            className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all ${
              activeSection === 'appearance'
                ? 'bg-accent text-accent-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/30'
            }`}
          >
            <Monitor className="h-3.5 w-3.5" />
            Appearance
          </button>
          <button
            onClick={() => setActiveSection('agents')}
            className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all ${
              activeSection === 'agents'
                ? 'bg-accent text-accent-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/30'
            }`}
          >
            <Bot className="h-3.5 w-3.5" />
            Agents
          </button>
          <button
            onClick={() => setActiveSection('terminal')}
            className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all ${
              activeSection === 'terminal'
                ? 'bg-accent text-accent-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/30'
            }`}
          >
            <Terminal className="h-3.5 w-3.5" />
            Terminal
          </button>
          <button
            onClick={() => setActiveSection('limits')}
            className={`flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-xs font-medium transition-all ${
              activeSection === 'limits'
                ? 'bg-accent text-accent-foreground shadow-sm'
                : 'text-muted-foreground hover:text-foreground hover:bg-accent/30'
            }`}
          >
            <Key className="h-3.5 w-3.5" />
            Limits
          </button>
        </div>

        {/* Content Pane */}
        <div className="flex-1 flex flex-col p-5 overflow-y-auto">
          {activeSection === 'appearance' && (
            <div className="flex flex-col h-full gap-4">
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
            </div>
          )}

          {activeSection === 'agents' && (
            <div className="flex flex-col h-full gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Agents</h3>
                <p className="text-xs text-muted-foreground">Enable or disable agents visible in the terminal launcher.</p>
              </div>

              <div className="flex flex-col gap-2 mt-2">
                {Object.values(agentTypes)
                  .filter((agent) => agent.id !== 'terminal') // Terminal is always enabled
                  .map((agent) => {
                    const isEnabled = !disabledAgents.includes(agent.id)
                    const Icon = agent.icon
                    return (
                      <div
                        key={agent.id}
                        onClick={() => toggleAgent(agent.id)}
                        className="flex items-center justify-between p-2.5 rounded-lg border border-border hover:bg-accent/30 cursor-pointer select-none transition-colors"
                      >
                        <div className="flex items-center gap-3">
                          <div className="h-7 w-7 rounded bg-muted flex items-center justify-center text-muted-foreground shrink-0">
                            {Icon && <Icon className="h-4 w-4" />}
                          </div>
                          <div>
                            <p className="text-xs font-medium">{agent.label}</p>
                            <p className="text-[10px] text-muted-foreground font-mono">{agent.cmd.join(' ')}</p>
                          </div>
                        </div>
                        <div
                          className={`h-4 w-4 rounded border flex items-center justify-center transition-all ${
                            isEnabled ? 'bg-primary border-primary text-primary-foreground' : 'border-muted-foreground'
                          }`}
                        >
                          {isEnabled && <Check className="h-3 w-3 stroke-[3]" />}
                        </div>
                      </div>
                    )
                  })}
              </div>
            </div>
          )}

          {activeSection === 'terminal' && (
            <div className="flex flex-col h-full gap-4">
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

          {activeSection === 'limits' && (
            <div className="flex flex-col h-full gap-4">
              <div>
                <h3 className="text-sm font-medium mb-1">Limits</h3>
                <p className="text-xs text-muted-foreground">Configure credentials for usage limits.</p>
              </div>

              {/* Step 1: Choose provider */}
              <div className="flex flex-col gap-2 shrink-0">
                <label className="text-xs font-semibold text-muted-foreground select-none">1. Select Provider</label>
                <div className="grid grid-cols-2 gap-3 mt-0.5">
                  {[
                    { id: 'antigravity', label: 'Antigravity', icon: Antigravity },
                    { id: 'opencode', label: 'OpenCode', icon: OpenCode }
                  ].map((prov) => {
                    const isSelected = selectedLimitProvider === prov.id
                    const Icon = prov.icon
                    return (
                      <div
                        key={prov.id}
                        onClick={() => setSelectedLimitProvider(prov.id as any)}
                        className={cn(
                          "flex items-center gap-3 p-2.5 rounded-lg border cursor-pointer select-none transition-all",
                          isSelected
                            ? "border-primary bg-accent/40 ring-1 ring-ring"
                            : "border-border hover:bg-accent/30 text-muted-foreground hover:text-foreground"
                        )}
                      >
                        <div className="h-8 w-8 rounded bg-muted flex items-center justify-center shrink-0">
                          <Icon className="h-5 w-5" />
                        </div>
                        <div className="text-left">
                          <p className="text-xs font-semibold text-foreground">{prov.label}</p>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>

              {/* Step 2: Configure credentials */}
              <div className="flex-1 flex flex-col min-h-0">
                <label className="text-xs font-semibold text-muted-foreground select-none mb-1.5">2. Configure Credentials</label>
                <div className="flex-1 overflow-y-auto pr-1">
                  {selectedLimitProvider === 'antigravity' && (
                    <div className="flex flex-col gap-2 p-3 rounded-lg border border-border bg-secondary/10 shrink-0">
                      <h4 className="text-xs font-semibold flex items-center gap-1.5">
                        <Antigravity className="h-4 w-4 shrink-0" />
                        Antigravity Configuration
                      </h4>
                      
                      <div className="flex flex-col gap-1.5 mt-1">
                        <p className="text-[10px] text-muted-foreground leading-normal">
                          Usage limits are automatically resolved from your local <code>agy</code> CLI process. Optionally configure a Google OAuth Refresh Token as a manual fallback.
                        </p>
                        <label className="text-[10px] font-medium text-muted-foreground mt-1">Refresh Token / Access Token (Optional)</label>
                        <input
                          type="password"
                          value={antigravityKey}
                          onChange={(e) => {
                            const val = e.target.value
                            setAntigravityKey(val)
                            saveSettings(val, opencodeCookie, opencodeWorkspace)
                          }}
                          placeholder="Enter Antigravity refresh token or access token..."
                          className="w-full px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors animate-none"
                        />
                      </div>
                    </div>
                  )}

                  {selectedLimitProvider === 'opencode' && (
                    <div className="flex flex-col gap-2 p-3 rounded-lg border border-border bg-secondary/10 shrink-0">
                      <h4 className="text-xs font-semibold flex items-center gap-1.5">
                        <OpenCode className="h-4 w-4 shrink-0" />
                        OpenCode Configuration
                      </h4>
                      <div className="grid grid-cols-2 gap-3 mt-1">
                        <div className="flex flex-col gap-1">
                          <label className="text-[10px] font-medium text-muted-foreground">Auth Cookie</label>
                          <input
                            type="password"
                            value={opencodeCookie}
                            onChange={(e) => {
                              const val = e.target.value
                              setOpencodeCookie(val)
                              saveSettings(antigravityKey, val, opencodeWorkspace)
                            }}
                            placeholder="auth cookie value..."
                            className="w-full px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors animate-none"
                          />
                        </div>
                        <div className="flex flex-col gap-1">
                          <label className="text-[10px] font-medium text-muted-foreground">Workspace ID</label>
                          <input
                            type="text"
                            value={opencodeWorkspace}
                            onChange={(e) => {
                              const val = e.target.value
                              setOpencodeWorkspace(val)
                              saveSettings(antigravityKey, opencodeCookie, val)
                            }}
                            placeholder="e.g. wrk_01KVB2..."
                            className="w-full px-2.5 py-1.5 rounded-md border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/50 outline-none focus:border-ring transition-colors animate-none"
                          />
                        </div>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
