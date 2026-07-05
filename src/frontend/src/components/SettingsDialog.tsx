import { useState, useEffect } from 'react'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { Monitor, Bot, Check, Moon, Sun } from 'lucide-react'
import { agentTypes } from '@/lib/agentTypes'

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

type Section = 'appearance' | 'agents'

export function SettingsDialog({ open, onOpenChange }: SettingsDialogProps) {
  const [activeSection, setActiveSection] = useState<Section>('appearance')
  const [theme, setTheme] = useState<'light' | 'dark' | 'system'>('system')
  const [disabledAgents, setDisabledAgents] = useState<string[]>([])

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
    }
  }, [open])

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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
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
        </div>
      </DialogContent>
    </Dialog>
  )
}
