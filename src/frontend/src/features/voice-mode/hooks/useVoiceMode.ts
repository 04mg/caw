import { useState, useEffect, useCallback } from 'react'
import { useKrokoVoiceMode, subscribeKrokoVoice, getKrokoVoiceState } from './useKrokoVoiceMode'
import { getVoiceMode } from '../services/krokoAsr'
import { joinTranscript } from '../services/transcript'

type VoicePhase = 'idle' | 'loading' | 'listening' | 'review'

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

let browserGlobalState: VoiceModeState = { phase: 'idle', transcript: '', error: null }
let browserListeners: Array<() => void> = []

function browserNotify() {
	for (const fn of browserListeners) fn()
}

function browserSetState(partial: Partial<VoiceModeState>) {
	browserGlobalState = { ...browserGlobalState, ...partial }
	browserNotify()
}

// State of whichever engine has a session in progress; falls back to the
// configured engine when both are idle. Reading the configured engine alone
// went wrong when the setting changed mid-session (or a component rendered
// before the change): the UI showed the idle engine while the other one kept
// the microphone open.
function activeVoiceState(): VoiceModeState {
	if (browserGlobalState.phase !== 'idle') return browserGlobalState
	const kroko = getKrokoVoiceState()
	if (kroko.phase !== 'idle') return kroko
	return getVoiceMode() === 'local' ? kroko : browserGlobalState
}

export function getVoicePhase(): VoicePhase {
	return activeVoiceState().phase
}

const SpeechRecognitionAPI =
	typeof window !== 'undefined'
		? window.SpeechRecognition || window.webkitSpeechRecognition
		: null

export function isVoiceSupported(): boolean {
	if (getVoiceMode() === 'local') return true
	return !!SpeechRecognitionAPI
}

// Errors reported by the Web Speech API, mapped to messages a user can act on.
// Anything not listed falls back to the raw error code.
function speechErrorMessage(code: string): string {
	switch (code) {
		case 'not-allowed':
			return window.isSecureContext === false
				? 'Microphone needs HTTPS (or localhost)'
				: 'Microphone permission denied'
		case 'service-not-allowed':
			return 'Speech recognition is disabled on this device'
		case 'audio-capture':
			return 'No microphone found'
		case 'network':
			return 'Speech service unreachable (network error)'
		case 'language-not-supported':
			return 'Voice language not supported by this browser'
		default:
			return `Speech error: ${code}`
	}
}

// A session that ends this quickly after starting, several times in a row,
// means the engine is failing rather than timing out on silence; restarting
// forever would just loop (and beep on Android each time).
const QUICK_END_MS = 1000
const MAX_QUICK_RESTARTS = 3

// The browser engine's session lives at module level, like its state: several
// components call useVoiceMode() (status bar, mobile control bar, hotkeys), and
// a session started from one must be stoppable from any other. Per-hook refs
// left the recognizer running when e.g. a click started it and Alt+M stopped it.
let activeRecognition: SpeechRecognitionInstance | null = null
// A recognizer stopped into the review phase. It stays attached until the
// review ends so the engine can still deliver the final text for the pending
// interim result.
let reviewRecognition: SpeechRecognitionInstance | null = null
let finalized = ''

function detach(recognition: SpeechRecognitionInstance | null) {
	if (!recognition) return
	recognition.onresult = null
	recognition.onerror = null
	recognition.onend = null
}

function abortRecognition(recognition: SpeechRecognitionInstance | null) {
	if (!recognition) return
	detach(recognition)
	try {
		recognition.abort()
	} catch {}
}

function browserStart() {
	if (!SpeechRecognitionAPI) {
		browserSetState({ error: 'Voice input is not supported in this browser' })
		return
	}
	if (window.isSecureContext === false) {
		browserSetState({ error: 'Microphone needs HTTPS (or localhost)' })
		return
	}

	abortRecognition(activeRecognition)
	abortRecognition(reviewRecognition)
	activeRecognition = null
	reviewRecognition = null

	const lang = localStorage.getItem('caw:voiceLanguage') || ''
	const recognition = new SpeechRecognitionAPI()
	recognition.continuous = true
	recognition.interimResults = true
	if (lang) recognition.lang = lang

	finalized = ''
	// Interim text of the running session. Committed when the engine ends a
	// session on its own (mobile engines do this after short pauses) so words
	// spoken right before the auto-restart aren't lost.
	let interim = ''
	let startedAt = Date.now()
	let quickEnds = 0

	recognition.onresult = (event: any) => {
		for (let i = event.resultIndex; i < event.results.length; i++) {
			const result = event.results[i]
			if (result.isFinal) {
				finalized = joinTranscript(finalized, result[0].transcript)
			}
		}
		let interimText = ''
		for (let i = event.resultIndex; i < event.results.length; i++) {
			const result = event.results[i]
			if (!result.isFinal) {
				interimText += result[0].transcript
			}
		}
		interim = interimText
		quickEnds = 0
		browserSetState({ transcript: joinTranscript(finalized, interimText) })
	}

	recognition.onerror = (event: any) => {
		if (event.error === 'no-speech' || event.error === 'aborted') return
		if (activeRecognition === recognition) activeRecognition = null
		abortRecognition(recognition)
		browserSetState({ error: speechErrorMessage(event.error), phase: 'idle' })
	}

	recognition.onend = () => {
		if (activeRecognition !== recognition || browserGlobalState.phase !== 'listening') return
		// Continuous recognition still ends on its own (silence timeouts,
		// mobile engines after every utterance): restart to keep listening.
		if (interim) {
			finalized = joinTranscript(finalized, interim)
			interim = ''
		}
		quickEnds = Date.now() - startedAt < QUICK_END_MS ? quickEnds + 1 : 0
		if (quickEnds >= MAX_QUICK_RESTARTS) {
			activeRecognition = null
			detach(recognition)
			browserSetState({ phase: 'idle', error: 'Voice recognition keeps stopping. Try again.' })
			return
		}
		try {
			startedAt = Date.now()
			recognition.start()
		} catch {
			activeRecognition = null
			detach(recognition)
			browserSetState({ phase: 'idle', error: 'Voice recognition stopped unexpectedly' })
		}
	}

	try {
		recognition.start()
		activeRecognition = recognition
		browserSetState({ phase: 'listening', transcript: '', error: null })
	} catch {
		detach(recognition)
		browserSetState({ error: 'Failed to start voice recognition' })
	}
}

function browserStop(autoSend?: { send: (text: string) => void }) {
	const recognition = activeRecognition
	activeRecognition = null
	if (recognition) {
		recognition.onend = null
		recognition.onerror = null
		try {
			recognition.stop()
		} catch {}
	}
	const text = browserGlobalState.transcript.trim()
	if (!text || autoSend) {
		detach(recognition)
		finalized = ''
		browserSetState({ phase: 'idle', transcript: '', error: null })
		if (text && autoSend) autoSend.send(text)
		return
	}
	reviewRecognition = recognition
	browserSetState({ phase: 'review' })
}

function browserReset() {
	abortRecognition(activeRecognition)
	abortRecognition(reviewRecognition)
	activeRecognition = null
	reviewRecognition = null
	finalized = ''
	browserSetState({ phase: 'idle', transcript: '', error: null })
}

const browser = { start: browserStart, stop: browserStop, reset: browserReset }

export function useVoiceMode() {
	const [, forceRender] = useState(0)
	const kroko = useKrokoVoiceMode()

	useEffect(() => {
		const browserListener = () => forceRender((n) => n + 1)
		browserListeners.push(browserListener)

		const unsubscribeKroko = subscribeKrokoVoice(() => forceRender((n) => n + 1))

		return () => {
			browserListeners = browserListeners.filter((l) => l !== browserListener)
			unsubscribeKroko()
		}
	}, [])

	const start = useCallback(async () => {
		// Resolve the engine at call time: the setting can change while this
		// component stays mounted.
		if (getVoiceMode() === 'local') {
			await kroko.start()
		} else {
			browser.start()
		}
	}, [kroko])

	const stop = useCallback(
		(autoSend?: { send: (text: string) => void }) => {
			if (getKrokoVoiceState().phase !== 'idle') {
				kroko.stop(autoSend)
			} else {
				browser.stop(autoSend)
			}
		},
		[kroko],
	)

	const reset = useCallback(() => {
		kroko.reset()
		browser.reset()
	}, [kroko])

	const activeState = activeVoiceState()

	return {
		phase: activeState.phase,
		transcript: activeState.transcript,
		error: activeState.error,
		start,
		stop,
		reset,
	}
}
