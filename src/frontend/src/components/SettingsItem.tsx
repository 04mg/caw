import React from 'react'

interface SettingsItemProps {
  icon: React.ElementType
  label: string
  onClick: () => void
}

export function SettingsItem({ icon: Icon, label, onClick }: SettingsItemProps) {
  return (
    <button
      onClick={onClick}
      className="flex items-center justify-between p-3 rounded-xl border border-border bg-card hover:bg-accent/40 cursor-pointer select-none transition-all group duration-200 text-left w-full outline-none focus:ring-1 focus:ring-ring"
    >
      <div className="flex items-center gap-3">
        <div className="h-9 w-9 rounded-lg bg-muted flex items-center justify-center shrink-0 group-hover:scale-105 transition-transform">
          <Icon className="h-5 w-5 text-foreground" />
        </div>
        <p className="text-xs font-semibold text-foreground">{label}</p>
      </div>
      <div className="text-muted-foreground group-hover:text-primary transition-colors text-xs font-semibold flex items-center gap-1.5 pr-1">
        Configure &rarr;
      </div>
    </button>
  )
}
