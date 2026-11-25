<script lang="ts">
  import { createEventDispatcher } from 'svelte';
  import { t } from '../lib/i18n';
  import { formatNumber } from '../lib/format';
  import Icon from './Icon.svelte';

  export let page = 1;
  export let limit = 25;
  export let total = 0;

  const dispatch = createEventDispatcher<{ change: number }>();

  $: pages = Math.max(1, Math.ceil(total / Math.max(1, limit)));
  $: from = total === 0 ? 0 : (page - 1) * limit + 1;
  $: to = Math.min(total, page * limit);

  function go(next: number): void {
    if (next < 1 || next > pages || next === page) return;
    dispatch('change', next);
  }
</script>

{#if total > 0}
  <div class="flex flex-col items-center justify-between gap-3 px-1 py-3 text-sm text-slate-500 dark:text-slate-400 sm:flex-row">
    <p>
      {formatNumber(from)}–{formatNumber(to)} {t('common.of')} {formatNumber(total)}
    </p>
    <div class="flex items-center gap-2">
      <button type="button" class="btn-secondary btn-sm" disabled={page <= 1} on:click={() => go(page - 1)}>
        <Icon name="chevronLeft" size={15} />
        <span class="hidden sm:inline">{t('common.back')}</span>
      </button>
      <span class="min-w-[5.5rem] text-center text-xs font-medium">{t('common.page')} {page} {t('common.of')} {pages}</span>
      <button type="button" class="btn-secondary btn-sm" disabled={page >= pages} on:click={() => go(page + 1)}>
        <span class="hidden sm:inline">{t('common.next')}</span>
        <Icon name="chevronRight" size={15} />
      </button>
    </div>
  </div>
{/if}
