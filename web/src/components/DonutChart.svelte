<script lang="ts">
  // A lightweight SVG donut. Segments are drawn clockwise starting at the top;
  // anything not covered by a segment shows the neutral track.
  export let segments: { label: string; value: number; color: string }[] = [];
  export let size = 168;
  export let thickness = 18;
  export let centerValue = '';
  export let centerLabel = '';
  // When set, segment values are a share of this fixed maximum (gauge mode)
  // instead of the sum of all segments (pie mode).
  export let max = 0;

  $: radius = Math.max(1, (size - thickness) / 2);
  $: circumference = 2 * Math.PI * radius;
  $: total = max > 0 ? max : segments.reduce((sum, s) => sum + Math.max(0, s.value), 0);
  $: arcs = (() => {
    if (total <= 0) return [] as { dash: string; offset: number; color: string; label: string }[];
    let acc = 0;
    return segments
      .filter((s) => s.value > 0)
      .map((s) => {
        const len = (s.value / total) * circumference;
        const arc = { dash: `${len} ${circumference - len}`, offset: -acc, color: s.color, label: s.label };
        acc += len;
        return arc;
      });
  })();
</script>

<div class="relative inline-flex items-center justify-center" style="width:{size}px;height:{size}px">
  <svg width={size} height={size} viewBox="0 0 {size} {size}" class="-rotate-90" aria-hidden="true">
    <circle
      cx={size / 2}
      cy={size / 2}
      r={radius}
      fill="none"
      stroke="currentColor"
      stroke-width={thickness}
      class="text-slate-200 dark:text-slate-800"
    />
    {#each arcs as arc}
      <circle
        cx={size / 2}
        cy={size / 2}
        r={radius}
        fill="none"
        stroke={arc.color}
        stroke-width={thickness}
        stroke-dasharray={arc.dash}
        stroke-dashoffset={arc.offset}
        stroke-linecap="butt"
      >
        <title>{arc.label}</title>
      </circle>
    {/each}
  </svg>
  <div class="absolute inset-0 flex flex-col items-center justify-center text-center">
    {#if centerValue}<span class="text-lg font-bold tracking-tight text-slate-900 dark:text-white">{centerValue}</span>{/if}
    {#if centerLabel}<span class="mt-0.5 text-[10px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{centerLabel}</span>{/if}
  </div>
</div>
