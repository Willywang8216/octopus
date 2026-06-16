import { CheckCircle2, AlertTriangle, AlertOctagon, XCircle, HelpCircle } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useTranslations } from 'next-intl';
import type { ChannelHealth } from '@/api/endpoints/channel';

const ICONS: Record<ChannelHealth, React.ComponentType<{ className?: string }>> = {
    alive: CheckCircle2,
    flaky: AlertTriangle,
    zombie: AlertOctagon,
    dead: XCircle,
    unknown: HelpCircle,
};

const COLORS: Record<ChannelHealth, string> = {
    alive: 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400 border-emerald-500/20',
    flaky: 'bg-amber-500/15 text-amber-700 dark:text-amber-400 border-amber-500/20',
    zombie: 'bg-orange-500/15 text-orange-700 dark:text-orange-400 border-orange-500/20',
    dead: 'bg-red-500/15 text-red-700 dark:text-red-400 border-red-500/20',
    unknown: 'bg-muted text-muted-foreground border-border',
};

export function HealthPill({
    health,
    size = 'sm',
    className,
}: {
    health?: ChannelHealth | null;
    size?: 'sm' | 'md';
    className?: string;
}) {
    const t = useTranslations('channel.health');
    const value: ChannelHealth = health ?? 'unknown';
    const Icon = ICONS[value];
    return (
        <span
            className={cn(
                'inline-flex items-center gap-1 rounded-full border font-medium',
                size === 'sm' ? 'h-5 px-1.5 text-[10px]' : 'h-6 px-2 text-xs',
                COLORS[value],
                className
            )}
        >
            <Icon className={size === 'sm' ? 'size-3' : 'size-3.5'} />
            {t(value)}
        </span>
    );
}
