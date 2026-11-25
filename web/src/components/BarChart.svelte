<script lang="ts">
  export let data: { label: string; value: number }[] = [];
  export let tone: 'brand' | 'emerald' | 'amber' | 'indigo' = 'brand';
  export let height = 96;
  export let format: (value: number) => string = (v) => String(v);

  const tones: Record<string, string> = {
    brand: 'bg-brand-500/80 hover:bg-brand-500',
    emerald: 'bg-emerald-500/80 hover:bg-emerald-500',
    amber: 'bg-amber-500/80 hover:bg-amber-500',
    indigo: 'bg-indigo-500/80 hover:bg-indigo-500'
  };

  $: max = Math.max(1, ...data.map((d) => d.value));
</script>

{#if data.length === 0}
  <div class="flex items-center justify-center text-xs text-slate-400 dark:text-slate-500" style="height:{height}px">
    —
  </div>
{:else}
  <div class="flex items-end gap-1" style="height:{height}px">
    {#each data as item}
      <div class="group flex min-w-0 flex-1 flex-col items-center justify-end" title="{item.label}: {format(item.value)}">
        <div
          class="w-full rounded-t transition-all {tones[tone]}"
          style="height:{Math.max(2, (item.value / max) * (height - 18))}px"
        />
        <span class="mt-1 hidden w-full truncate text-center text-[10px] text-slate-400 dark:text-slate-500 sm:block">
          {item.label}
        </span>
      </div>
    {/each}
  </div>
{/if}
