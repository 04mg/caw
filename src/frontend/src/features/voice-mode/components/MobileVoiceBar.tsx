import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { Check, X, CircleAlert } from 'lucide-react'
import { Button } from '@/components/button'
import { cn } from '@/features/shared/utils/utils'
import { getVoiceMode } from '@/features/voice-mode/services/krokoAsr'

type VoicePhase = 'idle' | 'loading' | 'listening' | 'review'

interface MobileVoiceBarProps {
	phase: VoicePhase
	transcript: string
	error: string | null
	onInsert: () => void
	onDiscard: () => void
}

// Inline voice row for the mobile control bar. While voice mode is active it
// takes the place of the modifier/arrow keys inside the bar (the mic button
// stays put on its left), so the live transcript sits in the same in-flow strip
// that already rides above the on-screen keyboard. It keeps the bar's exact
// height, so entering/leaving voice mode never resizes the terminal.
export function MobileVoiceBar({ phase, transcript, error, onInsert, onDiscard }: MobileVoiceBarProps) {
	const scrollRef = useRef<HTMLDivElement>(null)
	const [overflowing, setOverflowing] = useState(false)
	const text = transcript.trim()
	const hasText = text.length > 0
	const showError = !!error && phase === 'idle'

	// Keep the newest words visible: the transcript area is two lines tall and
	// scrolls; pin it to the bottom as text streams in.
	useLayoutEffect(() => {
		const el = scrollRef.current
		if (!el) return
		el.scrollTop = el.scrollHeight
		setOverflowing(el.scrollHeight > el.clientHeight + 1)
	}, [transcript, error, phase])

	let content: ReactNode
	if (showError) {
		content = <span className="text-red-400">{error}</span>
	} else if (phase === 'loading') {
		content = (
			<span className="text-muted-foreground">
				{getVoiceMode() === 'local' ? 'Loading voice engine…' : 'Starting microphone…'}
			</span>
		)
	} else if (hasText) {
		content = <span className="text-foreground">{text}</span>
	} else {
		content = <span className="text-muted-foreground">Listening… speak now</span>
	}

	return (
		<>
			<div
				data-testid="mobile-voice-status"
				data-phase={showError ? 'error' : phase}
				role="status"
				aria-live="polite"
				// 30px = the arrow-key cluster (h-6 keys + p-0.5 + border), the
				// tallest item of the regular bar, so swapping rows doesn't
				// resize (and refit) the terminal.
				className={cn(
					'flex flex-1 min-w-0 h-[30px] items-center gap-2 rounded-md border px-2.5 bg-background/50',
					showError ? 'border-red-500/40' : phase === 'listening' ? 'border-primary/40' : 'border-border/30',
				)}
			>
				{showError ? (
					<CircleAlert className="h-3.5 w-3.5 shrink-0 text-red-400" />
				) : phase === 'listening' ? (
					<span className="voice-pulse-dot" aria-hidden />
				) : null}
				<div
					ref={scrollRef}
					className={cn(
						'voice-transcript flex-1 min-w-0 max-h-7 overflow-y-auto text-[11px] leading-[14px] break-words select-none',
						overflowing && 'voice-transcript-fade',
					)}
				>
					{content}
				</div>
			</div>

			<div className="flex gap-1 shrink-0">
				<Button
					variant="outline"
					data-testid="mobile-voice-discard"
					aria-label={showError ? 'Dismiss' : 'Discard'}
					title={showError ? 'Dismiss' : 'Discard'}
					className="h-7 w-8 px-0 text-muted-foreground border-border/30 bg-background/50 animate-none"
					onClick={onDiscard}
				>
					<X className="h-3.5 w-3.5" />
				</Button>
				{!showError && (
					<Button
						variant="outline"
						data-testid="mobile-voice-send"
						aria-label="Insert into terminal"
						title="Insert into terminal"
						disabled={!hasText}
						className={cn(
							'h-7 w-8 px-0 border-border/30 animate-none',
							hasText ? 'bg-primary text-primary-foreground border-primary hover:bg-primary/90' : 'bg-background/50 text-muted-foreground',
						)}
						onClick={onInsert}
					>
						<Check className="h-3.5 w-3.5" />
					</Button>
				)}
			</div>
		</>
	)
}
