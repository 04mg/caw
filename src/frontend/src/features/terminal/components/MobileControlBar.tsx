import { useEffect, useState } from 'react'
import { Button } from '@/components/button'
import {
  toggleStickyCtrl,
  toggleStickyAlt,
  subscribeStickyModifiers,
  stickyModifiers,
  sendTerminalInput
} from '@/features/terminal/services/terminalRegistry'
import { ChevronUp, ChevronDown, ChevronLeft, ChevronRight, CornerDownLeft } from 'lucide-react'

interface MobileControlBarProps {
  terminalId: string
}

export function MobileControlBar({ terminalId }: MobileControlBarProps) {
  const [sticky, setSticky] = useState({ ctrl: false, alt: false })

  useEffect(() => {
    return subscribeStickyModifiers(() => {
      setSticky({ ctrl: stickyModifiers.ctrl, alt: stickyModifiers.alt })
    })
  }, [terminalId])

  const triggerKey = (sequence: string) => {
    sendTerminalInput(terminalId, sequence)
  }

  return (
    <div className="flex gap-1.5 overflow-x-auto py-1.5 px-3 bg-secondary/35 border-t border-border/60 scrollbar-none shrink-0 select-none items-center w-full justify-between">
      {/* Sticky Modifiers with toggled color states */}
      <div className="flex gap-1 shrink-0">
        <Button
          variant={sticky.ctrl ? 'default' : 'ghost'}
          className={`h-7 px-2.5 text-xs font-bold transition-all border ${
            sticky.ctrl 
              ? 'bg-amber-500 hover:bg-amber-600 text-black border-amber-400 font-extrabold shadow-sm animate-none' 
              : 'border-border/30 text-muted-foreground'
          }`}
          onClick={toggleStickyCtrl}
        >
          CTRL
        </Button>
        <Button
          variant={sticky.alt ? 'default' : 'ghost'}
          className={`h-7 px-2.5 text-xs font-bold transition-all border ${
            sticky.alt 
              ? 'bg-violet-600 hover:bg-violet-700 text-white border-violet-500 font-extrabold shadow-sm animate-none' 
              : 'border-border/30 text-muted-foreground'
          }`}
          onClick={toggleStickyAlt}
        >
          ALT
        </Button>
      </div>

      {/* Basic control keys */}
      <div className="flex gap-1 shrink-0">
        <Button variant="outline" className="h-7 px-2 text-xs text-foreground font-semibold border-border/30 bg-background/50" onClick={() => triggerKey('\x1b')}>
          ESC
        </Button>
        <Button variant="outline" className="h-7 px-2 text-xs text-foreground font-semibold border-border/30 bg-background/50" onClick={() => triggerKey('\t')}>
          TAB
        </Button>
      </div>

      {/* Common TUI shortcuts */}
      <div className="flex gap-1 shrink-0">
        <Button variant="outline" className="h-7 px-2 text-[10px] text-red-400 hover:text-red-300 font-bold border-border/30 bg-background/50" onClick={() => triggerKey('\u0003')}>
          ^C
        </Button>
        <Button variant="outline" className="h-7 px-2 text-[10px] text-yellow-400 hover:text-yellow-300 font-bold border-border/30 bg-background/50" onClick={() => triggerKey('\u0004')}>
          ^D
        </Button>
        <Button variant="outline" className="h-7 px-2 text-[10px] text-blue-400 hover:text-blue-300 font-bold border-border/30 bg-background/50" onClick={() => triggerKey('\u001a')}>
          ^Z
        </Button>
      </div>

      {/* Arrow Keys */}
      <div className="flex gap-0.5 border border-border/30 rounded-lg p-0.5 bg-background/30 shrink-0">
        <Button variant="ghost" size="icon" className="h-6 w-6 text-foreground/80 hover:text-foreground animate-none" onClick={() => triggerKey('\x1b[D')}>
          <ChevronLeft className="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" className="h-6 w-6 text-foreground/80 hover:text-foreground animate-none" onClick={() => triggerKey('\x1b[A')}>
          <ChevronUp className="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" className="h-6 w-6 text-foreground/80 hover:text-foreground animate-none" onClick={() => triggerKey('\x1b[B')}>
          <ChevronDown className="h-3.5 w-3.5" />
        </Button>
        <Button variant="ghost" size="icon" className="h-6 w-6 text-foreground/80 hover:text-foreground animate-none" onClick={() => triggerKey('\x1b[C')}>
          <ChevronRight className="h-3.5 w-3.5" />
        </Button>
      </div>

      <Button variant="outline" className="h-7 w-8 flex items-center justify-center border-border/30 bg-background/50 shrink-0" onClick={() => triggerKey('\r')}>
        <CornerDownLeft className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}
