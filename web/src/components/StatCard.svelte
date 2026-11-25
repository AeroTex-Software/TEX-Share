<script lang="ts">
  import Icon from './Icon.svelte';

  export let label = '';
  export let value: string | number = '';
  export let hint = '';
  export let icon = 'chart';
  export let tone: 'brand' | 'emerald' | 'amber' | 'rose' | 'slate' | 'indigo' = 'brand';

  const tones: Record<string, string> = {
    brand: 'bg-brand-100 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300',
    emerald: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300',
    amber: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300',
    rose: 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300',
    slate: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-300',
    indigo: 'bg-indigo-100 text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300'
  };
</script>

<div class="card card-pad">
  <div class="flex items-start justify-between gap-3">
    <div class="min-w-0">
      <p class="truncate text-xs font-medium uppercase tracking-wide text-slate-500 dark:text-slate-400">{label}</p>
      <p class="mt-2 text-2xl font-bold tracking-tight text-slate-900 dark:text-white">{value}</p>
      {#if hint}
        <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">{hint}</p>
      {/if}
    </div>
    <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl {tones[tone]}">
      <Icon name={icon} size={20} />
    </span>
  </div>
  <slot />
</div>
