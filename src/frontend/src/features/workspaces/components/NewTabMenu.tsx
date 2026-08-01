import { useState, useEffect, type ReactNode } from 'react'
import { Terminal, Plus, Workflow } from 'lucide-react'
import { agentTypes } from '@/features/agents/services/agentTypes'
import { getEffectiveAgentCmd, getDisabledAgents } from '@/features/prefs/stores/prefsStore'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from '@/components/dropdown-menu'
import { Checkbox } from '@/components/checkbox'

interface NewTabMenuProps {
  onAdd: (cmd?: string[], agentId?: string, label?: string, env?: [string, string][]) => void
  enableWorktrees?: boolean
  onToggleWorktrees?: () => void
  children?: ReactNode
  align?: 'start' | 'center' | 'end'
  className?: string
  triggerClassName?: string
  triggerTitle?: string
}

export function NewTabMenu({
  onAdd,
  enableWorktrees,
  onToggleWorktrees,
  children,
  align = 'start',
  className,
  triggerClassName,
  triggerTitle = 'New tab/agent',
}: NewTabMenuProps) {
  const [availableAgents, setAvailableAgents] = useState<any[]>([])

  useEffect(() => {
    fetch('/api/agents')
      .then((res) => res.ok ? res.json() : Promise.resolve({ data: [] }))
      .then((json) => {
        const data = json?.data
        if (Array.isArray(data)) {
          setAvailableAgents(data)
        }
      })
      .catch(() => {})
  }, [])

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        {children ?? (
          <button
            className={`flex items-center justify-center px-2 text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-colors h-full shrink-0 ${triggerClassName ?? ''}`}
            title={triggerTitle}
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align={align} className={className}>
        <DropdownMenuItem onClick={() => onAdd()}>
          <Terminal className="h-4 w-4" />
          <span>New Terminal</span>
        </DropdownMenuItem>
        {(() => {
          const disabledList = getDisabledAgents()

          const visibleAgents = availableAgents.filter((a) => !disabledList.includes(a.id))
          if (visibleAgents.length === 0) return null

          return (
            <>
              <DropdownMenuSeparator />
              {visibleAgents.map((agentInfo) => {
                const agent = agentTypes[agentInfo.id]
                const IconComponent = agent?.icon || Terminal
                return (
                  <DropdownMenuItem
                    key={agentInfo.id}
                    onClick={() => onAdd(getEffectiveAgentCmd(agentInfo.id, agentInfo.cmd), agentInfo.id, agentInfo.label, agent?.env)}
                  >
                    <IconComponent size={16} className="h-4 w-4" />
                    <span>{agentInfo.label}</span>
                  </DropdownMenuItem>
                )
              })}
            </>
          )
        })()}
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={(e) => {
            e.preventDefault()
            onToggleWorktrees?.()
          }}
          className="flex items-center justify-between w-full cursor-pointer gap-4"
        >
          <div className="flex items-center gap-2">
            <Workflow className={enableWorktrees ? 'lava-lamp-icon h-4 w-4' : 'h-4 w-4 opacity-50'} />
            <span className={enableWorktrees ? 'lava-lamp-text' : ''}>Worktrees</span>
          </div>
          <Checkbox
            checked={enableWorktrees}
            onChange={() => onToggleWorktrees?.()}
            onClick={(e) => e.stopPropagation()}
            className={enableWorktrees ? 'lava-lamp-checkbox' : ''}
          />
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}