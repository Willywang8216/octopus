'use client';

import { useState } from 'react';
import { ChevronDown, ChevronUp, AlertTriangle, CheckCircle2, XCircle, Activity, Clock } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { Badge } from '@/components/ui/badge';
import {
    type Channel,
    type ChannelTestErrorClass,
    type ChannelTestSummary,
    type ChannelTestKeySummary,
    type ChannelTestModelResult,
    type ChannelTestProgress,
} from '@/api/endpoints/channel';

/**
 * AttentionTag renders a destructive "attention needed" pill for an
 * auto-disabled key/channel — e.g. insufficient quota, invalid key.
 */
export function AttentionTag({
    cls,
    reason,
    className,
}: {
    cls?: ChannelTestErrorClass;
    reason?: string;
    className?: string;
}) {
    const t = useTranslations('channel.test');
    if (!cls && !reason) return null;
    const label = t(`errorClass.${cls || 'other'}` as never);
    return (
        <Badge
            variant="secondary"
            className={cn(
                'h-5 px-1.5 text-[10px] gap-1 inline-flex items-center bg-red-500/15 text-red-700 dark:text-red-400 border border-red-500/30',
                className
            )}
            title={reason || label}
        >
            <AlertTriangle className="size-3" />
            {label}
        </Badge>
    );
}

/**
 * Map an error class to a tailwind tone for inline result rows.
 */
function classTone(cls: ChannelTestErrorClass): string {
    switch (cls) {
        case 'auth_invalid':
        case 'permission_denied':
        case 'insufficient_quota':
            return 'bg-red-500/15 text-red-700 dark:text-red-400';
        case 'rate_limited':
        case 'timeout':
        case 'server_error':
        case 'network_error':
            return 'bg-orange-500/15 text-orange-700 dark:text-orange-400';
        case 'model_not_found':
        case 'bad_request':
        case 'transform_error':
        case 'unsupported_channel':
            return 'bg-yellow-500/15 text-yellow-700 dark:text-yellow-400';
        default:
            return 'bg-muted text-muted-foreground';
    }
}

function formatTested(testedAt: number) {
    if (!testedAt) return '—';
    return new Date(testedAt * 1000).toLocaleString();
}

/**
 * KeyResultsRow renders one expandable row per channel key, with a count
 * badge and the full per-model list inside the accordion.
 */
function KeyResultsRow({ keySummary, progress }: { keySummary: ChannelTestKeySummary; progress?: ChannelTestProgress | null }) {
    const t = useTranslations('channel.test');
    const [open, setOpen] = useState(false);
    const total = keySummary.success_count + keySummary.fail_count;
    const hasFailures = keySummary.fail_count > 0;
    const isActiveKey = !!progress?.running && progress.current_key_id === keySummary.key_id;

    return (
        <div className="rounded-2xl border bg-card overflow-hidden">
            <button
                type="button"
                onClick={() => setOpen((v) => !v)}
                className="w-full flex items-center gap-3 p-3 text-left hover:bg-accent/5 transition-colors"
            >
                <div
                    className={cn(
                        'size-2 shrink-0 rounded-full',
                        keySummary.auto_disabled
                            ? 'bg-destructive'
                            : keySummary.enabled
                                ? 'bg-emerald-500'
                                : 'bg-muted-foreground'
                    )}
                />
                <span className="font-mono text-sm shrink-0">{keySummary.key_preview || `#${keySummary.key_id}`}</span>
                {keySummary.remark && (
                    <span className="text-xs text-muted-foreground truncate max-w-32" title={keySummary.remark}>
                        {keySummary.remark}
                    </span>
                )}
                <div className="ml-auto flex items-center gap-2 shrink-0">
                    {keySummary.auto_disabled && <AttentionTag cls={keySummary.disabled_class} reason={keySummary.disabled_reason} />}
                    <Badge
                        variant="secondary"
                        className={cn(
                            'h-5 px-1.5 text-[10px] gap-1 inline-flex items-center',
                            hasFailures
                                ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                : 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                        )}
                    >
                        <CheckCircle2 className="size-3" />
                        {keySummary.success_count}
                        {' / '}
                        {total}
                    </Badge>
                    {open ? <ChevronUp className="size-3.5 text-muted-foreground" /> : <ChevronDown className="size-3.5 text-muted-foreground" />}
                </div>
            </button>
            {open && (
                <div className="border-t bg-background/60 divide-y">
                    {isActiveKey && (
                        <div className="p-3 text-xs text-orange-700 dark:text-orange-300 bg-orange-500/10 flex items-center gap-2">
                            <Activity className="size-3.5 animate-pulse" />
                            {t('progress.testingKeyModel', { model: progress?.current_model || t('progress.waiting') })}
                        </div>
                    )}
                    {(keySummary.models?.length ?? 0) === 0 && !isActiveKey && (
                        <div className="p-3 text-xs text-muted-foreground text-center">{t('noModelsTested')}</div>
                    )}
                    {(keySummary.models ?? []).map((m) => (
                        <ModelResultRow key={`${keySummary.key_id}-${m.model}`} result={m} />
                    ))}
                </div>
            )}
        </div>
    );
}

function ModelResultRow({ result }: { result: ChannelTestModelResult }) {
    const t = useTranslations('channel.test');
    const [open, setOpen] = useState(false);
    const expandable = !result.success && !!result.error_msg;
    return (
        <div>
            <button
                type="button"
                onClick={() => (expandable ? setOpen((v) => !v) : undefined)}
                disabled={!expandable}
                className="w-full flex items-center gap-2 p-2.5 text-left disabled:cursor-default"
            >
                {result.success ? (
                    <CheckCircle2 className="size-4 shrink-0 text-emerald-500" />
                ) : (
                    <XCircle className="size-4 shrink-0 text-destructive" />
                )}
                <span className="font-mono text-xs truncate flex-1 min-w-0">{result.model}</span>
                {result.status_code > 0 && (
                    <Badge variant="secondary" className="h-4 px-1 text-[10px] shrink-0">
                        {result.status_code}
                    </Badge>
                )}
                {result.latency_ms > 0 && (
                    <span className="inline-flex items-center gap-1 text-[10px] text-muted-foreground shrink-0">
                        <Clock className="size-3" />
                        {result.latency_ms}ms
                    </span>
                )}
                {!result.success && result.error_class && (
                    <Badge
                        variant="secondary"
                        className={cn('h-4 px-1 text-[10px] shrink-0', classTone(result.error_class))}
                    >
                        {t(`errorClass.${result.error_class || 'other'}` as never)}
                    </Badge>
                )}
            </button>
            {open && expandable && (
                <pre className="mx-2 mb-2 max-h-40 overflow-auto rounded-md bg-muted/40 p-2 text-[10px] font-mono text-muted-foreground whitespace-pre-wrap break-all">
                    {result.error_msg}
                </pre>
            )}
        </div>
    );
}


function TestProgressPanel({ progress }: { progress?: ChannelTestProgress | null }) {
    const t = useTranslations('channel.test.progress');
    if (!progress || (!progress.running && progress.phase !== 'failed' && progress.phase !== 'done')) return null;

    const total = progress.total_probes || 0;
    const completed = progress.completed_probes || 0;
    const pct = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0;
    const isWaiting = progress.phase === 'waiting' || (progress.running && !progress.current_model && completed === 0);

    return (
        <div className="rounded-2xl border bg-card p-3 space-y-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm font-medium">
                    <Activity className={cn('size-4', progress.running ? 'animate-pulse text-orange-500' : 'text-muted-foreground')} />
                    {progress.running ? t('running') : progress.phase === 'failed' ? t('failed') : t('done')}
                </div>
                <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                    {completed}/{total || '—'}
                </Badge>
            </div>
            <div className="h-2 overflow-hidden rounded-full bg-muted">
                <div className="h-full bg-orange-500 transition-all" style={{ width: `${pct}%` }} />
            </div>
            <div className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
                <div>
                    <span className="font-medium text-card-foreground">{t('key')}:</span>{' '}
                    {isWaiting ? t('waiting') : progress.current_key || '—'}
                </div>
                <div>
                    <span className="font-medium text-card-foreground">{t('model')}:</span>{' '}
                    {isWaiting ? t('waiting') : progress.current_model || '—'}
                </div>
                <div>
                    <span className="font-medium text-card-foreground">{t('passed')}:</span> {progress.success_count}
                </div>
                <div>
                    <span className="font-medium text-card-foreground">{t('failedCount')}:</span> {progress.fail_count}
                </div>
            </div>
            {progress.last_error && (
                <div className="rounded-lg bg-destructive/10 p-2 text-xs text-destructive">{progress.last_error}</div>
            )}
        </div>
    );
}

/**
 * TestResults renders the complete probe-result section for a single
 * channel. Used inside the channel detail dialog. Pure presentational —
 * the parent owns the data fetching and the trigger button.
 */
export function TestResults({
    channel,
    summary,
    isPending,
    progress,
}: {
    channel: Channel;
    summary?: ChannelTestSummary | null;
    isPending: boolean;
    progress?: ChannelTestProgress | null;
}) {
    const t = useTranslations('channel.test');

    if (isPending && !summary) {
        return <TestProgressPanel progress={progress} />;
    }
    if (!summary || (summary.total_keys === 0 && summary.total_models === 0)) {
        if (progress?.running || progress?.phase === 'waiting') {
            return <TestProgressPanel progress={progress} />;
        }

        return (
            <div className="rounded-2xl border border-dashed bg-card/40 p-4 text-sm text-muted-foreground text-center">
                {t('emptyHint')}
            </div>
        );
    }

    const allPass = summary.fail_count === 0 && summary.success_count > 0;

    return (
        <div className="space-y-3">
            <TestProgressPanel progress={progress} />
            {/* Top summary bar */}
            <div className="flex items-center gap-2 flex-wrap">
                <Badge
                    variant="secondary"
                    className={cn(
                        'h-6 px-2 text-xs gap-1 inline-flex items-center',
                        allPass
                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                            : summary.success_count === 0
                                ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                : 'bg-orange-500/15 text-orange-700 dark:text-orange-400'
                    )}
                >
                    <Activity className="size-3" />
                    {summary.success_count} / {summary.total_probes} {t('passing')}
                </Badge>
                <Badge variant="secondary" className="h-6 px-2 text-xs">
                    {summary.total_keys} {t('keys')} × {summary.total_models} {t('models')}
                </Badge>
                {summary.duration_ms > 0 && (
                    <Badge variant="secondary" className="h-6 px-2 text-xs gap-1 inline-flex items-center">
                        <Clock className="size-3" />
                        {(summary.duration_ms / 1000).toFixed(1)}s
                    </Badge>
                )}
                {summary.tested_at > 0 && (
                    <span className="text-[11px] text-muted-foreground ml-auto">
                        {t('lastTested')}: {formatTested(summary.tested_at)}
                    </span>
                )}
            </div>

            {/* Channel-level disabled banner */}
            {summary.disabled?.auto_disabled && (
                <div className="rounded-2xl border border-red-500/30 bg-red-500/5 p-3 flex items-start gap-3">
                    <AlertTriangle className="size-4 shrink-0 text-red-600 dark:text-red-400 mt-0.5" />
                    <div className="flex-1 min-w-0">
                        <div className="text-sm font-semibold text-red-700 dark:text-red-300">
                            {t('channelDisabledTitle')}
                        </div>
                        <div className="text-xs text-red-700/80 dark:text-red-400/80 mt-0.5">
                            {t('channelDisabledHint')}
                        </div>
                    </div>
                    <AttentionTag
                        cls={summary.disabled.disabled_class}
                        reason={summary.disabled.disabled_reason}
                    />
                </div>
            )}

            {/* Per-key results */}
            <div className="space-y-2">
                {(summary.keys ?? []).length === 0 && (
                    <div className="rounded-2xl border bg-card p-4 text-sm text-muted-foreground text-center">
                        {t('noKeysTested')}
                    </div>
                )}
                {(summary.keys ?? []).map((k) => (
                    <KeyResultsRow key={`${channel.id}-${k.key_id}`} keySummary={k} progress={progress} />
                ))}
            </div>
        </div>
    );
}
