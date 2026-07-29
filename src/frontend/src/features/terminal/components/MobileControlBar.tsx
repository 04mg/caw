import { useEffect, useState } from 'react'
import { Button } from '@/components/button'
import {
  toggleStickyCtrl,
  toggleStickyAlt,
  toggleStickyShift,
  subscribeStickyModifiers,
  stickyModifiers,
  resetStickyModifiers,
  sendTerminalInput,
  pasteFromClipboard
} from '@/features/terminal/services/terminalRegistry'
import { ChevronUp, ChevronDown, ChevronLeft, ChevronRight, CornerDownLeft, Mic, ClipboardPaste, Loader2 } from 'lucide-react'
import { useVoiceMode, isVoiceSupported } from '@/features/voice-mode/hooks/useVoiceMode'
import { cn } from '@/features/shared/utils/utils'

interface MobileControlBarProps {
  terminalId: string
}

export function MobileControlBar({ terminalId }: MobileControlBarProps) {
  const [sticky, setSticky] = useState({ ctrl: false, alt: false, shift: false })
  const [pasting, setPasting] = useState(false)
  const voice = useVoiceMode()

  useEffect(() => {
    return subscribeStickyModifiers(() => {
      setSticky({ ctrl: stickyModifiers.ctrl, alt: stickyModifiers.alt, shift: stickyModifiers.shift })
    })
  }, [terminalId])

  const triggerKey = (sequence: string) => {
    sendTerminalInput(terminalId, sequence)
  }

  const handleToggleVoice = () => {
    if (!isVoiceSupported()) return
    if (voice.phase === 'idle') {
      voice.start()
    } else if (voice.phase === 'listening') {
      voice.stop()
    }
  }

  const handlePaste = async () => {
    if (pasting) return
    setPasting(true)
    try {
      await pasteFromClipboard(terminalId)
    } finally {
      setPasting(false)
    }
  }

  return (
    <div className="flex gap-1.5 overflow-x-auto py-1.5 px-3 bg-secondary/35 border-t border-border/60 scrollbar-none shrink-0 select-none items-center w-full justify-between" onMouseDown={(e) => e.preventDefault()}>
      {/* Sticky Modifiers with toggled color states */}
      <div className="flex gap-1 shrink-0">
        {isVoiceSupported() && (
          <Button
            variant={voice.phase === 'listening' ? 'default' : 'ghost'}
            className={cn(
              "h-7 px-2 transition-all border animate-none",
              voice.phase === 'listening'
                ? 'bg-primary text-primary-foreground border-primary font-extrabold shadow-sm'
                : 'border-border/30 text-muted-foreground'
            )}
            onClick={handleToggleVoice}
            disabled={voice.phase === 'loading'}
          >
            {voice.phase === 'loading' ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Mic className={cn("h-3.5 w-3.5", voice.phase === 'listening' && "lava-lamp-mic")} />
            )}
          </Button>
        )}
        <Button
          variant="ghost"
          className="h-7 px-2 transition-all border border-border/30 text-muted-foreground hover:text-foreground"
          onClick={handlePaste}
          disabled={pasting}
        >
          {pasting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <ClipboardPaste className="h-3.5 w-3.5" />
          )}
        </Button>
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
        <Button
          variant={sticky.shift ? 'default' : 'ghost'}
          className={`h-7 px-2.5 text-xs font-bold transition-all border ${
            sticky.shift
              ? 'bg-emerald-600 hover:bg-emerald-700 text-white border-emerald-500 font-extrabold shadow-sm animate-none'
              : 'border-border/30 text-muted-foreground'
          }`}
          onClick={toggleStickyShift}
        >
          SHIFT
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
        <Button variant="outline" className="h-7 px-2 text-xs text-foreground font-semibold border-border/30 bg-background/50" onClick={() => triggerKey('\x1b[H')}>
          HOME
        </Button>
        <Button variant="outline" className="h-7 px-2 text-xs text-foreground font-semibold border-border/30 bg-background/50" onClick={() => triggerKey('\x1b[F')}>
          END
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

      <Button
        variant="outline"
        className={`h-7 w-8 flex items-center justify-center border-border/30 bg-background/50 shrink-0 ${
          sticky.ctrl ? 'bg-amber-500/20 border-amber-400/50' : ''
        }`}
        onClick={() => {
          if (stickyModifiers.ctrl) {
            triggerKey('\n')
            resetStickyModifiers()
          } else {
            triggerKey('\r')
          }
        }}
      >
        <CornerDownLeft className="h-3.5 w-3.5" />
      </Button>
    </div>
  )
}
