import { useState, useEffect, useCallback } from 'react'
import {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { RefreshCw, Key, AlertCircle, Check, Loader2, ChevronUp, Workflow } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, ClaudeCode, Codex, GithubCopilot } from '@lobehub/icons'
import { cn } from '@/lib/utils'

interface Quota {
	used:  number
	limit: number
}

interface QuotaItem {
	name:        string
	label:       string
	description: string
	used:        number
	limit:       number
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
}

interface StatusBarProps {
	workspaceName?: string
	worktreeBranch?: string
	onOpenSettings: () => void
}

export function StatusBar({ workspaceName, worktreeBranch, onOpenSettings }: StatusBarProps) {
	const [quotas, setQuotas] = useState<AllQuotas | null>(null)
	const [settings, setSettings] = useState<Record<string, Record<string, string>>>({})
	const [isLoading, setIsLoading] = useState(false)
	const [selectedView, setSelectedView] = useState<string>('')

	const fetchSettings = useCallback(async () => {
		try {
			const res = await fetch('/api/quotas/settings')
			if (res.ok) {
				const data = await res.json()
				setSettings(data || {})
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
				const data = await res.json()
				setQuotas(data)
			} else {
				console.error('Failed to fetch quotas', res.statusText)
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

	const hasClaude = settings.claude?.installed !== 'false'
	const hasCodex = settings.codex?.installed !== 'false'
	const hasCopilot = !!settings.copilot?.token
	const hasAntigravity = settings.antigravity?.installed !== 'false'
	const hasOpenCode = !!(settings.opencode?.cookie && settings.opencode?.workspaceId)
	const hasOllama = !!settings.ollama?.cookie
	const isConfigured = hasClaude || hasCodex || hasCopilot || hasAntigravity || hasOpenCode || hasOllama

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
			provider === 'opencode' ? 'OpenCode Go' : 'Ollama'

		if (!providerData) {
			return { text: 'Select Limit', isError: false }
		}

		if (providerData.error) {
			return { text: `${providerLabel}: Error`, isError: true }
		}

		if (!providerData.data) {
			return { text: `${providerLabel}: Loading`, isError: false }
		}

		// Check for dynamic group selection: provider:groupName:itemName
		if (provider === 'antigravity' && providerData.data.groups && parts.length === 3) {
			const groupName = parts[1]
			const itemName = parts[2]
			const group = providerData.data.groups.find(g => g.name === groupName)
			const item = group?.items.find(i => i.name === itemName)
			if (item) {
				return { text: `${providerLabel} (${item.label}): ${item.used}%`, isError: false, percentage: item.used }
			}
		}

		const type = parts[1]
		const typeLabel = type === 'fiveHour' ? '5h' : type === 'weekly' ? 'Wk' : 'Mo'
		const q = providerData.data[type as 'fiveHour' | 'weekly' | 'monthly']
		if (!q) {
			return { text: 'Select Limit', isError: false }
		}

		if (provider === 'opencode' || provider === 'ollama' || provider === 'antigravity' || provider === 'claude' || provider === 'codex' || provider === 'copilot') {
			return { text: `${providerLabel} (${typeLabel}): ${q.used}%`, isError: false, percentage: q.used }
		}

		const pct = q.limit > 0 ? Math.round((q.used / q.limit) * 100) : 0
		return { text: `${providerLabel} (${typeLabel}): ${q.used}/${q.limit} (${pct}%)`, isError: false, percentage: pct }
	}

	const activeDisplay = getQuotaDisplay()

	const renderProgressBar = (used: number, limit: number, isPercentageOnly: boolean) => {
		const pct = isPercentageOnly ? used : limit > 0 ? (used / limit) * 100 : 0
		return (
			<div className="flex flex-col gap-1 w-full mt-1.5 animate-none">
				<div className="flex justify-between text-[10px] text-muted-foreground select-none font-sans">
					<span>{isPercentageOnly ? `${used}%` : `${used} / ${limit}`}</span>
					<span>{Math.round(pct)}%</span>
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
				<div className="h-1.5 w-1.5 rounded-full bg-emerald-500" />
				<span className="font-medium text-foreground/80">
					{workspaceName ? `Workspace: ${workspaceName}` : 'Ready'}
				</span>
				{worktreeBranch && (
					<>
						<span className="text-border select-none">·</span>
						<span className="flex items-center gap-1 text-foreground/70">
							<Workflow className="h-3 w-3 shrink-0 text-violet-400" />
							<span className="font-mono text-[11px]">{worktreeBranch}</span>
						</span>
					</>
				)}
			</div>

			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<button className="flex items-center gap-1.5 px-2 py-1 rounded hover:bg-accent/40 hover:text-foreground transition-all cursor-pointer">
						{activeDisplay.isError ? (
							<AlertCircle className="h-3.5 w-3.5 text-red-400 shrink-0" />
						) : isLoading ? (
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
						{isConfigured && !isLoading && !activeDisplay.isError && (
							<>
								{selectedView.startsWith('claude') ? (
									<ClaudeCode className="h-3.5 w-3.5 shrink-0" />
								) : selectedView.startsWith('codex') ? (
									<Codex className="h-3.5 w-3.5 shrink-0" />
								) : selectedView.startsWith('copilot') ? (
									<GithubCopilot className="h-3.5 w-3.5 shrink-0" />
								) : selectedView.startsWith('antigravity') ? (
									<Antigravity className="h-3.5 w-3.5 shrink-0" />
								) : selectedView.startsWith('opencode') ? (
									<OpenCode className="h-3.5 w-3.5 shrink-0" />
								) : selectedView.startsWith('ollama') ? (
									<Ollama className="h-3.5 w-3.5 shrink-0" />
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

					<div className="flex flex-col gap-3 py-1.5 max-h-[220px] overflow-y-auto pr-1" style={{ scrollbarWidth: 'thin' }}>
						{!isConfigured ? (
							<div className="px-2 py-4 text-center text-xs text-muted-foreground">
								No providers configured.
								<button
									onClick={() => {
										onOpenSettings()
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
											<ClaudeCode className="h-3.5 w-3.5 shrink-0" />
											Claude
										</span>
										{quotas?.claude?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.claude.error}</span>
										) : !quotas?.claude?.data ? (
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
															{renderProgressBar(val.used, 100, true)}
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
											<Codex className="h-3.5 w-3.5 shrink-0" />
											Codex
										</span>
										{quotas?.codex?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.codex.error}</span>
										) : !quotas?.codex?.data ? (
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
															{renderProgressBar(val.used, 100, true)}
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
										{quotas?.copilot?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.copilot.error}</span>
										) : !quotas?.copilot?.data ? (
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
															{renderProgressBar(val.used, 100, true)}
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
											<Antigravity className="h-3.5 w-3.5 shrink-0" />
											Antigravity
										</span>
										{quotas?.antigravity?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.antigravity.error}</span>
										) : !quotas?.antigravity?.data ? (
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
																		{renderProgressBar(item.used, 100, true)}
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
																{renderProgressBar(val.used, 100, true)}
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
										{quotas?.opencode?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.opencode.error}</span>
										) : !quotas?.opencode?.data ? (
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
															{renderProgressBar(val.used, 100, true)}
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
										{quotas?.ollama?.error ? (
											<span className="text-[10px] text-red-400 italic font-sans">Error: {quotas.ollama.error}</span>
										) : !quotas?.ollama?.data ? (
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
															{renderProgressBar(val.used, 100, true)}
														</div>
													)
												})}
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
										onOpenSettings()
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
