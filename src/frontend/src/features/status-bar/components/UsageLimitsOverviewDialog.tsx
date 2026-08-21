import { useState, useEffect, useCallback, useMemo, type ElementType } from 'react'
import { Dialog, DialogContent, DialogTitle, DialogClose } from '@/components/dialog'
import { ChevronDown, Layers, X } from 'lucide-react'
import { CommandCodeIcon } from '@/features/agents/components/CommandCodeIcon'
import { ZedIcon } from '@/features/agents/components/ZedIcon'
import { ClaudeIcon } from '@/features/agents/components/ClaudeIcon'
import { CodexIcon } from '@/features/agents/components/CodexIcon'
import { GithubCopilotIcon } from '@/features/agents/components/GithubCopilotIcon'
import { AntigravityIcon } from '@/features/agents/components/AntigravityIcon'
import { OpenCodeIcon } from '@/features/agents/components/OpenCodeIcon'
import { OllamaIcon } from '@/features/agents/components/OllamaIcon'
import { OpenRouterIcon } from '@/features/agents/components/OpenRouterIcon'
import { cn } from '@/features/shared/utils/utils'
import { getDisabledProviders, subscribePrefs } from '@/features/prefs/stores/prefsStore'
import {
	formatQuotaValue,
	formatResetTime,
	getQuotaMetricEntries,
	normalizeProviderQuota,
	normalizeQuotaSettingsPayload,
	providerHasConfiguredAccount,
	QUOTA_PROVIDER_IDS,
	QUOTA_PROVIDER_LABELS,
	type NormalizedProviderQuota,
	type QuotaGroup,
	type QuotaItem,
	type QuotaProviderId,
} from '@/features/shared/utils/quotaLimits'

const PROVIDER_ICONS: Record<QuotaProviderId, ElementType> = {
	claude: ClaudeIcon,
	codex: CodexIcon,
	copilot: GithubCopilotIcon,
	antigravity: AntigravityIcon,
	opencode: OpenCodeIcon,
	ollama: OllamaIcon,
	openrouter: OpenRouterIcon,
	commandcode: CommandCodeIcon,
	zed: ZedIcon,
}

interface UsageLimitsOverviewDialogProps {
	open: boolean
	onOpenChange: (open: boolean) => void
}

function renderProviderIcon(providerId: QuotaProviderId) {
	const Icon = PROVIDER_ICONS[providerId]
	return <Icon className="h-4 w-4 shrink-0" />
}

export function UsageLimitsOverviewDialog({ open, onOpenChange }: UsageLimitsOverviewDialogProps) {
	const [quotas, setQuotas] = useState<Record<string, unknown> | null>(null)
	const [settingsPayload, setSettingsPayload] = useState<unknown>({})
	const [disabledProviders, setDisabledProviders] = useState<string[]>(getDisabledProviders())
	const [isLoading, setIsLoading] = useState(false)
	const [expandedCards, setExpandedCards] = useState<Record<string, boolean>>({})

	const settings = useMemo(() => normalizeQuotaSettingsPayload(settingsPayload).providers, [settingsPayload])
	const normalizedQuotas = useMemo(() => {
		const result = {} as Record<QuotaProviderId, NormalizedProviderQuota>
		for (const providerId of QUOTA_PROVIDER_IDS) {
			result[providerId] = normalizeProviderQuota(providerId, quotas?.[providerId], settings[providerId])
		}
		return result
	}, [quotas, settings])

	const activeProviders = useMemo(() => {
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

	const fetchSettings = useCallback(async () => {
		try {
			const res = await fetch('/api/quotas/settings')
			if (res.ok) {
				const json = await res.json()
				setSettingsPayload(json?.data || {})
			}
		} catch (e) {
			console.error('Error fetching settings in overview', e)
		}
	}, [])

	const fetchQuotas = useCallback(async () => {
		setIsLoading(true)
		try {
			const res = await fetch('/api/quotas')
			if (res.ok) {
				const json = await res.json()
				setQuotas(json?.data)
			}
		} catch (e) {
			console.error('Error fetching quotas in overview', e)
		} finally {
			setIsLoading(false)
		}
	}, [])

	const refreshAll = useCallback(async () => {
		await fetchSettings()
		await fetchQuotas()
	}, [fetchSettings, fetchQuotas])

	useEffect(() => {
		if (open) {
			refreshAll()
		}
	}, [open, refreshAll])

	useEffect(() => {
		return subscribePrefs(() => setDisabledProviders(getDisabledProviders()))
	}, [])

	const toggleExpand = (cardKey: string) => {
		setExpandedCards((prev) => ({
			...prev,
			[cardKey]: !prev[cardKey],
		}))
	}

	const renderProgressBar = (used: number, limit: number, unit?: string, resetTime?: string) => {
		if (unit === 'info') return null
		const display = formatQuotaValue(used, limit, unit)
		const pct = display.percentage
		return (
			<div className="flex flex-col gap-1 w-full mt-1 font-sans">
				<div className="flex items-center justify-between text-[11px] text-muted-foreground select-none">
					<span className="font-mono text-[10px]">{display.text}</span>
					{resetTime && (
						<span className="text-[10px] text-muted-foreground/75">
							reset in {formatResetTime(resetTime)}
						</span>
					)}
				</div>
				<div className="w-full bg-secondary/80 h-1.5 rounded-full overflow-hidden">
					<div
						className={cn(
							'h-full rounded-full transition-all duration-300',
							pct >= 90 ? 'bg-red-500' : pct >= 70 ? 'bg-amber-500' : 'bg-emerald-500',
						)}
						style={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
					/>
				</div>
			</div>
		)
	}

	const renderGroupItems = (groups: QuotaGroup[]) => {
		return (
			<div className="flex flex-col gap-3 pt-2.5 mt-2 border-t border-border/40">
				{groups.map((group) => (
					<div key={group.name} className="flex flex-col gap-2">
						<span className="text-[9px] font-semibold text-muted-foreground uppercase tracking-wider">
							{group.name}
						</span>
						<div className="flex flex-col gap-2 pl-2 border-l border-border/60">
							{group.items.map((item: QuotaItem) => (
								<div key={item.name} className="flex flex-col gap-0.5">
									<div className="flex justify-between items-center text-[11px] font-medium">
										<span className="text-foreground" title={item.description}>
											{item.label}
										</span>
									</div>
									{renderProgressBar(item.used, item.limit, item.unit, item.resetTime)}
								</div>
							))}
						</div>
					</div>
				))}
			</div>
		)
	}

	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent
				hideClose
				data-testid="usage-limits-overview-dialog"
				className="w-full max-w-2xl max-h-[85vh] p-0 flex flex-col overflow-hidden bg-background border border-border shadow-2xl rounded-xl font-sans select-none"
			>
				<div className="flex items-center justify-between px-6 py-4 border-b border-border/80 bg-secondary/15">
					<DialogTitle className="text-sm font-semibold text-foreground tracking-tight">
						Limits Overview
					</DialogTitle>
					<DialogClose className="p-1 -mr-1 text-muted-foreground hover:text-white transition-colors cursor-pointer outline-none">
						<X className="h-4 w-4" />
						<span className="sr-only">Close</span>
					</DialogClose>
				</div>

				<div className="p-6 overflow-y-auto thin-scroll flex flex-col gap-4 flex-1" style={{ scrollbarWidth: 'thin' }}>
					{isLoading && !quotas ? (
						<div className="py-12 flex flex-col items-center justify-center text-center gap-2 text-muted-foreground">
							<span className="text-xs">Loading limits…</span>
						</div>
					) : activeProviders.length === 0 ? (
						<div className="py-12 flex flex-col items-center justify-center text-center gap-3 text-muted-foreground">
							<div className="p-3 rounded-full bg-secondary/50 text-muted-foreground">
								<Layers className="h-6 w-6" />
							</div>
							<div className="flex flex-col gap-1">
								<p className="text-sm font-medium text-foreground">No active providers</p>
								<p className="text-xs text-muted-foreground max-w-xs">
									Configured AI providers and their live usage limits will be displayed here.
								</p>
							</div>
						</div>
					) : (
						<div className="grid grid-cols-1 md:grid-cols-2 gap-3.5">
							{activeProviders.map((providerId) => {
								const providerQuota = normalizedQuotas[providerId]
								const accounts = providerQuota.accounts.filter((account) => Boolean(account.data))
								const showAccountNames = accounts.length > 1

								return accounts.map((account, accIdx) => {
									const cardKey = `${providerId}-${account.id || accIdx}`
									const metrics = getQuotaMetricEntries(providerId, account.data)
									const groups = account.data?.groups || []
									const totalGroupItems = groups.reduce((acc, g) => acc + g.items.length, 0)
									const isExpanded = Boolean(expandedCards[cardKey])

									return (
										<div
											key={cardKey}
											data-testid={`overview-card-${cardKey}`}
											className="border border-border/80 bg-secondary/15 rounded-xl p-4 flex flex-col gap-3 shadow-xs hover:border-border transition-colors"
										>
											<div className="flex items-center justify-between">
												<div className="flex items-center gap-2.5">
													<div className="p-1.5 rounded-lg bg-background border border-border/60 flex items-center justify-center">
														{renderProviderIcon(providerId)}
													</div>
													<div className="flex flex-col">
														<span className="text-xs font-semibold text-foreground leading-tight">
															{showAccountNames ? account.name : QUOTA_PROVIDER_LABELS[providerId]}
														</span>
														{showAccountNames && (
															<span className="text-[10px] text-muted-foreground leading-none mt-0.5">
																{QUOTA_PROVIDER_LABELS[providerId]}
															</span>
														)}
													</div>
												</div>
											</div>

											<div className="flex flex-col gap-2.5">
												{metrics.map(({ key, label, quota }) => (
													<div key={key} className="flex flex-col gap-0.5">
														<span className="text-[11px] font-medium text-foreground/90">
															{label}
														</span>
														{renderProgressBar(quota.used, quota.limit, quota.unit, quota.resetTime)}
													</div>
												))}
											</div>

											{groups.length > 0 && totalGroupItems > 0 && (
												<div className="flex flex-col">
													<button
														type="button"
														onClick={() => toggleExpand(cardKey)}
														className="flex items-center justify-between w-full pt-2 mt-1 border-t border-border/40 text-[11px] font-medium text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
													>
														<span>{isExpanded ? 'Hide model limits' : `Show model limits (${totalGroupItems})`}</span>
														<ChevronDown
															className={cn('h-3.5 w-3.5 transition-transform duration-200', isExpanded && 'rotate-180')}
														/>
													</button>
													{isExpanded && renderGroupItems(groups)}
												</div>
											)}
										</div>
									)
								})
							})}
						</div>
					)}
				</div>
			</DialogContent>
		</Dialog>
	)
}
