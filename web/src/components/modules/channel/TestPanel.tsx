'use client';

import { useMemo } from 'react';
import { CheckCircle2, FlaskConical, Loader2, OctagonAlert } from 'lucide-react';
import { Button } from '@/components/ui/button';
import {
    useCancelChannelTest,
    useChannelTestStatus,
    useStartChannelTest,
} from '@/api/endpoints/channel';
import { toast } from '@/components/common/Toast';
import { cn } from '@/lib/utils';

/**
 * TestPanel renders the run-wide channel test controls plus an aggregate
 * progress strip. It is intentionally dense: a row of metrics + one CTA so
 * it doesn't push the channel list down.
 *
 * The progress numbers are derived from the live status payload we already
 * poll for (see useChannelTestStatus) so we don't duplicate any state.
 */
export function TestPanel() {
    const { data: status } = useChannelTestStatus();
    const startMutation = useStartChannelTest();
    const cancelMutation = useCancelChannelTest();

    const summary = useMemo(() => {
        const channels = status?.channels ? Object.values(status.channels) : [];
        const finished = channels.filter((c) => c.finished_at);
        const totalModels = finished.reduce((acc, c) => acc + c.total_models, 0);
        const workedModels = finished.reduce((acc, c) => acc + c.worked_models, 0);
        const totalKeys = finished.reduce((acc, c) => acc + c.total_keys, 0);
        const workedKeys = finished.reduce((acc, c) => acc + c.worked_keys, 0);
        const channelsWithFailures = finished.filter(
            (c) => c.total_models > 0 && c.worked_models < c.total_models,
        ).length;
        return {
            channelsTested: finished.length,
            totalModels,
            workedModels,
            totalKeys,
            workedKeys,
            channelsWithFailures,
        };
    }, [status]);

    const running = status?.running ?? false;
    const startedAt = status?.started_at ? new Date(status.started_at) : null;
    const finishedAt = status?.finished_at ? new Date(status.finished_at) : null;

    const handleStart = () => {
        startMutation.mutate(undefined, {
            onSuccess: () => {
                toast.success('Channel test started');
            },
            onError: (err) => {
                const msg =
                    err && typeof err === 'object' && 'message' in err && typeof err.message === 'string'
                        ? err.message
                        : 'Failed to start channel test';
                toast.error(msg);
            },
        });
    };

    const handleCancel = () => {
        cancelMutation.mutate(undefined, {
            onSuccess: () => {
                toast.info('Channel test cancelled');
            },
        });
    };

    const hasResults = summary.channelsTested > 0;
    const everCompleted = !!finishedAt && !running;

    return (
        <section className="mb-4 rounded-2xl border border-border/70 bg-card text-card-foreground p-3 sm:p-4">
            <header className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                    <FlaskConical className="size-4 text-primary shrink-0" />
                    <h3 className="text-sm font-semibold truncate">Channel diagnostics</h3>
                    {running && (
                        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                            <Loader2 className="size-3 animate-spin" />
                            Running…
                        </span>
                    )}
                </div>
                <div className="flex items-center gap-2">
                    {running ? (
                        <Button
                            type="button"
                            size="sm"
                            variant="secondary"
                            onClick={handleCancel}
                            disabled={cancelMutation.isPending}
                            className="rounded-xl h-8"
                        >
                            Cancel
                        </Button>
                    ) : (
                        <Button
                            type="button"
                            size="sm"
                            onClick={handleStart}
                            disabled={startMutation.isPending}
                            className="rounded-xl h-8"
                        >
                            <FlaskConical className="size-3.5" />
                            Test all channels
                        </Button>
                    )}
                </div>
            </header>

            <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <Metric
                    icon={<CheckCircle2 className="size-3.5 text-emerald-500" />}
                    label="Models OK"
                    primary={summary.workedModels}
                    secondary={summary.totalModels}
                    state={metricState(summary.workedModels, summary.totalModels)}
                />
                <Metric
                    icon={<CheckCircle2 className="size-3.5 text-emerald-500" />}
                    label="API keys OK"
                    primary={summary.workedKeys}
                    secondary={summary.totalKeys}
                    state={metricState(summary.workedKeys, summary.totalKeys)}
                />
                <Metric
                    icon={<OctagonAlert className="size-3.5 text-orange-500" />}
                    label="Channels with failures"
                    primary={summary.channelsWithFailures}
                    secondary={summary.channelsTested}
                    state={summary.channelsWithFailures > 0 ? 'warn' : 'ok'}
                />
                <Metric
                    icon={running
                        ? <Loader2 className="size-3.5 animate-spin text-primary" />
                        : <CheckCircle2 className="size-3.5 text-primary" />}
                    label="Channels tested"
                    primary={status?.completed_channels ?? 0}
                    secondary={status?.total_channels ?? 0}
                    state="neutral"
                />
            </dl>

            {hasResults && (
                <p className="mt-3 text-[11px] text-muted-foreground">
                    {everCompleted ? (
                        <>Last run finished {finishedAt!.toLocaleString()} · started {startedAt?.toLocaleString()}</>
                    ) : (
                        <>Started {startedAt?.toLocaleString() ?? 'just now'}</>
                    )}
                </p>
            )}

            {!hasResults && !running && (
                <p className="mt-3 text-[11px] text-muted-foreground">
                    Click <strong>Test all channels</strong> to send a 1-token probe through every model on every
                    enabled key. Each card will then show how many models/keys responded successfully and the
                    upstream failure reason for the rest.
                </p>
            )}
        </section>
    );
}

type MetricState = 'ok' | 'warn' | 'fail' | 'neutral';

function metricState(worked: number, total: number): MetricState {
    if (total === 0) return 'neutral';
    if (worked === total) return 'ok';
    if (worked === 0) return 'fail';
    return 'warn';
}

function Metric({
    icon,
    label,
    primary,
    secondary,
    state,
}: {
    icon: React.ReactNode;
    label: string;
    primary: number;
    secondary: number;
    state: MetricState;
}) {
    return (
        <div className="rounded-xl border border-border/60 bg-background/70 px-3 py-2 min-w-0">
            <dt className="mb-1 flex items-center gap-1.5 text-[11px] text-muted-foreground truncate">
                {icon}
                {label}
            </dt>
            <dd className={cn('flex items-baseline gap-1 text-base font-semibold', stateClass(state))}>
                <span>{primary}</span>
                <span className="text-xs text-muted-foreground">/ {secondary}</span>
            </dd>
        </div>
    );
}

function stateClass(state: MetricState): string {
    switch (state) {
        case 'ok':
            return 'text-emerald-600 dark:text-emerald-400';
        case 'warn':
            return 'text-orange-600 dark:text-orange-400';
        case 'fail':
            return 'text-red-600 dark:text-red-400';
        default:
            return 'text-foreground';
    }
}
