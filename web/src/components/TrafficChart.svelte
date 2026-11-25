<script lang="ts">
  // Multi-series line/area chart with a value axis and category labels. The plot
  // is drawn on a normalised 100x100 viewBox and stretched; strokes stay crisp
  // via non-scaling-stroke. Axis text is HTML so it never distorts.
  export let series: { label: string; color: string; points: number[] }[] = [];
  export let labels: string[] = [];
  export let height = 220;
  export let format: (value: number) => string = (v) => String(v);

  const W = 100;
  const H = 100;

  $: max = Math.max(1, ...series.flatMap((s) => s.points));
  $: count = series.reduce((m, s) => Math.max(m, s.points.length), 0);
  $: ticks = niceTicks(max, 4);
  $: tickMax = ticks.length > 1 ? ticks[ticks.length - 1] : max;

  function niceTicks(maxValue: number, segments: number): number[] {
    const raw = maxValue / segments;
    const mag = Math.pow(10, Math.floor(Math.log10(Math.max(1, raw))));
    const step = Math.max(1, Math.ceil(raw / mag) * mag);
    const out: number[] = [];
    for (let v = 0; v <= maxValue + step * 0.001; v += step) out.push(v);
    if (out.length < 2) out.push(step);
    return out;
  }

  function xAt(i: number): number {
    return count <= 1 ? W : (i / (count - 1)) * W;
  }
  function yAt(v: number): number {
    return H - (Math.max(0, v) / tickMax) * H;
  }
  function linePath(points: number[]): string {
    return points.map((v, i) => `${i === 0 ? 'M' : 'L'}${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ');
  }
  function areaPath(points: number[]): string {
    if (points.length === 0) return '';
    return `M0,${H} ${points.map((v, i) => `L${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ')} L${W},${H} Z`;
  }
</script>

<div class="flex gap-3" style="height:{height}px">
  <div class="flex w-14 shrink-0 flex-col justify-between text-right text-[10px] tabular-nums text-slate-400 dark:text-slate-500">
    {#each [...ticks].reverse() as tick}<span>{format(tick)}</span>{/each}
  </div>
  <div class="relative min-w-0 flex-1">
    <svg width="100%" height="100%" viewBox="0 0 {W} {H}" preserveAspectRatio="none" aria-hidden="true">
      {#each ticks as tick}
        <line
          x1="0"
          y1={yAt(tick)}
          x2={W}
          y2={yAt(tick)}
          stroke="currentColor"
          stroke-width="1"
          vector-effect="non-scaling-stroke"
          class="text-slate-200/70 dark:text-slate-800"
        />
      {/each}
      {#each series as s}
        <path d={areaPath(s.points)} fill={s.color} opacity="0.12" />
        <path
          d={linePath(s.points)}
          fill="none"
          stroke={s.color}
          stroke-width="2.5"
          stroke-linejoin="round"
          stroke-linecap="round"
          vector-effect="non-scaling-stroke"
        />
      {/each}
    </svg>
  </div>
</div>
<div class="mt-2 flex gap-3">
  <div class="w-14 shrink-0"></div>
  <div class="flex min-w-0 flex-1 justify-between text-[11px] font-medium text-slate-400 dark:text-slate-500">
    {#each labels as label}<span>{label}</span>{/each}
  </div>
</div>
