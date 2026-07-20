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

	if (!hasContent && !error) return null

	return createPortal(
		<div className="voice-bubble-container">
			<div className="voice-bubble">
				<div className="voice-bubble-content">
					{error ? (
						<span className="text-xs text-red-400 font-sans">{error}</span>
					) : (
						<span className="text-xs text-foreground font-sans leading-relaxed">{transcript}</span>
					)}

					{isListening && hasContent && (
						<div className="voice-bubble-pulse" />
					)}
				</div>
			</div>

			{!isListening && (
				<div className="voice-bubble-actions">
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
		</div>,
		document.body
	)
}
