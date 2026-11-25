<script lang="ts">
  import { locale, setLocale, t } from '../lib/i18n';
  import { setTheme, theme } from '../lib/theme';
  import type { Theme } from '../lib/types';
  import Icon from './Icon.svelte';

  export let compact = false;

  const themes: { value: Theme; icon: string; label: string }[] = [
    { value: 'system', icon: 'monitor', label: 'common.theme.system' },
    { value: 'light', icon: 'sun', label: 'common.theme.light' },
    { value: 'dark', icon: 'moon', label: 'common.theme.dark' }
  ];
</script>

<div class="flex items-center gap-1.5">
  <div class="flex items-center rounded-lg border border-slate-200 bg-white p-0.5 dark:border-slate-700 dark:bg-slate-900" role="group" aria-label={t('common.theme')}>
    {#each themes as item}
      <button
        type="button"
        class="rounded-md p-1.5 transition {$theme === item.value
          ? 'bg-brand-100 text-brand-700 dark:bg-brand-500/20 dark:text-brand-300'
          : 'text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'}"
        title={t(item.label)}
        aria-label={t(item.label)}
        aria-pressed={$theme === item.value}
        on:click={() => setTheme(item.value)}
      >
        <Icon name={item.icon} size={compact ? 15 : 16} />
      </button>
    {/each}
  </div>
  <div class="flex items-center rounded-lg border border-slate-200 bg-white p-0.5 text-xs font-semibold dark:border-slate-700 dark:bg-slate-900" role="group" aria-label={t('common.language')}>
    <button
      type="button"
      class="rounded-md px-2 py-1 uppercase transition {$locale === 'de'
        ? 'bg-brand-100 text-brand-700 dark:bg-brand-500/20 dark:text-brand-300'
        : 'text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'}"
      aria-pressed={$locale === 'de'}
      on:click={() => setLocale('de')}
    >DE</button>
    <button
      type="button"
      class="rounded-md px-2 py-1 uppercase transition {$locale === 'en'
        ? 'bg-brand-100 text-brand-700 dark:bg-brand-500/20 dark:text-brand-300'
        : 'text-slate-500 hover:bg-slate-100 dark:text-slate-400 dark:hover:bg-slate-800'}"
      aria-pressed={$locale === 'en'}
      on:click={() => setLocale('en')}
    >EN</button>
  </div>
</div>
