import { create } from 'zustand';
import { persist } from 'zustand/middleware';

export type AppLocale = 'zh_hans' | 'zh_hant' | 'en';
export type Locale = AppLocale | 'zh-Hans' | 'zh-Hant';

export function normalizeLocale(locale: Locale | string | null | undefined): AppLocale {
    switch (locale) {
        case 'zh-Hans':
        case 'zh_hans':
            return 'zh_hans';
        case 'zh-Hant':
        case 'zh_hant':
            return 'zh_hant';
        default:
            return 'en';
    }
}

export function intlLocale(locale: Locale | string | null | undefined): 'zh-Hans' | 'zh-Hant' | 'en' {
    switch (normalizeLocale(locale)) {
        case 'zh_hans':
            return 'zh-Hans';
        case 'zh_hant':
            return 'zh-Hant';
        default:
            return 'en';
    }
}

interface SettingState {
    locale: AppLocale;
    setLocale: (locale: Locale) => void;
}

export const useSettingStore = create<SettingState>()(
    persist(
        (set) => ({
            locale: 'zh_hans',
            setLocale: (locale) => set({ locale: normalizeLocale(locale) }),
        }),
        {
            name: 'octopus-settings',
        }
    )
);

