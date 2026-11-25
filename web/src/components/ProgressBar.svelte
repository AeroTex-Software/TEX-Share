<script lang="ts">
  export let value = 0;
  export let max = 100;
  export let label = '';
  export let tone: 'brand' | 'emerald' | 'amber' | 'rose' = 'brand';
  export let showPercent = true;
  export let indeterminate = false;

  const tones: Record<string, string> = {
    brand: 'bg-brand-500',
    emerald: 'bg-emerald-500',
    amber: 'bg-amber-500',
    rose: 'bg-rose-500'
  };

  $: pct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0;
</script>

<div class="w-full">
  {#if label || showPercent}
    <div class="mb-1 flex items-center justify-between gap-2 text-xs text-slate-500 dark:text-slate-400">
      <span class="truncate">{label}</span>
      {#if showPercent && !indeterminate}
        <span class="shrink-0 tabular-nums">{Math.round(pct)}%</span>
      {/if}
    </div>
  {/if}
  <div
    class="h-2 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-800"
    role="progressbar"
    aria-valuenow={indeterminate ? undefined : Math.round(pct)}
    aria-valuemin="0"
    aria-valuemax="100"
    aria-label={label || undefined}
  >
    <div
      class="h-full rounded-full transition-all duration-200 {tones[tone]} {indeterminate ? 'animate-pulse' : ''}"
      style="width:{indeterminate ? 40 : pct}%"
    />
  </div>
</div>
