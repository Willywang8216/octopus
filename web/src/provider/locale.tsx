'use client';

import { useEffect, useState, type ReactNode } from 'react';
import { NextIntlClientProvider } from 'next-intl';
import { intlLocale, normalizeLocale, useSettingStore, type AppLocale } from '@/stores/setting';

import zh_hansMessages from '../../public/locale/zh_hans.json';
import zh_hantMessages from '../../public/locale/zh_hant.json';
import enMessages from '../../public/locale/en.json';

const messages: Record<AppLocale, typeof zh_hansMessages> = {
    zh_hans: zh_hansMessages,
    zh_hant: zh_hantMessages,
    en: enMessages,
};

export function LocaleProvider({ children }: { children: ReactNode }) {
    const { locale } = useSettingStore();
    const [currentLocale, setCurrentLocale] = useState<AppLocale>('zh_hans');

    useEffect(() => {
        setCurrentLocale(normalizeLocale(locale));
    }, [locale]);

    return (
        <NextIntlClientProvider
            locale={intlLocale(currentLocale)}
            messages={messages[currentLocale]}
            timeZone="Asia/Shanghai"
        >
            {children}
        </NextIntlClientProvider>
    );
}

