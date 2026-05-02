'use client';

import { useMemo } from 'react';
import { type ChannelTestProgress, useChannelList } from '@/api/endpoints/channel';
import { Card } from './Card';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { Activity } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { useTranslations } from 'next-intl';

function ChannelTestingActivity({ progresses }: { progresses: ChannelTestProgress[] }) {
    const t = useTranslations('channel.test.progress');
    if (progresses.length === 0) return null;

    return (
        <section className="mb-4 rounded-3xl border border-orange-500/30 bg-orange-500/5 p-4 space-y-3">
            <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 text-sm font-semibold text-orange-700 dark:text-orange-300">
                    <Activity className="size-4 animate-pulse" />
                    {t('pageTitle')}
                </div>
                <Badge variant="secondary" className="h-6 px-2 text-xs bg-orange-500/15 text-orange-700 dark:text-orange-300">
                    {progresses.length}
                </Badge>
            </div>
            <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                {progresses.map((progress) => {
                    const total = progress.total_probes || 0;
                    const completed = progress.completed_probes || 0;
                    const pct = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0;
                    const waiting = progress.phase === 'waiting' || !progress.current_model;
                    return (
                        <div key={progress.channel_id} className="rounded-2xl border bg-card/80 p-3 space-y-2">
                            <div className="flex items-center justify-between gap-2">
                                <div className="min-w-0">
                                    <div className="truncate text-sm font-medium text-card-foreground">{progress.channel_name || `#${progress.channel_id}`}</div>
                                    <div className="text-xs text-muted-foreground">
                                        {completed}/{total || '—'} · {t(progress.phase === 'waiting' ? 'waiting' : 'running')}
                                    </div>
                                </div>
                                <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">{pct}%</Badge>
                            </div>
                            <div className="h-2 overflow-hidden rounded-full bg-muted">
                                <div className="h-full bg-orange-500 transition-all" style={{ width: `${pct}%` }} />
                            </div>
                            <div className="grid gap-1 text-xs text-muted-foreground">
                                <div><span className="text-card-foreground">{t('key')}:</span> {waiting ? t('waiting') : progress.current_key || '—'}</div>
                                <div><span className="text-card-foreground">{t('model')}:</span> {waiting ? t('waiting') : progress.current_model || '—'}</div>
                            </div>
                        </div>
                    );
                })}
            </div>
        </section>
    );
}

export function Channel() {
    const { data: channelsData } = useChannelList();
    const pageKey = 'channel' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((s) => s.getLayout(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.channelFilter);
    const healthFilter = useToolbarViewOptionsStore((s) => s.channelHealthFilter);

    const sortedChannels = useMemo(() => {
        if (!channelsData) return [];
        return [...channelsData].sort((a, b) => {
            const diff = sortField === 'name'
                ? a.raw.name.localeCompare(b.raw.name)
                : a.raw.id - b.raw.id;
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [channelsData, sortField, sortOrder]);

    const activeProgresses = useMemo(() => {
        return (channelsData ?? [])
            .map((item) => item.raw.test_progress)
            .filter((progress): progress is ChannelTestProgress => !!progress?.running);
    }, [channelsData]);

    const visibleChannels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        const byName = !term ? sortedChannels : sortedChannels.filter((c) => c.raw.name.toLowerCase().includes(term));

        const byEnabled = filter === 'enabled'
            ? byName.filter((c) => c.raw.enabled)
            : filter === 'disabled'
                ? byName.filter((c) => !c.raw.enabled)
                : byName;

        const byHealth = healthFilter === 'all'
            ? byEnabled
            : byEnabled.filter((c) => (c.raw.health ?? 'unknown') === healthFilter);

        return byHealth;
    }, [sortedChannels, searchTerm, filter, healthFilter]);

    return (
        <>
            <ChannelTestingActivity progresses={activeProgresses} />
            <VirtualizedGrid
            items={visibleChannels}
            layout={layout}
            columns={{ default: 1, md: 2, lg: 3 }}
            estimateItemHeight={216}
            getItemKey={(item) => `channel-${item.raw.id}`}
            renderItem={(item) => <Card channel={item.raw} stats={item.formatted} layout={layout} />}
            />
        </>
    );
}
