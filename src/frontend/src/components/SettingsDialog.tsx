import { useState, useEffect, useCallback } from 'react'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Slider } from '@/components/ui/slider'
import { Monitor, Bot, Terminal, Check, Moon, Sun, Key, ArrowLeft } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, Claude, Codex, GithubCopilot } from '@lobehub/icons'
import { agentTypes } from '@/lib/agentTypes'
import { setAllTerminalFontSizes } from '@/lib/terminalRegistry'

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  initialSection?: string
}

type Section = 'appearance' | 'agents' | 'terminal' | 'limits'

export function SettingsDialog({ open, onOpenChange, initialSection }: SettingsDialogProps) {
  const [activeSection, setActiveSection] = useState<Section>('appearance')
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])
  const [fontSize, setFontSize] = useState(13)
  const [shellPath, setShellPath] = useState('')
  const [antigravityKey, setAntigravityKey] = useState('')
  const [opencodeCookie, setOpencodeCookie] = useState('')
  const [opencodeWorkspace, setOpencodeWorkspace] = useState('')
  const [ollamaCookie, setOllamaCookie] = useState('')
  const [claudeAccessToken, setClaudeAccessToken] = useState('')
  const [codexAccessToken, setCodexAccessToken] = useState('')
  const [copilotToken, setCopilotToken] = useState('')
  const [copilotEnterpriseHost, setCopilotEnterpriseHost] = useState('')
  const [selectedLimitProvider, setSelectedLimitProvider] = useState<'claude' | 'codex' | 'copilot' | 'antigravity' | 'opencode' | 'ollama'>('claude')
  const [limitStep, setLimitStep] = useState<1 | 2>(1)
  const [agyInstalled, setAgyInstalled] = useState(true)
  const [claudeInstalled, setClaudeInstalled] = useState(true)
  const [codexInstalled, setCodexInstalled] = useState(true)
  const [quotas, setQuotas] = useState<Record<string, { error?: string }> | null>(null)

  const loadQuotaSettings = useCallback(async () => {
    try {
      const res = await fetch('/api/quotas/settings')
      if (res.ok) {
        const data = await res.json()
        setAntigravityKey(data.antigravity?.apiKey || '')
        setOpencodeCookie(data.opencode?.cookie || '')
        setOpencodeWorkspace(data.opencode?.workspaceId || '')
        setOllamaCookie(data.ollama?.cookie || '')
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
        const data = await res.json()
        setQuotas(data)
      }
    } catch (e) {
      console.error('Failed to load quotas', e)
    }
  }, [])

  // Reset limitStep when activeSection changes or dialog closes
  useEffect(() => {
    setLimitStep(1)
  }, [activeSection, open])

  // Load settings on open
  useEffect(() => {
    if (open) {
      if (initialSection) {
        setActiveSection(initialSection as Section)
      }

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
      loadQuotas()
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
  ) => {
    try {
      await fetch('/api/quotas/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          antigravity: { apiKey: agKey },
          opencode: { cookie: ocCookie, workspaceId: ocWorkspace },
          ollama: { cookie: olCookie },
          claude: { accessToken: clToken },
          codex: { accessToken: cdToken },
          copilot: { token: cpToken, enterpriseHost: cpHost },
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

              <div className="flex flex-col gap-2 mt-2 pb-4">
                {Object.values(agentTypes)
                  .filter((agent) => agent.id !== 'terminal')
                  .map((agent) => {
                    const isEnabled = !disabledAgents.includes(agent.id)
                    const Icon = agent.icon
                    return (
                      <div
                        key={agent.id}
                        onClick={() => toggleAgent(agent.id)}
                        className="flex items-center justify-between p-3 rounded-lg border border-border hover:bg-accent/30 cursor-pointer select-none transition-colors"
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

          {activeSection === 'limits' && limitStep === 1 && (
            <div className="flex flex-col h-full gap-4 animate-in fade-in duration-200">
              <div>
                <h3 className="text-sm font-semibold mb-1">Limits</h3>
                <p className="text-xs text-muted-foreground">Select a provider to configure credentials and view usage limits.</p>
              </div>

              <div className="grid grid-cols-1 gap-2.5 mt-2 pb-4">
                {[
                  { id: 'claude', label: 'Claude', description: 'Anthropic Claude Code OAuth usage', icon: Claude.Color, show: claudeInstalled },
                  { id: 'codex', label: 'Codex', description: 'OpenAI Codex CLI OAuth usage', icon: Codex.Color, show: codexInstalled },
                  { id: 'copilot', label: 'GitHub Copilot', description: 'Copilot internal usage API (GitHub OAuth token)', icon: GithubCopilot, show: true },
                  { id: 'antigravity', label: 'Antigravity', description: 'Google Cloud & local agy integration', icon: Antigravity.Color, show: agyInstalled },
                  { id: 'opencode', label: 'OpenCode Go', description: 'OpenCode Go workspace authorization', icon: OpenCode, show: true },
                  { id: 'ollama', label: 'Ollama', description: 'Ollama session cookie limits', icon: Ollama, show: true },
                ].filter(p => p.show).map((prov) => {
                  const Icon = prov.icon
                  return (
                    <button
                      key={prov.id}
                      onClick={() => {
                        setSelectedLimitProvider(prov.id as any)
                        setLimitStep(2)
                      }}
                      className="flex items-center justify-between p-3 rounded-xl border border-border bg-card hover:bg-accent/40 cursor-pointer select-none transition-all group duration-200 text-left w-full outline-none focus:ring-1 focus:ring-ring"
                    >
                      <div className="flex items-center gap-3">
                        <div className="h-9 w-9 rounded-lg bg-muted flex items-center justify-center shrink-0 group-hover:scale-105 transition-transform">
                          <Icon className="h-5 w-5 text-foreground" />
                        </div>
                        <div>
                          <p className="text-xs font-semibold text-foreground">{prov.label}</p>
                          <p className="text-[10px] text-muted-foreground mt-0.5">{prov.description}</p>
                        </div>
                      </div>
                      <div className="text-muted-foreground group-hover:text-primary transition-colors text-xs font-semibold flex items-center gap-1.5 pr-1">
                        Configure &rarr;
                      </div>
                    </button>
                  )
                })}
              </div>
            </div>
          )}

          {activeSection === 'limits' && limitStep === 2 && (
            <div className="flex flex-col h-full gap-4 animate-in fade-in slide-in-from-right-2 duration-200">
              <div className="flex flex-col gap-2">
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
                  </h3>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    Specify the details needed to authenticate limits tracking for this provider.
                  </p>
                </div>
              </div>

              {quotas?.[selectedLimitProvider]?.error && (
                <div className="px-4 py-2 rounded-lg border border-red-400/30 bg-red-500/10 text-xs text-red-400">
                  Error: {quotas[selectedLimitProvider].error}
                </div>
              )}

              <div className="flex-1 overflow-y-auto pr-1">
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
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, val, codexAccessToken, copilotToken, copilotEnterpriseHost)
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
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, val, copilotToken, copilotEnterpriseHost)
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
                      Provide a GitHub OAuth token (scope <code>read:user</code>) obtained via the device flow.
                    </p>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">GitHub OAuth Token</label>
                      <input
                        type="password"
                        value={copilotToken}
                        onChange={(e) => {
                          const val = e.target.value
                          setCopilotToken(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, val, copilotEnterpriseHost)
                        }}
                        placeholder="gho_..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                    <div className="flex flex-col gap-1.5">
                      <label className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider">Enterprise Host (Optional)</label>
                      <input
                        type="text"
                        value={copilotEnterpriseHost}
                        onChange={(e) => {
                          const val = e.target.value
                          setCopilotEnterpriseHost(val)
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, val)
                        }}
                        placeholder="e.g. octocorp.ghe.com"
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
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
                          saveSettings(val, opencodeCookie, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost)
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
                            saveSettings(antigravityKey, val, opencodeWorkspace, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost)
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
                            saveSettings(antigravityKey, opencodeCookie, val, ollamaCookie, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost)
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
                          saveSettings(antigravityKey, opencodeCookie, opencodeWorkspace, val, claudeAccessToken, codexAccessToken, copilotToken, copilotEnterpriseHost)
                        }}
                        placeholder="Enter __Secure-session cookie..."
                        className="w-full px-3 py-2 rounded-lg border border-input bg-background text-xs font-mono text-foreground placeholder:text-muted-foreground/40 outline-none focus:border-primary focus:ring-1 focus:ring-ring transition-all"
                      />
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
