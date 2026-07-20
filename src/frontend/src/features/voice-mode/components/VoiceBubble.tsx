import { createPortal } from 'react-dom'
import { Check, X } from 'lucide-react'
import { cn } from '@/features/shared/utils/utils'

interface VoiceBubbleProps {
	transcript: string
	error: string | null
	isListening: boolean
	onSend: () => void
	onDiscard: () => void
	targetRect?: DOMRect | null
}

export function VoiceBubble({ transcript, error, isListening, onSend, onDiscard, targetRect }: VoiceBubbleProps) {
	const hasContent = transcript.trim().length > 0

	if (!hasContent && !error) return null

	const style: React.CSSProperties = targetRect
		? {
			position: 'fixed',
			top: targetRect.top - 8,
			left: targetRect.left + targetRect.width / 2,
			transform: 'translateX(-50%) translateY(-100%)',
		}
		: {}

	return createPortal(
		<div className="voice-bubble-container" style={style}>
			<div className="voice-bubble">
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
