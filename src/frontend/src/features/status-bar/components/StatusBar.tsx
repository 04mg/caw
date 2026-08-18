import { useState, useEffect, useCallback, useMemo, type ElementType } from 'react'
import {
	DropdownMenu,
	DropdownMenuTrigger,
	DropdownMenuContent,
	DropdownMenuSeparator,
} from '@/components/dropdown-menu'
import { RefreshCw, Key, Check, Loader2, ChevronUp, Workflow, Folder, SquareKanban, Settings, Mic, SquareArrowOutUpLeft } from 'lucide-react'
import { Antigravity, OpenCode, Ollama, Claude, Codex, GithubCopilot, OpenRouter } from '@lobehub/icons'
import { CommandCodeIcon } from '@/features/agents/components/CommandCodeIcon'
import { ZedIcon } from '@/features/agents/components/ZedIcon'
import { cn } from '@/features/shared/utils/utils'
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/tooltip'
import { useVoiceMode, isVoiceSupported } from '@/features/voice-mode/hooks/useVoiceMode'
import { VoiceBubble } from '@/features/voice-mode/components/VoiceBubble'
import { getDisabledProviders, subscribePrefs } from '@/features/prefs/stores/prefsStore'
import {
	deserializeQuotaSelection,
	getQuotaMetricEntries,
	normalizeProviderQuota,
	normalizeQuotaSettingsPayload,
	providerHasConfiguredAccount,
	serializeQuotaSelection,
	QUOTA_PROVIDER_IDS,
	QUOTA_PROVIDER_LABELS,
	type NormalizedProviderQuota,
	type QuotaGroup,
	type QuotaItem,
	type QuotaMetricKey,
	type QuotaProviderId,
	type QuotaSelection,
} from '@/features/shared/utils/quotaLimits'

const PROVIDER_ICONS: Record<QuotaProviderId, ElementType> = {
	claude: Claude.Color,
	codex: Codex.Color,
	copilot: GithubCopilot,
	antigravity: Antigravity.Color,
	opencode: OpenCode,
	ollama: Ollama,
	openrouter: OpenRouter,
	commandcode: CommandCodeIcon,
	zed: ZedIcon,
}

const formatQuotaValue = (used: number, limit: number, unit?: string): { text: string, percentage: number } => {
	if (unit === 'info') {
		return { text: '', percentage: 0 }
	}
	if (unit === 'percentage' || !unit) {
		return { text: `${used}%`, percentage: used }
	}
	const pct = limit > 0 ? Math.round((used / limit) * 100) : 0
	if (unit === 'currency') {
		return { text: `${used}$ / ${limit}$`, percentage: pct }
	}
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
	onOpenOverview?: () => void
	hideControlCenter?: boolean
	controlCenterButtonRef?: React.Ref<HTMLButtonElement>
	onSendText?: (text: string) => void
}

function renderProviderIcon(providerId: QuotaProviderId) {
	const Icon = PROVIDER_ICONS[providerId]
	return <Icon className="h-3.5 w-3.5 shrink-0" />
}

export function StatusBar({ workspaceName, worktreeBranch, agentBoardOpen, onToggleAgentBoard, onOpenSettings, onOpenOverview, hideControlCenter, controlCenterButtonRef, onSendText }: StatusBarProps) {
	const [quotas, setQuotas] = useState<Record<string, unknown> | null>(null)
	const [settingsPayload, setSettingsPayload] = useState<unknown>({})
	const [disabledProviders, setDisabledProviders] = useState<string[]>(getDisabledProviders())
	const [isLoading, setIsLoading] = useState(false)
	const [selectedView, setSelectedView] = useState<string>('')
	const [isMobile, setIsMobile] = useState(() => typeof window !== 'undefined' && window.innerWidth < 768)
	const voice = useVoiceMode()

	const settings = useMemo(() => normalizeQuotaSettingsPayload(settingsPayload).providers, [settingsPayload])
	const normalizedQuotas = useMemo(() => {
		const result = {} as Record<QuotaProviderId, NormalizedProviderQuota>
		for (const providerId of QUOTA_PROVIDER_IDS) {
			result[providerId] = normalizeProviderQuota(providerId, quotas?.[providerId], settings[providerId])
		}
		return result
	}, [quotas, settings])

	const visibleProviders = useMemo(() => {
		return QUOTA_PROVIDER_IDS.filter((providerId) => {
			if (disabledProviders.includes(providerId)) return false
			const quota = normalizedQuotas[providerId]
			const hasUsableData = quota.accounts.some((account) => Boolean(account.data))
			if (!hasUsableData && (quota.error || quota.accounts.some((account) => Boolean(account.error)))) {
				return false
			}
			return providerHasConfiguredAccount(providerId, settings[providerId], quota)
		})
	}, [disabledProviders, normalizedQuotas, settings])

	const availableSelections = useMemo(() => {
		const selections: string[] = []
		for (const providerId of visibleProviders) {
			for (const account of normalizedQuotas[providerId].accounts) {
				for (const metric of getQuotaMetricEntries(providerId, account.data)) {
					selections.push(serializeQuotaSelection({
						providerId,
						accountId: normalizedQuotas[providerId].accounts.length > 1 ? account.id : undefined,
						kind: 'window',
						metricKey: metric.key,
					}))
				}
				for (const group of account.data?.groups || []) {
					for (const item of group.items) {
						selections.push(serializeQuotaSelection({
							providerId,
							accountId: normalizedQuotas[providerId].accounts.length > 1 ? account.id : undefined,
							kind: 'groupItem',
							groupName: group.name,
							itemName: item.name,
						}))
					}
				}
			}
		}
		return selections
	}, [normalizedQuotas, visibleProviders])

	useEffect(() => {
		const onResize = () => setIsMobile(window.innerWidth < 768)
		window.addEventListener('resize', onResize)
		return () => window.removeEventListener('resize', onResize)
	}, [])

	const fetchSettings = useCallback(async () => {
		try {
			const res = await fetch('/api/quotas/settings')
			if (res.ok) {
				const json = await res.json()
				setSettingsPayload(json?.data || {})
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

	useEffect(() => {
		refreshAll()
		const timer = setInterval(fetchQuotas, 180000)

		window.addEventListener('caw:settings-updated', refreshAll)

		return () => {
			clearInterval(timer)
			window.removeEventListener('caw:settings-updated', refreshAll)
		}
	}, [refreshAll, fetchQuotas])

	useEffect(() => {
		return subscribePrefs(() => setDisabledProviders(getDisabledProviders()))
	}, [])

	useEffect(() => {
		const saved = localStorage.getItem('caw:quota:selected_view')
		if (saved) {
			const parsed = deserializeQuotaSelection(saved)
			setSelectedView(parsed ? serializeQuotaSelection(parsed) : saved)
		}
	}, [])

	useEffect(() => {
		if (availableSelections.length === 0) return
		if (!selectedView || !availableSelections.includes(selectedView)) {
			const next = availableSelections[0]
			setSelectedView(next)
			localStorage.setItem('caw:quota:selected_view', next)
		}
	}, [availableSelections, selectedView])

	const selectView = (view: QuotaSelection) => {
		const next = serializeQuotaSelection(view)
		setSelectedView(next)
		localStorage.setItem('caw:quota:selected_view', next)
	}

	const handleToggleVoice = () => {
		if (!isVoiceSupported()) return
		if (voice.phase === 'idle') {
			voice.start()
		} else if (voice.phase === 'listening') {
			if (isMobile) {
				voice.stop()
			} else {
				voice.stop({ send: (text) => onSendText?.(text) })
			}
		}
	}

	const handleSendVoice = () => {
		const text = voice.transcript.trim()
		if (text) onSendText?.(text)
		voice.reset()
	}

	const handleDiscardVoice = () => {
		voice.reset()
	}

	const isConfigured = visibleProviders.length > 0
	const activeSelection = deserializeQuotaSelection(selectedView)

	const getQuotaDisplay = () => {
		if (!isConfigured) {
			return { text: 'Configure Limits', isError: false }
		}
		if (isLoading && !quotas) {
			return { text: 'Loading Limits...', isError: false }
		}
		if (!quotas && availableSelections.length === 0) {
			return { text: 'No Data', isError: false }
		}
		if (!activeSelection) {
			return { text: 'Select Limit', isError: false }
		}

		const providerId = activeSelection.providerId as QuotaProviderId
		if (disabledProviders.includes(providerId)) {
			return { text: 'Select Limit', isError: false }
		}
		const providerQuota = normalizedQuotas[providerId]
		if (!providerQuota) {
			return { text: 'Select Limit', isError: false }
		}
		if (providerQuota.error && providerQuota.accounts.length === 0) {
			return { text: 'Select Limit', isError: false }
		}

		const account = providerQuota.accounts.find((entry) => entry.id === activeSelection.accountId) || providerQuota.accounts[0]
		if (!account?.data) {
			return { text: `${QUOTA_PROVIDER_LABELS[providerId]}: Loading`, isError: false }
		}

		const showAccount = providerQuota.accounts.length > 1
		const accountPrefix = showAccount ? (isMobile ? `${account.name} ` : ` · ${account.name}`) : ''

		if (activeSelection.kind === 'groupItem') {
			const group = account.data.groups?.find((entry) => entry.name === activeSelection.groupName)
			const item = group?.items.find((entry) => entry.name === activeSelection.itemName)
			if (!item) {
				return { text: 'Select Limit', isError: false }
			}
			if (item.unit === 'info') {
				const infoText = item.resetTime ? formatResetTime(item.resetTime) : item.label
				return {
					text: isMobile
						? `${QUOTA_PROVIDER_LABELS[providerId]} ${accountPrefix}${infoText}`.trim()
						: `${QUOTA_PROVIDER_LABELS[providerId]}${accountPrefix} (${item.label}): ${infoText}`,
					isError: false,
					percentage: 0,
				}
			}
			const display = formatQuotaValue(item.used, item.limit, item.unit)
			return {
				text: isMobile
					? `${QUOTA_PROVIDER_LABELS[providerId]} ${accountPrefix}${display.text}`.trim()
					: `${QUOTA_PROVIDER_LABELS[providerId]}${accountPrefix} (${item.label}): ${display.text}`,
				isError: false,
				percentage: display.percentage,
			}
		}

		const metric = account.data[activeSelection.metricKey as QuotaMetricKey]
		const label = getQuotaMetricEntries(providerId, account.data).find((entry) => entry.key === activeSelection.metricKey)?.label
		if (!metric || !label) {
			return { text: 'Select Limit', isError: false }
		}
		const display = formatQuotaValue(metric.used, metric.limit, metric.unit)
		return {
			text: isMobile
				? `${QUOTA_PROVIDER_LABELS[providerId]} ${accountPrefix}${display.text}`.trim()
				: `${QUOTA_PROVIDER_LABELS[providerId]}${accountPrefix} (${label}): ${display.text}`,
			isError: false,
			percentage: display.percentage,
		}
	}

	const activeDisplay = getQuotaDisplay()

	const renderProgressBar = (used: number, limit: number, unit?: string, resetTime?: string) => {
		if (unit === 'info') return null
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
							'h-full rounded-full transition-all duration-300',
							pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500',
						)}
						style={{ width: `${Math.min(100, pct)}%` }}
					/>
				</div>
			</div>
		)
	}

	const renderGroupItems = (providerId: QuotaProviderId, accountId: string | undefined, groups: QuotaGroup[]) => (
		groups.map((group) => (
			<div key={group.name} className="flex flex-col gap-1.5">
				<span className="text-[9px] font-semibold text-foreground/50 tracking-wider uppercase font-sans">
					{group.name}
				</span>
				{group.items.map((item: QuotaItem) => {
					const isActive = selectedView === serializeQuotaSelection({
						providerId,
						accountId,
						kind: 'groupItem',
						groupName: group.name,
						itemName: item.name,
					})
					return (
						<div
							key={item.name}
							onClick={() => selectView({ providerId, accountId, kind: 'groupItem', groupName: group.name, itemName: item.name })}
							className={cn(
								'flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all',
								isActive && 'bg-accent/20 border-border',
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
	)

	const renderProviderRows = (providerId: QuotaProviderId) => {
		const providerQuota = normalizedQuotas[providerId]
		const showAccounts = providerQuota.accounts.length > 1
		if (providerQuota.accounts.length === 0 || !providerQuota.accounts.some((account) => account.data)) {
			return <span className="text-[10px] text-muted-foreground italic font-sans">{providerQuota.error || 'Loading or no connection...'}</span>
		}

		return (
			<div className="flex flex-col gap-2.5 pl-1.5 border-l border-border">
				{providerQuota.accounts.map((account, index) => {
					if (!account.data) {
						return (
							<div key={account.id || index} className="flex flex-col gap-1">
								{showAccounts && <span className="text-[10px] font-semibold text-foreground/60 tracking-wide">{account.name}</span>}
								<span className="text-[10px] text-muted-foreground italic font-sans">{account.error || providerQuota.error || 'Loading or no connection...'}</span>
							</div>
						)
					}
					const accountId = showAccounts ? account.id : undefined
					return (
						<div key={account.id || index} className="flex flex-col gap-2.5">
							{showAccounts && (
								<span className="text-[10px] font-semibold text-foreground/60 tracking-wide">{account.name}</span>
							)}
							{getQuotaMetricEntries(providerId, account.data).map(({ key, label, quota }) => {
								const isActive = selectedView === serializeQuotaSelection({ providerId, accountId, kind: 'window', metricKey: key })
								return (
									<div
										key={key}
										onClick={() => selectView({ providerId, accountId, kind: 'window', metricKey: key })}
										className={cn(
											'flex flex-col p-1.5 rounded border border-transparent hover:border-border hover:bg-accent/10 cursor-pointer transition-all',
											isActive && 'bg-accent/20 border-border',
										)}
									>
										<div className="flex justify-between items-center text-[11px] font-medium font-sans">
											<span className="text-foreground">{label}</span>
											{isActive && <Check className="h-3 w-3 text-primary" />}
										</div>
										{renderProgressBar(quota.used, quota.limit, quota.unit, quota.resetTime)}
									</div>
								)
							})}
							{account.data.groups && account.data.groups.length > 0 && renderGroupItems(providerId, accountId, account.data.groups)}
						</div>
					)
				})}
			</div>
		)
	}

	return (
		<div className="min-h-[33px] shrink-0 border-t border-border bg-secondary/20 px-4 flex items-start md:items-center justify-between text-xs text-foreground md:text-muted-foreground select-none font-sans pb-4 md:pb-0 pt-2 md:pt-0" style={{ paddingBottom: isMobile ? 'calc(env(safe-area-inset-bottom, 0px) + 16px)' : undefined }}>
			<div className="flex items-center gap-2">
				{!hideControlCenter && (
					<>
						<Tooltip delayDuration={0}>
							<TooltipTrigger asChild>
								<button
									ref={controlCenterButtonRef}
									onClick={onToggleAgentBoard}
									data-testid="status-bar-control-center"
									className={cn(
										'shrink-0 transition-colors cursor-pointer',
										agentBoardOpen ? 'text-primary' : isMobile ? 'text-foreground' : 'text-muted-foreground hover:text-foreground',
									)}
								>
									<SquareKanban className={cn(isMobile ? 'h-5 w-5' : 'h-3.5 w-3.5')} />
								</button>
							</TooltipTrigger>
							<TooltipContent side="top" className="select-none">
								Control Center
							</TooltipContent>
						</Tooltip>
						{!isMobile && <span className="h-4 w-px bg-border shrink-0" />}
					</>
				)}
				{hideControlCenter && (
					<Tooltip delayDuration={0}>
						<TooltipTrigger asChild>
							<button
								onClick={() => onOpenSettings()}
								className={cn('shrink-0 transition-colors cursor-pointer', isMobile ? 'text-foreground' : 'text-muted-foreground hover:text-foreground')}
							>
								<Settings className={cn(isMobile ? 'h-5 w-5 ml-1' : 'h-3.5 w-3.5')} />
							</button>
						</TooltipTrigger>
						<TooltipContent side="top" className="select-none">
							Settings
						</TooltipContent>
					</Tooltip>
				)}
				{hideControlCenter && !isMobile && (
					<span className="h-4 w-px bg-border shrink-0" />
				)}
				{!isMobile && (
					<>
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
					</>
				)}
			</div>

			<div className="flex items-center gap-2">
				{isMobile && voice.phase !== 'idle' && (
					<VoiceBubble
						transcript={voice.transcript}
						error={voice.error}
						isListening={voice.phase === 'listening'}
						onSend={handleSendVoice}
						onDiscard={handleDiscardVoice}
					/>
				)}
				{!isMobile && isVoiceSupported() && (
					<>
						{voice.phase === 'listening' && (
							<span
								className="text-[11px] font-sans text-muted-foreground truncate max-w-[160px] animate-in fade-in duration-200 select-none"
								title={voice.transcript}
							>
								{voice.transcript.trim().split(/\s+/).filter(Boolean).slice(-1).join(' ') || ''}
							</span>
						)}
						<Tooltip delayDuration={0}>
							<TooltipTrigger asChild>
								<button
									onClick={handleToggleVoice}
									data-testid="status-bar-voice-mode"
									disabled={voice.phase === 'loading'}
									className={cn(
										'shrink-0 transition-colors cursor-pointer',
										voice.phase === 'listening'
											? 'text-primary'
											: voice.phase === 'loading'
												? 'text-muted-foreground cursor-wait'
												: 'text-muted-foreground hover:text-foreground',
									)}
								>
									{voice.phase === 'loading' ? (
										<Loader2 className="h-3.5 w-3.5 animate-spin" />
									) : (
										<Mic className={cn('h-3.5 w-3.5', voice.phase === 'listening' && 'lava-lamp-mic')} />
									)}
								</button>
							</TooltipTrigger>
							<TooltipContent side="top" className="select-none">
								{voice.phase === 'loading' ? 'Loading voice engine…' : 'Voice Mode'}
							</TooltipContent>
						</Tooltip>
						<span className="h-4 w-px bg-border shrink-0" />
					</>
				)}

				<DropdownMenu>
					<DropdownMenuTrigger asChild>
						<button data-testid="status-bar-quota-trigger" className={cn('flex items-center gap-1.5 py-1 rounded hover:bg-accent/40 hover:text-foreground transition-all cursor-pointer', !isMobile && 'px-2')}>
							{isLoading ? (
								<Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground shrink-0" />
							) : (
								<div className={cn(
									'h-1.5 w-1.5 rounded-full',
									activeDisplay.text === 'Configure Limits'
										? 'bg-amber-400'
										: activeDisplay.percentage !== undefined
											? activeDisplay.percentage >= 90 ? 'bg-red-500' : activeDisplay.percentage >= 70 ? 'bg-amber-500' : 'bg-emerald-500'
											: 'bg-muted-foreground',
								)} />
							)}
							{activeSelection && isConfigured && !isLoading && renderProviderIcon(activeSelection.providerId as QuotaProviderId)}
							<span className="text-[11px] shrink-0 font-sans">{activeDisplay.text}</span>
							<ChevronUp className="h-3 w-3 opacity-60 shrink-0" />
						</button>
					</DropdownMenuTrigger>

					<DropdownMenuContent align="end" side="top" sideOffset={6} className="w-[320px] p-2 flex flex-col bg-popover border border-border rounded-lg shadow-lg select-none font-sans">
						<div className="flex items-center justify-between px-2 py-1.5">
							<div className="flex items-center gap-1.5">
								<button
									onClick={(e) => {
										e.stopPropagation()
										onOpenOverview?.()
									}}
									data-testid="status-bar-quota-overview"
									className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-accent/30 transition-all cursor-pointer"
									title="Limits Overview"
								>
									<SquareArrowOutUpLeft className="h-3 w-3" />
								</button>
								<span className="text-xs font-semibold text-foreground">Usage limits</span>
							</div>
							<button
								onClick={(e) => {
									e.stopPropagation()
									refreshAll()
								}}
								disabled={isLoading}
								data-testid="status-bar-quota-refresh"
								className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-accent/30 disabled:opacity-50 transition-all cursor-pointer"
								title="Refresh Limits"
							>
								<RefreshCw className={cn('h-3 w-3', isLoading && 'animate-spin')} />
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
										data-testid="quota-configure-providers"
										className="mt-2 block w-full px-2 py-1.5 text-center text-xs font-medium text-primary bg-secondary/40 border border-border rounded hover:bg-secondary transition-all"
									>
										Configure Providers in Settings
									</button>
								</div>
							) : (
								<>
									{visibleProviders.map((providerId, index) => (
										<div key={providerId}>
											{index > 0 && <DropdownMenuSeparator className="bg-border mb-3" />}
											<div data-testid={`quota-row-${providerId}`} className="px-2 flex flex-col gap-2">
												<span className="text-[10px] font-semibold text-foreground/70 tracking-wider uppercase flex items-center gap-1.5">
													{renderProviderIcon(providerId)}
													{QUOTA_PROVIDER_LABELS[providerId]}
												</span>
												{renderProviderRows(providerId)}
											</div>
										</div>
									))}
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
										data-testid="quota-configure-providers"
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
		</div>
	)
}
