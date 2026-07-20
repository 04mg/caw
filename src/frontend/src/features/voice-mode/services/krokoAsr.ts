const KROKO_SDK_URL =
	'https://huggingface.co/spaces/Banafo/Kroko-Streaming-ASR-Wasm/resolve/main/kroko-sdk.js'
const KROKO_API_BASE = 'https://license.kroko.ai/api/public/v1'
const CACHE_NAME = 'kroko-sdk'

let sdkModule: any = null
let sdkLoadPromise: Promise<any> | null = null

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

export async function loadKrokoSdk(): Promise<any> {
	if (sdkModule) return sdkModule
	if (sdkLoadPromise) return sdkLoadPromise

	sdkLoadPromise = (async () => {
		const mod = await import(KROKO_SDK_URL)
		sdkModule = mod
		return mod
	})()
	return sdkLoadPromise
}

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

async function fetchWithCache(url: string): Promise<Response> {
	const cache = await caches.open(CACHE_NAME)
	const cached = await cache.match(url)
	if (cached) return cached
	const response = await fetch(url)
	if (!response.ok) throw new Error(`Failed to fetch: ${url}`)
	await cache.put(url, response.clone())
	return response
}

async function createCacheEntry(name: string, contents: Uint8Array): Promise<string> {
	const dummyUrl = `${KROKO_SDK_URL}/${name}`
	const cache = await caches.open(CACHE_NAME)
	const existing = await cache.match(dummyUrl)
	if (!existing) {
		const response = new Response(new Uint8Array(contents), { status: 200, statusText: 'OK' })
		await cache.put(dummyUrl, response.clone())
	}
	return dummyUrl
}

interface ModelData {
	header: Record<string, any>
	blob: Uint8Array
	encoder: Uint8Array
	decoder: Uint8Array
	joiner: Uint8Array
	tokens: Uint8Array
}

async function unpackModel(url: string): Promise<[string, string, string, string]> {
	const res = await fetchWithCache(url)
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
		const len = new DataView(blob.buffer, blob.byteOffset).getUint32(offset, true)
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

	return Promise.all([
		createCacheEntry('encoder', modelData.encoder),
		createCacheEntry('decoder', modelData.decoder),
		createCacheEntry('joiner', modelData.joiner),
		createCacheEntry('tokens', modelData.tokens),
	])
}

export async function downloadModel(
	url: string,
	onProgress?: (status: string) => void,
): Promise<void> {
	onProgress?.('Downloading model...')
	const [encoder, decoder, joiner, tokens] = await unpackModel(url)
	void encoder
	void decoder
	void joiner
	void tokens
	onProgress?.('Model ready')
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
	const mod = await loadKrokoSdk()

	const KrokoWorker = mod.KrokoWorker
	if (!KrokoWorker) throw new Error('KrokoWorker not available')

	const worker = new KrokoWorker()
	const encoderUrl = `${KROKO_SDK_URL}/encoder`
	const decoderUrl = `${KROKO_SDK_URL}/decoder`
	const joinerUrl = `${KROKO_SDK_URL}/joiner`
	const tokensUrl = `${KROKO_SDK_URL}/tokens`

	const recognizer = await worker.createOnlineRecognizer({
		modelConfig: {
			transducer: {
				encoder: encoderUrl,
				decoder: decoderUrl,
				joiner: joinerUrl,
			},
			tokens: tokensUrl,
		},
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
