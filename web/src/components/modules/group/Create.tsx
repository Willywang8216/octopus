'use client';

import { useState } from 'react';
import type { GroupItem } from '@/api/endpoints/group';
import {
    MorphingDialogClose,
    MorphingDialogTitle,
    MorphingDialogDescription,
    useMorphingDialog,
} from '@/components/ui/morphing-dialog';
import { useCreateCoderPresetGroups, useCreateGroup } from '@/api/endpoints/group';
import { useTranslations } from 'next-intl';
import { GroupEditor } from './Editor';
import { toast } from '@/components/common/Toast';
import { Input } from '@/components/ui/input';
import { Button } from '@/components/ui/button';

export function CreateDialogContent() {
    const { setIsOpen } = useMorphingDialog();
    const createGroup = useCreateGroup();
    const createCoderPresetGroups = useCreateCoderPresetGroups();
    const t = useTranslations('group');
    const [presetModel, setPresetModel] = useState('agentic-coder');

    const handleCreateCoderPresets = () => {
        const modelName = presetModel.trim();
        if (!modelName) return;
        createCoderPresetGroups.mutate(modelName, {
            onSuccess: () => {
                toast.success(t('preset.success'));
                setIsOpen(false);
            },
            onError: (error) => toast.error(t('preset.failed'), { description: error.message }),
        });
    };

    return (
        <div className="w-screen max-w-full md:max-w-4xl h-[calc(100vh-2rem)] min-h-0 flex flex-col">
            <MorphingDialogTitle className="shrink-0">
                <header className="mb-5 flex items-center justify-between">
                    <h2 className="text-2xl font-bold text-card-foreground">
                        {t('create.title')}
                    </h2>
                    <MorphingDialogClose
                        className="relative right-0 top-0"
                        variants={{
                            initial: { opacity: 0, scale: 0.8 },
                            animate: { opacity: 1, scale: 1 },
                            exit: { opacity: 0, scale: 0.8 },
                        }}
                    />
                </header>
            </MorphingDialogTitle>
            <MorphingDialogDescription className="flex-1 min-h-0 overflow-hidden">
                <div className="mb-4 rounded-2xl border border-border bg-muted/30 p-3">
                    <div className="mb-2 text-sm font-medium text-card-foreground">{t('preset.title')}</div>
                    <div className="flex flex-col gap-2 sm:flex-row">
                        <Input
                            value={presetModel}
                            onChange={(event) => setPresetModel(event.target.value)}
                            placeholder={t('preset.modelPlaceholder')}
                            className="rounded-xl"
                        />
                        <Button
                            type="button"
                            onClick={handleCreateCoderPresets}
                            disabled={createCoderPresetGroups.isPending || !presetModel.trim()}
                            className="rounded-xl"
                        >
                            {createCoderPresetGroups.isPending ? t('preset.creating') : t('preset.create')}
                        </Button>
                    </div>
                    <p className="mt-2 text-xs text-muted-foreground">{t('preset.hint')}</p>
                </div>
                <GroupEditor
                    submitText={t('create.submit')}
                    submittingText={t('create.submitting')}
                    isSubmitting={createGroup.isPending}
                    onSubmit={({ name, match_regex, mode, first_token_time_out, session_keep_time, members }) => {
                        const items: GroupItem[] = members.map((member, index) => ({
                            channel_id: member.channel_id,
                            model_name: member.name,
                            priority: index + 1,
                            weight: member.weight ?? 1,
                        }));

                        createGroup.mutate(
                            { name, mode, match_regex: match_regex ?? '', first_token_time_out: first_token_time_out ?? 0, session_keep_time: session_keep_time ?? 0, items },
                            {
                                onSuccess: () => setIsOpen(false),
                                onError: (error) => toast.error(t('toast.createFailed'), { description: error.message }),
                            }
                        );
                    }}
                />
            </MorphingDialogDescription>
        </div>
    );
}
