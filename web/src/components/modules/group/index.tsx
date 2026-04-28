'use client';

import { useMemo } from 'react';
import { GroupCard } from './Card';
import { useGroupList, useCreateAgenticGroups } from '@/api/endpoints/group';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { Button } from '@/components/ui/button';
import { toast } from '@/components/common/Toast';
import { useTranslations } from 'next-intl';
import { Bot } from 'lucide-react';

export function Group() {
    const { data: groups } = useGroupList();
    const createAgentic = useCreateAgenticGroups();
    const t = useTranslations('group.createAgentic');
    const pageKey = 'group' as const;
    const searchTerm = useSearchStore((s) => s.getSearchTerm(pageKey));
    const sortField = useToolbarViewOptionsStore((s) => s.getSortField(pageKey));
    const sortOrder = useToolbarViewOptionsStore((s) => s.getSortOrder(pageKey));
    const filter = useToolbarViewOptionsStore((s) => s.groupFilter);

    const sortedGroups = useMemo(() => {
        if (!groups) return [];
        return [...groups].sort((a, b) => {
            const diff = sortField === 'name'
                ? a.name.localeCompare(b.name)
                : (a.id || 0) - (b.id || 0);
            return sortOrder === 'asc' ? diff : -diff;
        });
    }, [groups, sortField, sortOrder]);

    const visibleGroups = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        const byName = !term ? sortedGroups : sortedGroups.filter((g) => g.name.toLowerCase().includes(term));

        if (filter === 'with-members') return byName.filter((g) => (g.items?.length || 0) > 0);
        if (filter === 'empty') return byName.filter((g) => (g.items?.length || 0) === 0);

        return byName;
    }, [sortedGroups, searchTerm, filter]);

    const handleCreateAgentic = () => {
        createAgentic.mutate(undefined, {
            onSuccess: () => toast.success(t('success')),
            onError: (err) => toast.error(t('failed'), { description: err.message }),
        });
    };

    return (
        <div className="flex flex-col gap-4 h-full">
            <div className="flex justify-end px-1">
                <Button
                    variant="outline"
                    size="sm"
                    onClick={handleCreateAgentic}
                    disabled={createAgentic.isPending}
                    className="rounded-xl gap-2"
                >
                    <Bot className="size-4" />
                    {createAgentic.isPending ? t('creating') : t('button')}
                </Button>
            </div>
            <div className="flex-1 min-h-0">
                <VirtualizedGrid
                    items={visibleGroups}
                    columns={{ default: 1, md: 2, lg: 3 }}
                    estimateItemHeight={520}
                    getItemKey={(group, index) => group.id ?? `group-${index}`}
                    renderItem={(group) => <GroupCard group={group} />}
                />
            </div>
        </div>
    );
}
