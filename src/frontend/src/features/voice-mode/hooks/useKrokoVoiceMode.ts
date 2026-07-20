import { useState, useEffect, useCallback, useRef } from 'react'
import {
	createKrokoRecognizer,
	downsampleBuffer,
	loadKrokoSdk,
} from '../services/krokoAsr'

type VoicePhase = 'idle' | 'loading' | 'listening' | 'review'

interface KrokoVoiceState {
	phase: VoicePhase
	transcript: string
	error: string | null
}

const EXPECTED_SAMPLE_RATE = 16000

let globalState: KrokoVoiceState = { phase: 'idle', transcript: '', error: null }
let listeners: Array<() => void> = []

function notify() {
	for (const fn of listeners) fn()
}

function setState(partial: Partial<KrokoVoiceState>) {
	globalState = { ...globalState, ...partial }
	notify()
}

export function getKrokoVoicePhase(): VoicePhase {
	return globalState.phase
}

export function getKrokoVoiceState(): KrokoVoiceState {
	return globalState
}

export function subscribeKrokoVoice(cb: () => void): () => void {
	listeners.push(cb)
	return () => {
		listeners = listeners.filter((l) => l !== cb)
	}
}

let recognizerCache: any = null

export async function ensureRecognizer(): Promise<any> {
	if (recognizerCache) return recognizerCache
	await loadKrokoSdk()
	try {
		recognizerCache = await createKrokoRecognizer()
	} catch (err) {
		recognizerCache = null
		throw err
	}
	return recognizerCache
}

export function useKrokoVoiceMode() {
	const [, forceRender] = useState(0)
	const audioCtxRef = useRef<AudioContext | null>(null)
	const mediaStreamRef = useRef<MediaStream | null>(null)
	const processorRef = useRef<ScriptProcessorNode | null>(null)
	const recognizerRef = useRef<any>(null)
	const streamRef = useRef<any>(null)
	const finalizedRef = useRef('')
	const activeRef = useRef(false)
	const sessionGenRef = useRef(0)

	useEffect(() => {
		const listener = () => forceRender((n) => n + 1)
		listeners.push(listener)
		return () => {
			listeners = listeners.filter((l) => l !== listener)
		}
	}, [])

	const cleanup = useCallback(() => {
		activeRef.current = false
		sessionGenRef.current++ // invalidate any in-flight handler from this session
		if (processorRef.current) {
			try {
				processorRef.current.onaudioprocess = null
			} catch {}
			try {
				processorRef.current.disconnect()
			} catch {}
			processorRef.current = null
		}
		if (mediaStreamRef.current) {
			mediaStreamRef.current.getTracks().forEach((t) => t.stop())
			mediaStreamRef.current = null
		}
		if (audioCtxRef.current) {
			try {
				audioCtxRef.current.close()
			} catch {}
			audioCtxRef.current = null
		}
		streamRef.current = null
		finalizedRef.current = ''
	}, [])

	const start = useCallback(async () => {
		const gen = ++sessionGenRef.current
		try {
			setState({ phase: 'loading', transcript: '', error: null })
			finalizedRef.current = ''
			activeRef.current = false

			const recognizer = await ensureRecognizer()
			if (gen !== sessionGenRef.current) return
			recognizerRef.current = recognizer

			const stream = await navigator.mediaDevices.getUserMedia({
				audio: {
					echoCancellation: true,
					noiseSuppression: true,
					sampleRate: EXPECTED_SAMPLE_RATE,
				} as any,
			})
			if (gen !== sessionGenRef.current) {
				stream.getTracks().forEach((t) => t.stop())
				return
			}
			mediaStreamRef.current = stream

			const audioCtx = new AudioContext({ sampleRate: EXPECTED_SAMPLE_RATE })
			if (gen !== sessionGenRef.current) {
				stream.getTracks().forEach((t) => t.stop())
				audioCtx.close()
				return
			}
			audioCtxRef.current = audioCtx

			const source = audioCtx.createMediaStreamSource(stream)
			const bufferSize = 4096
			const processor = audioCtx.createScriptProcessor(bufferSize, 1, 1)
			processorRef.current = processor

			let recognizerStream: any = null
			streamRef.current = null

			processor.onaudioprocess = async (e: AudioProcessingEvent) => {
				if (gen !== sessionGenRef.current) return

				let samples: Float32Array = new Float32Array(e.inputBuffer.getChannelData(0))
				const recordRate = audioCtx.sampleRate
				if (recordRate !== EXPECTED_SAMPLE_RATE) {
					samples = downsampleBuffer(samples, recordRate, EXPECTED_SAMPLE_RATE)
				}

				try {
					if (!recognizerStream) {
						recognizerStream = await recognizer.createStream()
						if (gen !== sessionGenRef.current) return
						streamRef.current = recognizerStream
					}

					await recognizerStream.acceptWaveform(EXPECTED_SAMPLE_RATE, samples)
					if (gen !== sessionGenRef.current) return

					while (await recognizer.isReady(recognizerStream)) {
						if (gen !== sessionGenRef.current) return
						await recognizer.decode(recognizerStream)
					}
					if (gen !== sessionGenRef.current) return

					const isEndpoint = await recognizer.isEndpoint(recognizerStream)
					if (gen !== sessionGenRef.current) return
					const result = await recognizer.getResult(recognizerStream)
					if (gen !== sessionGenRef.current) return
					const text = result.text || ''

					if (text.length > 0 && finalizedRef.current + text !== globalState.transcript) {
						setState({ transcript: finalizedRef.current + text })
					}

					if (isEndpoint) {
						if (text.length > 0) {
							finalizedRef.current += text
						}
						await recognizer.reset(recognizerStream)
						if (gen !== sessionGenRef.current) return
						recognizerStream = null
						streamRef.current = null
					}
				} catch {
					// recognizer errors during processing — ignore, keep listening
				}
			}

			source.connect(processor)
			processor.connect(audioCtx.destination)
			activeRef.current = true
			if (gen !== sessionGenRef.current) return
			setState({ phase: 'listening' })
		} catch (err: any) {
			if (gen !== sessionGenRef.current) return
			cleanup()
			setState({
				error:
					err.name === 'NotAllowedError'
						? 'Microphone permission denied'
						: err.message || 'Failed to start local voice recognition',
				phase: 'idle',
			})
		}
	}, [cleanup])

	const stop = useCallback(
		(autoSend?: { send: (text: string) => void }) => {
			cleanup()
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
		},
		[cleanup],
	)

	const reset = useCallback(() => {
		cleanup()
		finalizedRef.current = ''
		setState({ phase: 'idle', transcript: '', error: null })
	}, [cleanup])

	return {
		phase: globalState.phase,
		transcript: globalState.transcript,
		error: globalState.error,
		start,
		stop,
		reset,
	}
}
