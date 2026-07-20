import { useState, useEffect, useCallback, useRef } from 'react'
import { useKrokoVoiceMode } from './useKrokoVoiceMode'
import { getVoiceMode } from '../services/krokoAsr'

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
	if (getVoiceMode() === 'local') return true
	return !!SpeechRecognitionAPI
}

function BrowserVoiceMode() {
	const speechRef = useRef<any>(null)
	const finalizedRef = useRef('')

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

	const stop = useCallback((autoSend?: { send: (text: string) => void }) => {
		if (speechRef.current) {
			try {
				speechRef.current.stop()
			} catch {}
			speechRef.current = null
		}
		const text = globalState.transcript.trim()
		if (!text) {
			setState({ phase: 'idle', transcript: '', error: null })
			return
		}
		if (autoSend) {
			autoSend.send(text)
			finalizedRef.current = ''
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

	return { start, stop, reset }
}

export function useVoiceMode() {
	const [, forceRender] = useState(0)
	const kroko = useKrokoVoiceMode()
	const browser = useRef(BrowserVoiceMode()).current

	useEffect(() => {
		const listener = () => forceRender((n) => n + 1)
		listeners.push(listener)
		return () => {
			listeners = listeners.filter((l) => l !== listener)
		}
	}, [])

	const mode = getVoiceMode()

	const start = useCallback(async () => {
		if (mode === 'local') {
			await kroko.start()
		} else {
			await browser.start()
		}
	}, [mode, kroko, browser])

	const stop = useCallback(
		(autoSend?: { send: (text: string) => void }) => {
			if (mode === 'local') {
				kroko.stop(autoSend)
			} else {
				browser.stop(autoSend)
			}
		},
		[mode, kroko, browser],
	)

	const reset = useCallback(() => {
		if (mode === 'local') {
			kroko.reset()
		} else {
			browser.reset()
		}
	}, [mode, kroko, browser])

	return {
		phase: globalState.phase,
		transcript: globalState.transcript,
		error: globalState.error,
		start,
		stop,
		reset,
	}
}
