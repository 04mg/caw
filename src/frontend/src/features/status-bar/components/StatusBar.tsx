import { useState, useEffect, useCallback } from 'react'
import {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuSeparator,
} from '@/components/dropdown-menu'
import { RefreshCw, Key, Check, Loader2, ChevronUp, Workflow, Folder, SquareKanban } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, Claude, Codex, GithubCopilot, OpenRouter } from '@lobehub/icons'
import { cn } from '@/features/shared/utils/utils'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/tooltip'

interface Quota {
	used:  number
	limit: number
	unit?: string // "" | "percentage" | "credits" | "count" | "info"
	resetTime?: string
}

interface QuotaItem {
	name:        string
	label:       string
	description: string
	used:        number
	limit:       number
	unit?:       string
	resetTime?:  string
}

interface QuotaGroup {
	name:        string
	description: string
	items:       QuotaItem[]
}

interface QuotaResponse {
	fiveHour: Quota
	weekly:   Quota
	monthly:  Quota
	groups?:  QuotaGroup[]
}

interface ProviderData {
	data?: QuotaResponse
	error?: string
}

interface AllQuotas {
	antigravity?: ProviderData
	opencode?:    ProviderData
	ollama?:      ProviderData
	claude?:     ProviderData
	codex?:      ProviderData
	copilot?:    ProviderData
	openrouter?: ProviderData
}

const formatQuotaValue = (used: number, limit: number, unit?: string): { text: string, percentage: number } => {
	if (unit === 'info') {
		return { text: '', percentage: 0 }
	}
	if (unit === 'percentage' || !unit) {
		return { text: `${used}%`, percentage: used }
	}
	// credits / count
	const pct = limit > 0 ? Math.round((used / limit) * 100) : 0
	return { text: `${used}/${limit}`, percentage: pct }
}

const formatResetTime = (iso: string): string => {
	const then = new Date(iso).getTime()
	if (isNaN(then)) return iso
	const diff = then - Date.now()
	if (diff <= 0) return 'soon'
	const days = Math.floor(diff / 86400000)
	const hours = Math.floor((diff % 86400000) / 3600000)
	const minutes = Math.floor((diff % 3600000) / 60000)
	if (days >= 1) return `${days}d ${hours}h`
	if (hours >= 1) return `${hours}h ${minutes}m`
	return `${minutes}m`
}

interface StatusBarProps {
	workspaceName?: string
	worktreeBranch?: string
	agentBoardOpen?: boolean
	onToggleAgentBoard?: () => void
	onOpenSettings: (section?: string) => void
	hideControlCenter?: boolean
}

export function StatusBar({ workspaceName, worktreeBranch, agentBoardOpen, onToggleAgentBoard, onOpenSettings, hideControlCenter }: StatusBarProps) {
	const [quotas, setQuotas] = useState<AllQuotas | null>(null)
	const [settings, setSettings] = useState<Record<string, Record<string, string>>>({})
	const [isLoading, setIsLoading] = useState(false)
	const [selectedView, setSelectedView] = useState<string>('')

	const fetchSettings = useCallback(async () => {
		try {
			const res = await fetch('/api/quotas/settings')
			if (res.ok) {
				const json = await res.json()
				setSettings(json?.data || {})
			}
		} catch (e) {
			console.error('Error fetching settings', e)
		}
	}, [])

	const fetchQuotas = useCallback(async () => {
		setIsLoading(true)
		try {
			const res = await fetch('/api/quotas')
			if (res.ok) {
				const json = await res.json()
				setQuotas(json?.data)
			} else {
				const json = await res.json().catch(() => null)
				console.error('Failed to fetch quotas', json?.error?.message ?? res.statusText)
			}
		} catch (e) {
			console.error('Error fetching quotas', e)
		} finally {
			setIsLoading(false)
		}
	}, [])

	const refreshAll = useCallback(async () => {
		await fetchSettings()
		await fetchQuotas()
	}, [fetchSettings, fetchQuotas])

	// Auto poll every 60s
	useEffect(() => {
		refreshAll()
		const timer = setInterval(fetchQuotas, 60000)

		window.addEventListener('caw:settings-updated', refreshAll)

		return () => {
			clearInterval(timer)
			window.removeEventListener('caw:settings-updated', refreshAll)
		}
	}, [refreshAll, fetchQuotas])

	// Load / initialize selected view
	useEffect(() => {
		const saved = localStorage.getItem('caw:quota:selected_view')
		if (saved) {
			setSelectedView(saved)
		} else {
			setSelectedView('claude:fiveHour')
		}
	}, [])

	const selectView = (view: string) => {
		setSelectedView(view)
		localStorage.setItem('caw:quota:selected_view', view)
	}

	const hasClaude = settings.claude?.installed !== 'false' && !(quotas && quotas.claude?.error)
	const hasCodex = settings.codex?.installed !== 'false' && !(quotas && quotas.codex?.error)
	const hasCopilot = !!settings.copilot?.token
	const hasAntigravity = settings.antigravity?.installed !== 'false'
	const hasOpenCode = !!(settings.opencode?.cookie && settings.opencode?.workspaceId)
	const hasOllama = !!settings.ollama?.cookie
	const hasOpenRouter = !!settings.openrouter?.apiKey
	const isConfigured = hasClaude || hasCodex || hasCopilot || hasAntigravity || hasOpenCode || hasOllama || hasOpenRouter

	const getQuotaDisplay = () => {
		if (!isConfigured) {
			return { text: 'Configure Limits', isError: false }
		}
		if (isLoading && !quotas) {
			return { text: 'Loading Limits...', isError: false }
		}
		if (!quotas) {
			return { text: 'No Data', isError: false }
		}

		const parts = selectedView.split(':')
		const provider = parts[0]
		const providerData = quotas[provider as keyof AllQuotas]

		const providerLabel =
			provider === 'claude' ? 'Claude' :
			provider === 'codex' ? 'Codex' :
			provider === 'copilot' ? 'Copilot' :
			provider === 'antigravity' ? 'Antigravity' :
			provider === 'opencode' ? 'OpenCode Go' :
			provider === 'openrouter' ? 'OpenRouter' : 'Ollama'

		if (!providerData) {
			return { text: 'Select Limit', isError: false }
		}

		if (providerData.error) {
			return { text: 'Select Limit', isError: false }
		}

		if (!providerData.data) {
			return { text: `${providerLabel}: Loading`, isError: false }
		}

		// Check for dynamic group selection: provider:groupName:itemName
		if (providerData.data.groups && parts.length === 3) {
			const groupName = parts[1]
			const itemName = parts[2]
			const group = providerData.data.groups.find(g => g.name === groupName)
			const item = group?.items.find(i => i.name === itemName)
			if (item) {
				const display = formatQuotaValue(item.used, item.limit, item.unit)
				return { text: `${providerLabel} (${item.label}): ${display.text}`, isError: false, percentage: display.percentage }
			}
		}

		const type = parts[1]
		const limitLabels: Record<string, Record<string, string>> = {
			claude: {
				fiveHour: 'Session Limit',
				weekly: 'Weekly Limit',
				monthly: 'Monthly Limit',
			},
			codex: {
				fiveHour: '5h Limit',
				weekly: 'Weekly Limit',
			},
			copilot: {
				fiveHour: 'Premium Interactions',
				weekly: 'Chat Limit',
			},
			antigravity: {
				fiveHour: '5h Rolling Limit',
				weekly: 'Weekly Limit',
				monthly: 'Monthly Limit',
			},
			opencode: {
				fiveHour: '5h Rolling Limit',
				weekly: 'Weekly Limit',
				monthly: 'Monthly Limit',
			},
			ollama: {
				fiveHour: 'Session Limit',
				weekly: 'Weekly Limit',
			},
			openrouter: {
				fiveHour: 'Daily Usage',
				weekly: 'Weekly Usage',
				monthly: 'Monthly Usage',
			},
		}
		const labelMap = limitLabels[provider]
		const typeLabel = (labelMap && labelMap[type]) || (type === 'fiveHour' ? '5h' : type === 'weekly' ? 'Wk' : 'Mo')
		const q = providerData.data[type as 'fiveHour' | 'weekly' | 'monthly']
		if (!q) {
			return { text: 'Select Limit', isError: false }
		}

		const display = formatQuotaValue(q.used, q.limit, q.unit)
		return { text: `${providerLabel} (${typeLabel}): ${display.text}`, isError: false, percentage: display.percentage }
	}

	const activeDisplay = getQuotaDisplay()

	const renderProgressBar = (used: number, limit: number, unit?: string, resetTime?: string) => {
		const isInfo = unit === 'info'
		if (isInfo) {
			return null
		}
		const display = formatQuotaValue(used, limit, unit)
		const pct = display.percentage
		return (
			<div className="flex flex-col gap-1 w-full mt-1.5 animate-none">
				<div className="flex items-center justify-between text-[10px] text-muted-foreground select-none font-sans">
					<span>{display.text}</span>
					{resetTime && (
						<span className="text-[9px] text-muted-foreground/70">reset in {formatResetTime(resetTime)}</span>
					)}
				</div>
				<div className="w-full bg-secondary h-1.5 rounded-full overflow-hidden">
					<div
						className={cn(
							"h-full rounded-full transition-all duration-300",
							pct >= 90 ? "bg-red-500" : pct >= 70 ? "bg-amber-500" : "bg-emerald-500"
						)}
						style={{ width: `${Math.min(100, pct)}%` }}
					/>
				</div>
			</div>
		)
	}

	return (
		<div className="h-[33px] shrink-0 border-t border-border bg-secondary/20 px-4 flex items-center justify-between text-xs text-muted-foreground select-none font-sans">
		<div className="flex items-center gap-2">
			{!hideControlCenter && (
				<>
					<Tooltip delayDuration={0}>
						<TooltipTrigger asChild>
							<button
								onClick={onToggleAgentBoard}
								className={cn(
									"shrink-0 transition-colors cursor-pointer",
									agentBoardOpen ? "text-primary" : "text-muted-foreground hover:text-foreground"
								)}
							>
								<SquareKanban className="h-3.5 w-3.5" />
							</button>
						</TooltipTrigger>
						<TooltipContent side="top" className="select-none">
							Control Center
						</TooltipContent>
					</Tooltip>
					<span className="h-4 w-px bg-border shrink-0" />
				</>
			)}
			{workspaceName ? (
				<Folder className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
			) : (
				<span className="h-2 w-2 rounded-full bg-emerald-500 shrink-0" />
			)}
			<span className="text-[11px] font-sans text-muted-foreground">
				{workspaceName || 'Ready'}
			</span>
			{worktreeBranch && (
				<>
					<span className="text-border select-none">·</span>
					<span className="flex items-center gap-1 text-muted-foreground">
						<Workflow className="h-3 w-3 shrink-0 text-violet-400" />
						<span className="text-[11px] font-sans">{worktreeBranch}</span>
					</span>
				</>
			)}
		</div>

			<DropdownMenu>
				<DropdownMenuTrigger asChild>
				<button className="flex items-center gap-1.5 px-2 py-1 rounded hover:bg-accent/40 hover:text-foreground transition-all cursor-pointer">
					{isLoading ? (
						<Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground shrink-0" />
					) : (
						<div className={cn(
							"h-1.5 w-1.5 rounded-full",
							activeDisplay.text === 'Configure Limits' 
								? 'bg-amber-400' 
								: activeDisplay.percentage !== undefined
									? activeDisplay.percentage >= 90 ? 'bg-red-500' : activeDisplay.percentage >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
									: 'bg-muted-foreground'
						)} />
					)}
					{isConfigured && !isLoading && (
						<>
							{selectedView.startsWith('claude') ? (
								<Claude.Color className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('codex') ? (
								<Codex.Color className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('copilot') ? (
								<GithubCopilot className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('antigravity') ? (
								<Antigravity.Color className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('opencode') ? (
								<OpenCode className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('ollama') ? (
								<Ollama className="h-3.5 w-3.5 shrink-0" />
							) : selectedView.startsWith('openrouter') ? (
								<OpenRouter className="h-3.5 w-3.5 shrink-0" />
							) : null}
						</>
					)}
						<span className="text-[11px] shrink-0 font-sans">{activeDisplay.text}</span>
						<ChevronUp className="h-3 w-3 opacity-60 shrink-0" />
					</button>
				</DropdownMenuTrigger>

				<DropdownMenuContent align="end" side="top" sideOffset={6} className="w-[320px] p-2 flex flex-col bg-popover border border-border rounded-lg shadow-lg select-none font-sans">
					<div className="flex items-center justify-between px-2 py-1.5">
						<span className="text-xs font-semibold text-foreground">Usage limits</span>
						<button
							onClick={(e) => {
								e.stopPropagation()
								refreshAll()
							}}
							disabled={isLoading}
							className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-accent/30 disabled:opacity-50 transition-all cursor-pointer"
							title="Refresh Limits"
						>
							<RefreshCw className={cn("h-3 w-3", isLoading && "animate-spin")} />
						</button>
					</div>

					<DropdownMenuSeparator className="bg-border" />

					<div className="flex flex-col gap-3 py-1.5 max-h-[220px] overflow-y-auto pr-1 thin-scroll" style={{ scrollbarWidth: 'thin' }}>
						{!isConfigured ? (
							<div className="px-2 py-4 text-center text-xs text-muted-foreground">
								No providers configured.
								<button
									onClick={() => {
										onOpenSettings('limits')
									}}
									className="mt-2 block w-full px-2 py-1.5 text-center text-xs font-medium text-primary bg-secondary/40 border border-border rounded hover:bg-secondary transition-all"
								>
									Configure Providers in Settings
								</button>
							</div>
						) : (
							<>
								{hasClaude && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<Claude.Color className="h-3.5 w-3.5 shrink-0" />
											Claude
										</span>
										{!quotas?.claude?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: 'Session Limit' },
													{ key: 'weekly', label: 'Weekly Limit' },
													{ key: 'monthly', label: 'Monthly Limit' }
												].map(({ key, label }) => {
													const val = quotas.claude!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `claude:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
											</div>
										)}
									</div>
								)}

								{hasClaude && hasCodex && <DropdownMenuSeparator className="bg-border" />}

								{hasCodex && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<Codex.Color className="h-3.5 w-3.5 shrink-0" />
											Codex
										</span>
										{!quotas?.codex?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: '5h Limit' },
													{ key: 'weekly', label: 'Weekly Limit' }
												].map(({ key, label }) => {
													const val = quotas.codex!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `codex:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
											</div>
										)}
									</div>
								)}

								{(hasClaude || hasCodex) && hasCopilot && <DropdownMenuSeparator className="bg-border" />}

								{hasCopilot && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<GithubCopilot className="h-3.5 w-3.5 shrink-0" />
											Copilot
										</span>
										{!quotas?.copilot?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: 'Premium Interactions' },
													{ key: 'weekly', label: 'Chat Limit' }
												].map(({ key, label }) => {
													const val = quotas.copilot!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `copilot:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
											</div>
										)}
									</div>
								)}

								{(hasClaude || hasCodex || hasCopilot) && hasAntigravity && <DropdownMenuSeparator className="bg-border" />}

								{hasAntigravity && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<Antigravity.Color className="h-3.5 w-3.5 shrink-0" />
											Antigravity
										</span>
										{!quotas?.antigravity?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-3 pl-1.5 border-l border-border">
												{quotas.antigravity.data.groups ? (
													quotas.antigravity.data.groups.map((group) => (
														<div key={group.name} className="flex flex-col gap-1.5">
															<span className="text-[9px] font-semibold text-foreground/50 tracking-wider uppercase font-sans">
																{group.name}
															</span>
															{group.items.map((item) => {
																const viewKey = `antigravity:${group.name}:${item.name}`
																const isActive = selectedView === viewKey
																return (
																	<div
																		key={item.name}
																		onClick={() => selectView(viewKey)}
																		className={cn(
																			"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																			isActive && "bg-accent/20 border-border"
																		)}
																	>
																		<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																			<span className="text-foreground" title={item.description}>{item.label}</span>
																			{isActive && <Check className="h-3 w-3 text-primary" />}
																		</div>
																		{renderProgressBar(item.used, item.limit, item.unit, item.resetTime)}
																	</div>
																)
															})}
														</div>
													))
												) : (
													[
														{ key: 'fiveHour', label: '5h Rolling Limit' },
														{ key: 'weekly', label: 'Weekly Limit' },
														{ key: 'monthly', label: 'Monthly Limit' }
													].map(({ key, label }) => {
														const val = quotas.antigravity!.data![key as 'fiveHour' | 'weekly' | 'monthly']
														const viewKey = `antigravity:${key}`
														const isActive = selectedView === viewKey
														return (
															<div
																key={key}
																onClick={() => selectView(viewKey)}
																className={cn(
																	"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																	isActive && "bg-accent/20 border-border"
																)}
															>
																<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																	<span className="text-foreground">{label}</span>
																	{isActive && <Check className="h-3 w-3 text-primary" />}
																</div>
																{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
															</div>
														)
													})
												)}
											</div>
										)}
									</div>
								)}

								{hasAntigravity && hasOpenCode && <DropdownMenuSeparator className="bg-border" />}

								{hasOpenCode && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<OpenCode className="h-3.5 w-3.5 shrink-0" />
											OpenCode Go
										</span>
										{!quotas?.opencode?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: '5h Rolling Limit' },
													{ key: 'weekly', label: 'Weekly Limit' },
													{ key: 'monthly', label: 'Monthly Limit' }
												].map(({ key, label }) => {
													const val = quotas.opencode!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `opencode:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
											</div>
										)}
									</div>
								)}

								{(hasAntigravity || hasOpenCode) && hasOllama && <DropdownMenuSeparator className="bg-border" />}

								{hasOllama && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<Ollama className="h-3.5 w-3.5 shrink-0" />
											Ollama
										</span>
										{!quotas?.ollama?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: 'Session Limit' },
													{ key: 'weekly', label: 'Weekly Limit' }
												].map(({ key, label }) => {
													const val = quotas.ollama!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `ollama:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
											</div>
										)}
									</div>
								)}
								{(hasAntigravity || hasOpenCode || hasOllama) && hasOpenRouter && <DropdownMenuSeparator className="bg-border" />}

								{hasOpenRouter && (
									<div className="px-2 flex flex-col gap-2">
										<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
											<OpenRouter className="h-3.5 w-3.5 shrink-0" />
											OpenRouter
										</span>
										{!quotas?.openrouter?.data ? (
											<span className="text-[10px] text-muted-foreground italic font-sans">Loading or no connection...</span>
										) : (
											<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
												{[
													{ key: 'fiveHour', label: 'Daily Usage' },
													{ key: 'weekly', label: 'Weekly Usage' },
													{ key: 'monthly', label: 'Monthly Usage' }
												].map(({ key, label }) => {
													const val = quotas.openrouter!.data![key as 'fiveHour' | 'weekly' | 'monthly']
													const viewKey = `openrouter:${key}`
													const isActive = selectedView === viewKey
													return (
														<div
															key={key}
															onClick={() => selectView(viewKey)}
															className={cn(
																"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																isActive && "bg-accent/20 border-border"
															)}
														>
															<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																<span className="text-foreground">{label}</span>
																{isActive && <Check className="h-3 w-3 text-primary" />}
															</div>
															{renderProgressBar(val.used, val.limit, val.unit, val.resetTime)}
														</div>
													)
												})}
												{quotas.openrouter.data.groups?.map((group) => (
													<div key={group.name} className="flex flex-col gap-1.5">
														<span className="text-[9px] font-semibold text-foreground/50 tracking-wider uppercase font-sans">
															{group.name}
														</span>
														{group.items.map((item) => {
															const viewKey = `openrouter:${group.name}:${item.name}`
															const isActive = selectedView === viewKey
															return (
																<div
																	key={item.name}
																	onClick={() => selectView(viewKey)}
																	className={cn(
																		"flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all",
																		isActive && "bg-accent/20 border-border"
																	)}
																>
																	<div className="flex justify-between items-center text-[11px] font-medium font-sans">
																		<span className="text-foreground" title={item.description}>{item.label}</span>
																		{isActive && <Check className="h-3 w-3 text-primary" />}
																	</div>
																{renderProgressBar(item.used, item.limit, item.unit, item.resetTime)}
															</div>
															)
														})}
													</div>
												))}
											</div>
										)}
									</div>
								)}
							</>
						)}
					</div>

					{isConfigured && (
						<>
							<DropdownMenuSeparator className="bg-border" />
							<div className="px-1 py-1">
								<button
									onClick={() => {
										onOpenSettings('limits')
									}}
									className="flex items-center justify-center gap-1.5 w-full px-2 py-1.5 text-xs text-muted-foreground hover:text-foreground hover:bg-accent/30 rounded transition-all cursor-pointer font-sans"
								>
									<Key className="h-3 w-3" />
									Configure Providers...
								</button>
							</div>
						</>
					)}
				</DropdownMenuContent>
			</DropdownMenu>
		</div>
	)
}
