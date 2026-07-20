const WASM_CDN_BASE =
	'https://cdn.jsdelivr.net/npm/@siteed/sherpa-onnx.rn@1.3.1/wasm/'
const KROKO_API_BASE = 'https://license.kroko.ai/api/public/v1'
const CACHE_NAME = 'kroko-sdk'
const MODEL_FS_PREFIX = '/caw-models'

declare global {
	interface Window {
		Module: any
		createOnlineRecognizer: any
		SherpaOnnx: any
	}
}

let moduleReady: Promise<any> | null = null

function loadScript(src: string): Promise<void> {
	return new Promise((resolve, reject) => {
		const existing = document.querySelector<HTMLScriptElement>(
			`script[src="${src}"]`,
		)
		if (existing) {
			resolve()
			return
		}
		const script = document.createElement('script')
		script.src = src
		script.onload = () => resolve()
		script.onerror = () => reject(new Error(`Failed to load: ${src}`))
		document.head.appendChild(script)
	})
}

export async function loadSherpaOnnx(): Promise<any> {
	if (moduleReady) return moduleReady

	moduleReady = (async () => {
		window.Module = {
			locateFile: (path: string) => WASM_CDN_BASE + path,
		}

		await loadScript('./wasm/sherpa-onnx-wasm-combined.js')

		await new Promise<void>((resolve, reject) => {
			window.Module.onRuntimeInitialized = resolve
			window.Module.onAbort = () =>
				reject(new Error('Sherpa-ONNX WASM failed to initialize'))
		})

		window.SherpaOnnx = {}
		await loadScript('./wasm/sherpa-onnx-asr.js')

		return window.Module
	})()

	moduleReady.catch(() => {
		moduleReady = null
	})
	return moduleReady
}

export interface KrokoModel {
	model_id: string
	name: string
	language_iso: string
	file_size: number
	type: string
	url: string
	streaming: boolean
}

export interface KrokoLanguage {
	iso: string
	name: string
}

let cachedLanguages: KrokoLanguage[] | null = null
let cachedModels: KrokoModel[] | null = null

export async function fetchLanguages(): Promise<KrokoLanguage[]> {
	if (cachedLanguages) return cachedLanguages
	const res = await fetch(`${KROKO_API_BASE}/languages`)
	if (!res.ok) throw new Error('Failed to fetch languages')
	const data: KrokoLanguage[] = await res.json()
	cachedLanguages = data.sort((a, b) => a.name.localeCompare(b.name))
	return cachedLanguages
}

export async function fetchModels(): Promise<KrokoModel[]> {
	if (cachedModels) return cachedModels
	const res = await fetch(`${KROKO_API_BASE}/models`)
	if (!res.ok) throw new Error('Failed to fetch models')
	const data: KrokoModel[] = await res.json()
	cachedModels = data
		.filter((m) => m.streaming && m.type === 'free')
		.sort((a, b) => a.name.localeCompare(b.name))
	return cachedModels
}

async function fetchWithCache(
	url: string,
	onProgress?: (downloaded: number, total: number) => void,
): Promise<Response> {
	const cache = await caches.open(CACHE_NAME)
	const cached = await cache.match(url)
	if (cached) return cached
	const response = await fetch(url)
	if (!response.ok) throw new Error(`Failed to fetch: ${url}`)

	const total = Number(response.headers.get('Content-Length') || 0)
	if (!onProgress || !response.body || total === 0) {
		await cache.put(url, response.clone())
		return response
	}

	const reader = response.body.getReader()
	const chunks: Uint8Array[] = []
	let downloaded = 0
	for (;;) {
		const { done, value } = await reader.read()
		if (done) break
		if (value) {
			chunks.push(value)
			downloaded += value.byteLength
			onProgress(downloaded, total)
		}
	}
	const blob = new Blob(chunks as BlobPart[])
	const cachedResponse = new Response(blob, {
		status: response.status,
		statusText: response.statusText,
		headers: response.headers,
	})
	await cache.put(url, cachedResponse.clone())
	return cachedResponse
}

async function createCacheEntry(
	name: string,
	contents: Uint8Array,
): Promise<void> {
	const cache = await caches.open(CACHE_NAME)
	const key = `${MODEL_FS_PREFIX}/${name}`
	const existing = await cache.match(key)
	if (!existing) {
		const response = new Response(new Uint8Array(contents), {
			status: 200,
			statusText: 'OK',
		})
		await cache.put(key, response.clone())
	}
}

async function readCacheEntry(name: string): Promise<Uint8Array | null> {
	const cache = await caches.open(CACHE_NAME)
	const res = await cache.match(`${MODEL_FS_PREFIX}/${name}`)
	if (!res) return null
	return new Uint8Array(await res.arrayBuffer())
}

interface ModelData {
	header: Record<string, any>
	blob: Uint8Array
	encoder: Uint8Array
	decoder: Uint8Array
	joiner: Uint8Array
	tokens: Uint8Array
}

async function unpackModel(
	url: string,
	onProgress?: (downloaded: number, total: number) => void,
): Promise<void> {
	const res = await fetchWithCache(url, onProgress)
	const arrayBuf = await res.arrayBuffer()
	const data = new Uint8Array(arrayBuf)

	if (data.byteLength < 4) throw new Error('Invalid model file')

	const view = new DataView(data.buffer)
	const headerLen = view.getUint32(0, true)
	if (data.byteLength < 4 + headerLen) throw new Error('Invalid model header')

	const headerBytes = data.slice(4, 4 + headerLen)
	const header = JSON.parse(new TextDecoder().decode(headerBytes))
	const blob = data.slice(4 + headerLen)

	const modelData: ModelData = {
		header,
		blob,
		encoder: new Uint8Array(0),
		decoder: new Uint8Array(0),
		joiner: new Uint8Array(0),
		tokens: new Uint8Array(0),
	}

	if (blob.byteLength < 4) throw new Error('Invalid model payload')

	let offset = 0
	const readBlock = (): Uint8Array => {
		if (offset + 4 > blob.length) throw new Error('Invalid block header')
		const len = new DataView(blob.buffer, blob.byteOffset).getUint32(
			offset,
			true,
		)
		offset += 4
		if (offset + len > blob.length) throw new Error('Block size mismatch')
		const buf = blob.slice(offset, offset + len)
		offset += len
		return buf
	}

	modelData.encoder = readBlock()
	modelData.decoder = readBlock()
	modelData.joiner = readBlock()
	modelData.tokens = readBlock()

	await Promise.all([
		createCacheEntry('encoder', modelData.encoder),
		createCacheEntry('decoder', modelData.decoder),
		createCacheEntry('joiner', modelData.joiner),
		createCacheEntry('tokens', modelData.tokens),
	])
}

export async function downloadModel(
	url: string,
	onProgress?: (downloadedMB: number, totalMB: number) => void,
): Promise<void> {
	await unpackModel(url, (downloaded, total) => {
		onProgress?.(downloaded / 1000 / 1000, total / 1000 / 1000)
	})
}

export async function isModelCached(url: string): Promise<boolean> {
	try {
		const cache = await caches.open(CACHE_NAME)
		const res = await cache.match(url)
		return !!res
	} catch {
		return false
	}
}

export async function deleteModel(): Promise<void> {
	await caches.delete(CACHE_NAME)
}

export async function createKrokoRecognizer(): Promise<any> {
	const mod = await loadSherpaOnnx()

	const encoderBuf = await readCacheEntry('encoder')
	const decoderBuf = await readCacheEntry('decoder')
	const joinerBuf = await readCacheEntry('joiner')
	const tokensBuf = await readCacheEntry('tokens')

	if (!encoderBuf || !decoderBuf || !joinerBuf || !tokensBuf) {
		throw new Error('Model not downloaded')
	}

	const fs = mod.FS
	try {
		fs.mkdir(MODEL_FS_PREFIX)
	} catch {}
	fs.writeFile(`${MODEL_FS_PREFIX}/encoder.onnx`, encoderBuf)
	fs.writeFile(`${MODEL_FS_PREFIX}/decoder.onnx`, decoderBuf)
	fs.writeFile(`${MODEL_FS_PREFIX}/joiner.onnx`, joinerBuf)
	fs.writeFile(`${MODEL_FS_PREFIX}/tokens.txt`, tokensBuf)

	const recognizer = window.createOnlineRecognizer(mod, {
		featConfig: {
			sampleRate: 16000,
			featureDim: 80,
		},
		modelConfig: {
			transducer: {
				encoder: `${MODEL_FS_PREFIX}/encoder.onnx`,
				decoder: `${MODEL_FS_PREFIX}/decoder.onnx`,
				joiner: `${MODEL_FS_PREFIX}/joiner.onnx`,
			},
			tokens: `${MODEL_FS_PREFIX}/tokens.txt`,
			provider: 'cpu',
			numThreads: 1,
			debug: 0,
		},
		decodingMethod: 'greedy_search',
		enableEndpoint: 1,
		rule1MinTrailingSilence: 2.4,
		rule2MinTrailingSilence: 1.2,
		rule3MinUtteranceLength: 300,
	})

	return recognizer
}

export function getVoiceMode(): 'browser' | 'local' {
	if (typeof localStorage === 'undefined') return 'browser'
	return (localStorage.getItem('caw:voiceMode') as 'browser' | 'local') || 'browser'
}

export function setVoiceMode(mode: 'browser' | 'local'): void {
	localStorage.setItem('caw:voiceMode', mode)
}

export function getKrokoLanguage(): string {
	if (typeof localStorage === 'undefined') return ''
	return localStorage.getItem('caw:krokoLanguage') || ''
}

export function setKrokoLanguage(iso: string): void {
	localStorage.setItem('caw:krokoLanguage', iso)
}

export function isKrokoSupported(): boolean {
	return typeof window !== 'undefined' && typeof caches !== 'undefined'
}

export function downsampleBuffer(
	buffer: Float32Array,
	recordSampleRate: number,
	exportSampleRate: number,
): Float32Array {
	if (exportSampleRate === recordSampleRate) return buffer
	const sampleRateRatio = recordSampleRate / exportSampleRate
	const newLength = Math.round(buffer.length / sampleRateRatio)
	const result = new Float32Array(newLength)
	let offsetResult = 0
	let offsetBuffer = 0
	while (offsetResult < result.length) {
		const nextOffsetBuffer = Math.round((offsetResult + 1) * sampleRateRatio)
		let accum = 0
		let count = 0
		for (let i = offsetBuffer; i < nextOffsetBuffer && i < buffer.length; i++) {
			accum += buffer[i]
			count++
		}
		result[offsetResult] = accum / count
		offsetResult++
		offsetBuffer = nextOffsetBuffer
	}
	return result
}
