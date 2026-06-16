'use client';

import { useEffect, useRef, useState } from 'react';
import { useTranslations } from 'next-intl';
import { Activity, Clock, Timer } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';

export function SettingChannelHealth() {
    const t = useTranslations('setting.channelHealth');
    const tRoot = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [interval, setInterval] = useState('');
    const [timeout, setTimeoutValue] = useState('');
    const initialInterval = useRef('');
    const initialTimeout = useRef('');

    useEffect(() => {
        if (!settings) return;
        const intervalSetting = settings.find((s) => s.key === SettingKey.HealthCheckInterval);
        const timeoutSetting = settings.find((s) => s.key === SettingKey.HealthCheckProbeTimeout);
        if (intervalSetting) {
            queueMicrotask(() => setInterval(intervalSetting.value));
            initialInterval.current = intervalSetting.value;
        }
        if (timeoutSetting) {
            queueMicrotask(() => setTimeoutValue(timeoutSetting.value));
            initialTimeout.current = timeoutSetting.value;
        }
    }, [settings]);

    const handleSave = (key: string, value: string, initialValue: string) => {
        if (value === initialValue) return;
        setSetting.mutate({ key, value }, {
            onSuccess: () => {
                toast.success(tRoot('saved'));
                if (key === SettingKey.HealthCheckInterval) initialInterval.current = value;
                if (key === SettingKey.HealthCheckProbeTimeout) initialTimeout.current = value;
            },
        });
    };

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Activity className="h-5 w-5" />
                {t('title')}
            </h2>
            <p className="text-xs text-muted-foreground">{t('hint')}</p>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Clock className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('interval.label')}</span>
                </div>
                <Input
                    type="number"
                    min="0"
                    value={interval}
                    onChange={(event) => setInterval(event.target.value)}
                    onBlur={() => handleSave(SettingKey.HealthCheckInterval, interval, initialInterval.current)}
                    placeholder={t('interval.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Timer className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('timeout.label')}</span>
                </div>
                <Input
                    type="number"
                    min="1"
                    value={timeout}
                    onChange={(event) => setTimeoutValue(event.target.value)}
                    onBlur={() => handleSave(SettingKey.HealthCheckProbeTimeout, timeout, initialTimeout.current)}
                    placeholder={t('timeout.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>
        </div>
    );
}
