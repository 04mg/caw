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

	return createPortal(
		<div className="voice-bubble-container">
			<div className="voice-bubble">
				<div className="voice-bubble-content">
					{error ? (
						<span className="text-xs text-red-400 font-sans">{error}</span>
					) : transcript ? (
						<span className="text-xs text-foreground font-sans leading-relaxed">{transcript}</span>
					) : isListening ? (
						<span className="text-xs text-muted-foreground font-sans italic">Listening...</span>
					) : null}

					{isListening && (
						<div className="voice-bubble-pulse" />
					)}
				</div>

				{!isListening && (
					<div className="voice-bubble-actions">
						<button
							onClick={onDiscard}
							className={cn(
								"voice-bubble-btn",
								"text-muted-foreground hover:text-foreground hover:bg-destructive/10"
							)}
							title="Discard"
						>
							<X className="h-3.5 w-3.5" />
						</button>
						<button
							onClick={onSend}
							disabled={!hasContent}
							className={cn(
								"voice-bubble-btn",
								hasContent
									? "text-primary hover:bg-primary/10"
									: "text-muted-foreground/40 cursor-not-allowed"
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
