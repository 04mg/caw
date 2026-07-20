import { useState, useEffect, useCallback, useRef } from 'react'

type VoicePhase = 'idle' | 'listening' | 'review'

interface VoiceModeState {
	phase: VoicePhase
	transcript: string
	error: string | null
}

interface SpeechRecognitionInstance extends EventTarget {
	continuous: boolean
	interimResults: boolean
	lang: string
	onresult: ((event: any) => void) | null
	onerror: ((event: any) => void) | null
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

let globalState: VoiceModeState = { phase: 'idle', transcript: '', error: null }
let listeners: Array<() => void> = []

function notify() {
	for (const fn of listeners) fn()
}

function setState(partial: Partial<VoiceModeState>) {
	globalState = { ...globalState, ...partial }
	notify()
}

export function getVoicePhase(): VoicePhase {
	return globalState.phase
}

const SpeechRecognitionAPI =
	typeof window !== 'undefined'
		? window.SpeechRecognition || window.webkitSpeechRecognition
		: null

export function isVoiceSupported(): boolean {
	return !!SpeechRecognitionAPI
}

export function useVoiceMode() {
	const [, forceRender] = useState(0)
	const speechRef = useRef<any>(null)
	const finalizedRef = useRef('')

	useEffect(() => {
		const listener = () => forceRender((n) => n + 1)
		listeners.push(listener)
		return () => {
			listeners = listeners.filter((l) => l !== listener)
		}
	}, [])

	const start = useCallback(async () => {
		if (!SpeechRecognitionAPI) {
			setState({ error: 'Voice input is not supported in this browser' })
			return
		}

		const lang = localStorage.getItem('caw:voiceLanguage') || ''
		const recognition = new SpeechRecognitionAPI()
		recognition.continuous = true
		recognition.interimResults = true
		if (lang) recognition.lang = lang

		finalizedRef.current = ''

		recognition.onresult = (event: any) => {
			for (let i = event.resultIndex; i < event.results.length; i++) {
				const result = event.results[i]
				if (result.isFinal) {
					finalizedRef.current += result[0].transcript
				}
			}
			let interimText = ''
			for (let i = event.resultIndex; i < event.results.length; i++) {
				const result = event.results[i]
				if (!result.isFinal) {
					interimText += result[0].transcript
				}
			}
			setState({ transcript: finalizedRef.current + interimText })
		}

		recognition.onerror = (event: any) => {
			if (event.error === 'no-speech' || event.error === 'aborted') return
			setState({
				error: event.error === 'not-allowed'
					? 'Microphone permission denied'
					: `Speech error: ${event.error}`,
				phase: 'idle',
			})
		}

		recognition.onend = () => {
			if (globalState.phase === 'listening') {
				try {
					recognition.start()
				} catch {
					setState({ phase: 'idle' })
				}
				return
			}
		}

		try {
			recognition.start()
			speechRef.current = recognition
			setState({ phase: 'listening', transcript: '', error: null })
		} catch {
			setState({ error: 'Failed to start voice recognition' })
		}
	}, [])

	const stop = useCallback(() => {
		if (speechRef.current) {
			try {
				speechRef.current.stop()
			} catch {}
			speechRef.current = null
		}
		if (!globalState.transcript.trim()) {
			setState({ phase: 'idle', transcript: '', error: null })
		} else {
			setState({ phase: 'review' })
		}
	}, [])

	const reset = useCallback(() => {
		if (speechRef.current) {
			try {
				speechRef.current.stop()
			} catch {}
			speechRef.current = null
		}
		finalizedRef.current = ''
		setState({ phase: 'idle', transcript: '', error: null })
	}, [])

	return {
		phase: globalState.phase,
		transcript: globalState.transcript,
		error: globalState.error,
		start,
		stop,
		reset,
	}
}
