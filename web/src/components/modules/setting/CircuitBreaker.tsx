'use client';

import { useEffect, useState, useRef } from 'react';
import { useTranslations } from 'next-intl';
import { Zap, Hash, Timer, TimerOff, HelpCircle, ShieldOff, RotateCcw, Search } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { useSettingList, useSetSetting, SettingKey } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';

export function SettingCircuitBreaker() {
    const t = useTranslations('setting');
    const { data: settings } = useSettingList();
    const setSetting = useSetSetting();

    const [threshold, setThreshold] = useState('');
    const [cooldown, setCooldown] = useState('');
    const [maxCooldown, setMaxCooldown] = useState('');
    const [autoDisableThreshold, setAutoDisableThreshold] = useState('');
    const [autoDisableRetryHours, setAutoDisableRetryHours] = useState('');
    const [modelCheckInterval, setModelCheckInterval] = useState('');

    const initialThreshold = useRef('');
    const initialCooldown = useRef('');
    const initialMaxCooldown = useRef('');
    const initialAutoDisableThreshold = useRef('');
    const initialAutoDisableRetryHours = useRef('');
    const initialModelCheckInterval = useRef('');

    useEffect(() => {
        if (settings) {
            const th = settings.find(s => s.key === SettingKey.CircuitBreakerThreshold);
            const cd = settings.find(s => s.key === SettingKey.CircuitBreakerCooldown);
            const mcd = settings.find(s => s.key === SettingKey.CircuitBreakerMaxCooldown);
            if (th) {
                queueMicrotask(() => setThreshold(th.value));
                initialThreshold.current = th.value;
            }
            if (cd) {
                queueMicrotask(() => setCooldown(cd.value));
                initialCooldown.current = cd.value;
            }
            if (mcd) {
                queueMicrotask(() => setMaxCooldown(mcd.value));
                initialMaxCooldown.current = mcd.value;
            }
            const adt = settings.find(s => s.key === SettingKey.AutoDisableThreshold);
            const adr = settings.find(s => s.key === SettingKey.AutoDisableRetryHours);
            const mci = settings.find(s => s.key === SettingKey.ModelCheckInterval);
            if (adt) {
                queueMicrotask(() => setAutoDisableThreshold(adt.value));
                initialAutoDisableThreshold.current = adt.value;
            }
            if (adr) {
                queueMicrotask(() => setAutoDisableRetryHours(adr.value));
                initialAutoDisableRetryHours.current = adr.value;
            }
            if (mci) {
                queueMicrotask(() => setModelCheckInterval(mci.value));
                initialModelCheckInterval.current = mci.value;
            }
        }
    }, [settings]);

    const handleSave = (key: string, value: string, initialValue: string) => {
        if (value === initialValue) return;

        setSetting.mutate({ key, value }, {
            onSuccess: () => {
                toast.success(t('saved'));
                if (key === SettingKey.CircuitBreakerThreshold) {
                    initialThreshold.current = value;
                } else if (key === SettingKey.CircuitBreakerCooldown) {
                    initialCooldown.current = value;
                } else if (key === SettingKey.CircuitBreakerMaxCooldown) {
                    initialMaxCooldown.current = value;
                } else if (key === SettingKey.AutoDisableThreshold) {
                    initialAutoDisableThreshold.current = value;
                } else if (key === SettingKey.AutoDisableRetryHours) {
                    initialAutoDisableRetryHours.current = value;
                } else if (key === SettingKey.ModelCheckInterval) {
                    initialModelCheckInterval.current = value;
                }
            }
        });
    };

    return (
        <div className="rounded-3xl border border-border bg-card p-6 space-y-5">
            <h2 className="text-lg font-bold text-card-foreground flex items-center gap-2">
                <Zap className="h-5 w-5" />
                {t('circuitBreaker.title')}
                <TooltipProvider>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                        </TooltipTrigger>
                        <TooltipContent>
                            {t('circuitBreaker.hint')}
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            </h2>

            {/* 熔断触发阈值 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Hash className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('circuitBreaker.threshold.label')}</span>
                </div>
                <Input
                    type="number"
                    value={threshold}
                    onChange={(e) => setThreshold(e.target.value)}
                    onBlur={() => handleSave(SettingKey.CircuitBreakerThreshold, threshold, initialThreshold.current)}
                    placeholder={t('circuitBreaker.threshold.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 基础冷却时间 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Timer className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('circuitBreaker.cooldown.label')}</span>
                </div>
                <Input
                    type="number"
                    value={cooldown}
                    onChange={(e) => setCooldown(e.target.value)}
                    onBlur={() => handleSave(SettingKey.CircuitBreakerCooldown, cooldown, initialCooldown.current)}
                    placeholder={t('circuitBreaker.cooldown.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 最大冷却时间 */}
            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <TimerOff className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('circuitBreaker.maxCooldown.label')}</span>
                </div>
                <Input
                    type="number"
                    value={maxCooldown}
                    onChange={(e) => setMaxCooldown(e.target.value)}
                    onBlur={() => handleSave(SettingKey.CircuitBreakerMaxCooldown, maxCooldown, initialMaxCooldown.current)}
                    placeholder={t('circuitBreaker.maxCooldown.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 分隔线 */}
            <div className="border-t border-border" />

            {/* Auto Disable Section */}
            <h3 className="text-base font-bold text-card-foreground flex items-center gap-2">
                <ShieldOff className="h-4 w-4" />
                {t('autoDisable.title')}
                <TooltipProvider>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">
                            {t('autoDisable.hint')}
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            </h3>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Hash className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('autoDisable.threshold.label')}</span>
                </div>
                <Input
                    type="number"
                    value={autoDisableThreshold}
                    onChange={(e) => setAutoDisableThreshold(e.target.value)}
                    onBlur={() => handleSave(SettingKey.AutoDisableThreshold, autoDisableThreshold, initialAutoDisableThreshold.current)}
                    placeholder={t('autoDisable.threshold.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <RotateCcw className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('autoDisable.retryHours.label')}</span>
                </div>
                <Input
                    type="number"
                    value={autoDisableRetryHours}
                    onChange={(e) => setAutoDisableRetryHours(e.target.value)}
                    onBlur={() => handleSave(SettingKey.AutoDisableRetryHours, autoDisableRetryHours, initialAutoDisableRetryHours.current)}
                    placeholder={t('autoDisable.retryHours.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>

            {/* 分隔线 */}
            <div className="border-t border-border" />

            {/* Model Check Section */}
            <h3 className="text-base font-bold text-card-foreground flex items-center gap-2">
                <Search className="h-4 w-4" />
                {t('modelCheck.title')}
                <TooltipProvider>
                    <Tooltip>
                        <TooltipTrigger asChild>
                            <HelpCircle className="size-4 text-muted-foreground cursor-help" />
                        </TooltipTrigger>
                        <TooltipContent className="max-w-xs">
                            {t('modelCheck.hint')}
                        </TooltipContent>
                    </Tooltip>
                </TooltipProvider>
            </h3>

            <div className="flex items-center justify-between gap-4">
                <div className="flex items-center gap-3">
                    <Timer className="h-5 w-5 text-muted-foreground" />
                    <span className="text-sm font-medium">{t('modelCheck.interval.label')}</span>
                </div>
                <Input
                    type="number"
                    value={modelCheckInterval}
                    onChange={(e) => setModelCheckInterval(e.target.value)}
                    onBlur={() => handleSave(SettingKey.ModelCheckInterval, modelCheckInterval, initialModelCheckInterval.current)}
                    placeholder={t('modelCheck.interval.placeholder')}
                    className="w-48 rounded-xl"
                />
            </div>
        </div>
    );
}
