'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Activity, ChevronDown, ChevronUp, Key, Layers, StopCircle } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { type Channel, useCancelChannelTest, useChannelTestAllStatus } from '@/api/endpoints/channel';
import { HealthPill } from './HealthPill';
import { AttentionTag } from './TestResults';
import { toast } from '@/components/common/Toast';

const CHANNEL_TYPE_LABELS: Record<number, string> = {
    0: 'OpenAI Chat',
    1: 'OpenAI Response',
    2: 'Anthropic',
    3: 'Gemini',
    4: 'Volcengine',
    5: 'OpenAI Embedding',
    6: 'OpenAI Rerank',
};

function splitModels(models: string) {
    return models.split(',').map((s) => s.trim()).filter(Boolean);
}

export function ChannelOverview({
    channels,
    onChannelClick,
}: {
    channels: Array<{ raw: Channel }>;
    onChannelClick: (channelId: number) => void;
}) {
    const t = useTranslations('channel.overview');
    const tTest = useTranslations('channel.test');
    const [expanded, setExpanded] = useState(true);
    const cancelTest = useCancelChannelTest();
    const statusQuery = useChannelTestAllStatus(true);
    const isTestAllRunning = Boolean(statusQuery.data?.running);

    if (channels.length === 0) return null;

    return (
        <section className="mb-4 rounded-3xl border border-border bg-card/50 overflow-hidden">
            <div className="flex items-center justify-between gap-2 p-3 sm:p-4">
                <button
                    type="button"
                    onClick={() => setExpanded((v) => !v)}
                    className="flex items-center gap-2 text-sm font-semibold text-card-foreground hover:text-foreground transition-colors"
                >
                    <Activity className="size-4 text-primary" />
                    {t('title')}
                    <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">
                        {channels.length}
                    </Badge>
                    {expanded ? <ChevronUp className="size-3.5 text-muted-foreground" /> : <ChevronDown className="size-3.5 text-muted-foreground" />}
                </button>
                {isTestAllRunning && (
                    <Button
                        type="button"
                        variant="destructive"
                        size="sm"
                        onClick={() => {
                            cancelTest.mutate(undefined, {
                                onSuccess: () => toast.success(tTest('toastCancelledAll')),
                            });
                        }}
                        disabled={cancelTest.isPending}
                        className="h-7 rounded-lg gap-1 text-xs"
                    >
                        <StopCircle className="size-3" />
                        {t('stopAll')}
                    </Button>
                )}
            </div>

            {expanded && (
                <div className="border-t">
                    {/* Desktop table header */}
                    <div className="hidden lg:grid lg:grid-cols-[1fr_100px_80px_80px_100px_120px_80px_80px] gap-2 px-4 py-2 text-xs font-medium text-muted-foreground uppercase tracking-wider border-b bg-muted/30">
                        <span>{t('name')}</span>
                        <span>{t('type')}</span>
                        <span>{t('keys')}</span>
                        <span>{t('models')}</span>
                        <span>{t('testResult')}</span>
                        <span>{t('error')}</span>
                        <span>{t('status')}</span>
                        <span className="text-right">{t('action')}</span>
                    </div>

                    {/* Channel rows */}
                    <div className="divide-y max-h-[400px] overflow-y-auto">
                        {channels.map(({ raw: channel }) => {
                            const modelCount = new Set([...splitModels(channel.model), ...splitModels(channel.custom_model)]).size;
                            const enabledKeyCount = channel.keys.filter((k) => k.enabled).length;
                            const testSuccess = channel.keys.reduce((s, k) => s + (k.last_test_success ?? 0), 0);
                            const testFailed = channel.keys.reduce((s, k) => s + (k.last_test_failed ?? 0), 0);
                            const testTotal = testSuccess + testFailed;
                            const progress = channel.test_progress;
                            const isRunning = progress?.running;

                            return (
                                <button
                                    key={channel.id}
                                    type="button"
                                    onClick={() => onChannelClick(channel.id)}
                                    className="w-full text-left hover:bg-accent/5 transition-colors focus:outline-none focus:ring-2 focus:ring-primary/20 focus:ring-inset"
                                >
                                    {/* Desktop layout */}
                                    <div className="hidden lg:grid lg:grid-cols-[1fr_100px_80px_80px_100px_120px_80px_80px] gap-2 items-center px-4 py-2.5">
                                        <div className="flex items-center gap-2 min-w-0">
                                            <span className="text-sm font-medium truncate">{channel.name}</span>
                                            <HealthPill health={channel.health} size="sm" />
                                        </div>
                                        <span className="text-xs text-muted-foreground truncate">
                                            {CHANNEL_TYPE_LABELS[channel.type] ?? 'Unknown'}
                                        </span>
                                        <Badge
                                            variant="secondary"
                                            className={cn(
                                                'h-5 px-1.5 text-[10px] gap-1 w-fit',
                                                enabledKeyCount === 0 && channel.keys.length > 0
                                                    ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                                    : ''
                                            )}
                                        >
                                            <Key className="size-3" />
                                            {enabledKeyCount}/{channel.keys.length}
                                        </Badge>
                                        <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1 w-fit">
                                            <Layers className="size-3" />
                                            {modelCount}
                                        </Badge>
                                        <div>
                                            {isRunning ? (
                                                <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1 bg-orange-500/15 text-orange-700 dark:text-orange-400">
                                                    <Activity className="size-3 animate-pulse" />
                                                    {progress?.completed_probes ?? 0}/{progress?.total_probes ?? 0}
                                                </Badge>
                                            ) : testTotal > 0 ? (
                                                <Badge
                                                    variant="secondary"
                                                    className={cn(
                                                        'h-5 px-1.5 text-[10px] gap-1',
                                                        testFailed === 0
                                                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                                                            : testSuccess === 0
                                                                ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                                                : 'bg-orange-500/15 text-orange-700 dark:text-orange-400'
                                                    )}
                                                >
                                                    {testSuccess}/{testTotal}
                                                </Badge>
                                            ) : (
                                                <span className="text-xs text-muted-foreground">—</span>
                                            )}
                                        </div>
                                        <div>
                                            {channel.auto_disabled && (
                                                <AttentionTag
                                                    cls={channel.disabled_class}
                                                    reason={channel.disabled_reason}
                                                    className="max-w-[110px] truncate"
                                                />
                                            )}
                                        </div>
                                        <div>
                                            {isRunning && progress?.current_model && (
                                                <span className="text-[10px] text-muted-foreground truncate block max-w-[70px]" title={progress.current_model}>
                                                    {progress.current_model}
                                                </span>
                                            )}
                                        </div>
                                        <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
                                            {isRunning && (
                                                <Button
                                                    type="button"
                                                    variant="destructive"
                                                    size="sm"
                                                    onClick={(e) => {
                                                        e.stopPropagation();
                                                        cancelTest.mutate(
                                                            { channel_id: channel.id },
                                                            { onSuccess: () => toast.success(tTest('toastCancelled')) }
                                                        );
                                                    }}
                                                    disabled={cancelTest.isPending}
                                                    className="h-6 px-2 rounded-md gap-1 text-[10px]"
                                                >
                                                    <StopCircle className="size-3" />
                                                    {t('stopChannel')}
                                                </Button>
                                            )}
                                        </div>
                                    </div>

                                    {/* Mobile layout */}
                                    <div className="lg:hidden p-3 space-y-2">
                                        <div className="flex items-center justify-between gap-2">
                                            <div className="flex items-center gap-2 min-w-0">
                                                <span className="text-sm font-medium truncate">{channel.name}</span>
                                                <HealthPill health={channel.health} size="sm" />
                                            </div>
                                            {isRunning && (
                                                <Button
                                                    type="button"
                                                    variant="destructive"
                                                    size="sm"
                                                    onClick={(e) => {
                                                        e.stopPropagation();
                                                        cancelTest.mutate(
                                                            { channel_id: channel.id },
                                                            { onSuccess: () => toast.success(tTest('toastCancelled')) }
                                                        );
                                                    }}
                                                    disabled={cancelTest.isPending}
                                                    className="h-6 px-2 rounded-md gap-1 text-[10px] shrink-0"
                                                >
                                                    <StopCircle className="size-3" />
                                                    {t('stopChannel')}
                                                </Button>
                                            )}
                                        </div>
                                        <div className="flex flex-wrap items-center gap-1.5">
                                            <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1">
                                                {CHANNEL_TYPE_LABELS[channel.type] ?? 'Unknown'}
                                            </Badge>
                                            <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1">
                                                <Key className="size-3" />
                                                {enabledKeyCount}/{channel.keys.length}
                                            </Badge>
                                            <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1">
                                                <Layers className="size-3" />
                                                {modelCount}
                                            </Badge>
                                            {isRunning ? (
                                                <Badge variant="secondary" className="h-5 px-1.5 text-[10px] gap-1 bg-orange-500/15 text-orange-700 dark:text-orange-400">
                                                    <Activity className="size-3 animate-pulse" />
                                                    {progress?.completed_probes ?? 0}/{progress?.total_probes ?? 0}
                                                </Badge>
                                            ) : testTotal > 0 ? (
                                                <Badge
                                                    variant="secondary"
                                                    className={cn(
                                                        'h-5 px-1.5 text-[10px] gap-1',
                                                        testFailed === 0
                                                            ? 'bg-emerald-500/15 text-emerald-700 dark:text-emerald-400'
                                                            : testSuccess === 0
                                                                ? 'bg-red-500/15 text-red-700 dark:text-red-400'
                                                                : 'bg-orange-500/15 text-orange-700 dark:text-orange-400'
                                                    )}
                                                >
                                                    {testSuccess}/{testTotal}
                                                </Badge>
                                            ) : null}
                                            {channel.auto_disabled && (
                                                <AttentionTag
                                                    cls={channel.disabled_class}
                                                    reason={channel.disabled_reason}
                                                />
                                            )}
                                        </div>
                                        {isRunning && progress?.current_model && (
                                            <div className="text-[10px] text-muted-foreground truncate">
                                                {progress.current_key} → {progress.current_model}
                                            </div>
                                        )}
                                    </div>
                                </button>
                            );
                        })}
                    </div>
                </div>
            )}
        </section>
    );
}
