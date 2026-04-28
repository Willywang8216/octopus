'use client';

import { Loader2 } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useChannelTestStatus, type ChannelTestChannelSummary } from '@/api/endpoints/channel';

/**
 * TestBadge renders the per-channel "models OK / total · keys OK / total"
 * pill that lives in the channel card header and detail dialog. It pulls
 * data from the run-status query so it stays in sync with the global poller
 * without each card spawning its own request.
 */
export function TestBadge({ channelID, size = 'sm' }: { channelID: number; size?: 'sm' | 'md' }) {
    const { data: status } = useChannelTestStatus();
    const summary = status?.channels?.[String(channelID)];

    if (!summary) {
        return null;
    }

    return (
        <span
            className={cn(
                'inline-flex items-center gap-1 rounded-full border whitespace-nowrap',
                size === 'sm' ? 'px-1.5 py-0.5 text-[10px]' : 'px-2 py-0.5 text-xs',
                pillToneClass(summary),
            )}
            title={tooltipFor(summary)}
        >
            {!summary.finished_at && status?.running ? (
                <Loader2 className="size-3 animate-spin" />
            ) : null}
            <span className="font-semibold">
                {summary.worked_models}
                <span className="opacity-60">/{summary.total_models}</span>
            </span>
            <span className="opacity-60">models</span>
            <span className="font-semibold">
                {summary.worked_keys}
                <span className="opacity-60">/{summary.total_keys}</span>
            </span>
            <span className="opacity-60">keys</span>
        </span>
    );
}

function pillToneClass(summary: ChannelTestChannelSummary): string {
    if (summary.skipped) {
        return 'border-muted-foreground/30 bg-muted/40 text-muted-foreground';
    }
    if (!summary.finished_at) {
        return 'border-primary/30 bg-primary/10 text-primary';
    }
    if (summary.total_models === 0 || summary.worked_models === 0) {
        return 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300';
    }
    if (summary.worked_models < summary.total_models || summary.worked_keys < summary.total_keys) {
        return 'border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300';
    }
    return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
}

function tooltipFor(summary: ChannelTestChannelSummary): string {
    if (summary.skipped) {
        return `Skipped: ${summary.skipped}`;
    }
    if (!summary.finished_at) {
        return 'Test in progress';
    }
    return `${summary.worked_models}/${summary.total_models} models · ${summary.worked_keys}/${summary.total_keys} keys`;
}
