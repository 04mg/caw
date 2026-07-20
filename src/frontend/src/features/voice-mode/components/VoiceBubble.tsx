import { useRef, useState, useEffect } from 'react'
import { createPortal } from 'react-dom'
import { Check, X } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'

interface VoiceBubbleProps {
	transcript: string
	error: string | null
	isListening: boolean
	onSend: () => void
	onDiscard: () => void
}

export function VoiceBubble({ transcript, error, isListening, onSend, onDiscard }: VoiceBubbleProps) {
	const hasContent = transcript.trim().length > 0
	const scrollRef = useRef<HTMLDivElement>(null)
	const [isOverflowing, setIsOverflowing] = useState(false)

	useEffect(() => {
		const el = scrollRef.current
		if (el) {
			setIsOverflowing(el.scrollHeight > el.clientHeight)
		}
	}, [transcript])

	if (!hasContent && !error) return null

	return createPortal(
		<div className="voice-bubble-container">
			<div className="voice-bubble">
				<div ref={scrollRef} className={cn('voice-bubble-scroll', isOverflowing && 'voice-bubble-scroll-fade')}>
					<div className="voice-bubble-content">
						{error ? (
							<span className="text-xs text-red-400">{error}</span>
						) : (
							<span className="text-xs text-foreground leading-relaxed">{transcript}</span>
						)}

						{isListening && hasContent && (
							<div className="voice-bubble-pulse" />
						)}
					</div>
				</div>

				{!isListening && (
					<div className="voice-bubble-footer">
						<button
							onClick={onDiscard}
							className="voice-bubble-btn"
							title="Discard"
						>
							<X className="h-3.5 w-3.5" />
						</button>
						<button
							onClick={onSend}
							disabled={!hasContent}
							className={cn(
								"voice-bubble-btn",
								!hasContent && "opacity-40 cursor-not-allowed"
							)}
							title="Send to terminal"
						>
							<Check className="h-3.5 w-3.5" />
						</button>
					</div>
				)}
			</div>
		</div>,
		document.body
	)
}