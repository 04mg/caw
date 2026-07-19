import { useState, useRef, useCallback, useEffect } from 'react'

interface SpeechRecognitionResult {
	isFinal: boolean
	[key: number]: { transcript: string }
	length: number
}

interface SpeechRecognitionResultList {
	length: number
	item(index: number): SpeechRecognitionResult
	[key: number]: SpeechRecognitionResult
}

interface SpeechRecognitionResultEvent extends Event {
	results: SpeechRecognitionResultList
	resultIndex: number
}

interface SpeechRecognitionErrorEvent extends Event {
	error: string
	message: string
}

interface SpeechRecognitionInstance extends EventTarget {
	continuous: boolean
	interimResults: boolean
	lang: string
	onresult: ((event: SpeechRecognitionResultEvent) => void) | null
	onerror: ((event: SpeechRecognitionErrorEvent) => void) | null
	onend: (() => void) | null
	start(): void
	stop(): void
	abort(): void
}

interface SpeechRecognitionConstructor {
	new(): SpeechRecognitionInstance
}

declare global {
	interface Window {
		SpeechRecognition?: SpeechRecognitionConstructor
		webkitSpeechRecognition?: SpeechRecognitionConstructor
	}
}

const SpeechRecognitionAPI =
	typeof window !== 'undefined'
		? window.SpeechRecognition || window.webkitSpeechRecognition
		: null

export function isVoiceSupported(): boolean {
	return !!SpeechRecognitionAPI
}

export function useSpeechRecognition() {
	const [isListening, setIsListening] = useState(false)
	const [transcript, setTranscript] = useState('')
	const [error, setError] = useState<string | null>(null)
	const recognitionRef = useRef<SpeechRecognitionInstance | null>(null)
	const shouldRestartRef = useRef(false)

	const reset = useCallback(() => {
		setTranscript('')
		setError(null)
	}, [])

	const stop = useCallback(() => {
		shouldRestartRef.current = false
		if (recognitionRef.current) {
			try {
				recognitionRef.current.stop()
			} catch {
				// already stopped
			}
		}
		setIsListening(false)
	}, [])

	const start = useCallback(() => {
		if (!SpeechRecognitionAPI) {
			setError('Voice input is not supported in this browser')
			return
		}

		const lang = localStorage.getItem('caw:voiceLanguage') || ''
		const recognition = new SpeechRecognitionAPI()
		recognition.continuous = true
		recognition.interimResults = true
		if (lang) recognition.lang = lang

		recognition.onresult = (event: SpeechRecognitionResultEvent) => {
			let finalText = ''
			let interimText = ''
			for (let i = event.resultIndex; i < event.results.length; i++) {
				const result = event.results[i]
				if (result.isFinal) {
					finalText += result[0].transcript
				} else {
					interimText += result[0].transcript
				}
			}
			setTranscript((prev) => {
				const base = prev.replace(/\u200B$/, '')
				if (finalText) return base + finalText
				return base + interimText
			})
		}

		recognition.onerror = (event: SpeechRecognitionErrorEvent) => {
			if (event.error === 'no-speech' || event.error === 'aborted') return
			setError(event.error === 'not-allowed'
				? 'Microphone permission denied'
				: `Speech error: ${event.error}`)
			setIsListening(false)
			shouldRestartRef.current = false
		}

		recognition.onend = () => {
			if (shouldRestartRef.current) {
				try {
					recognition.start()
				} catch {
					setIsListening(false)
					shouldRestartRef.current = false
				}
				return
			}
			setIsListening(false)
		}

		try {
			recognition.start()
			recognitionRef.current = recognition
			shouldRestartRef.current = true
			setIsListening(true)
			setError(null)
		} catch {
			setError('Failed to start voice recognition')
		}
	}, [])

	useEffect(() => {
		return () => {
			shouldRestartRef.current = false
			if (recognitionRef.current) {
				try {
					recognitionRef.current.stop()
				} catch {
					// ignore
				}
			}
		}
	}, [])

	return { isListening, transcript, error, start, stop, reset, setTranscript }
}
