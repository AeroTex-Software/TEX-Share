<script lang="ts">
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  export let version = '';
  export let admin = false;
</script>

<footer class="border-t border-slate-200/80 py-6 text-center text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
  <div class="mx-auto flex max-w-6xl flex-col items-center justify-center gap-2 px-4 sm:flex-row">
    {#if admin}
      <span>{t('admin.footer')}</span>
      <span class="hidden text-slate-300 dark:text-slate-700 sm:inline">·</span>
    {/if}
    <a
      href="https://github.com/AeroTex-Software/TEX-Share"
      target="_blank"
      rel="noopener noreferrer"
      class="inline-flex items-center gap-1.5 font-medium text-slate-600 transition hover:text-brand-600 dark:text-slate-300 dark:hover:text-brand-400"
    >
      <Icon name="shield" size={14} />
      <span>{t('app.footer')}</span>
    </a>
    {#if version}
      <span class="hidden text-slate-300 dark:text-slate-700 sm:inline">·</span>
      <span>v{version}</span>
    {/if}
  </div>
</footer>
