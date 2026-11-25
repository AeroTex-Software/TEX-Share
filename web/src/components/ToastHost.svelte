<script lang="ts">
  import { dismissToast, toasts } from '../lib/stores';
  import Icon from './Icon.svelte';

  const styles: Record<string, string> = {
    success: 'border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200',
    error: 'border-rose-200 bg-rose-50 text-rose-900 dark:border-rose-500/30 dark:bg-rose-500/10 dark:text-rose-200',
    info: 'border-brand-200 bg-brand-50 text-brand-900 dark:border-brand-500/30 dark:bg-brand-500/10 dark:text-brand-100'
  };
  const icons: Record<string, string> = { success: 'check', error: 'error', info: 'info' };
</script>

<div class="pointer-events-none fixed bottom-4 right-4 z-[70] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2" aria-live="polite">
  {#each $toasts as item (item.id)}
    <div class="fade-in pointer-events-auto flex items-start gap-3 rounded-xl border px-4 py-3 text-sm shadow-lg {styles[item.kind]}">
      <Icon name={icons[item.kind]} size={18} />
      <p class="min-w-0 flex-1 leading-snug">{item.message}</p>
      <button type="button" class="opacity-60 transition hover:opacity-100" aria-label="Close" on:click={() => dismissToast(item.id)}>
        <Icon name="close" size={16} />
      </button>
    </div>
  {/each}
</div>
