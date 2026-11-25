<script lang="ts">
  // Multi-series line/area chart drawn on a normalized 100x100 viewBox and
  // stretched to the container. Strokes stay crisp via non-scaling-stroke.
  export let series: { label: string; color: string; points: number[] }[] = [];
  export let height = 150;
  export let format: (value: number) => string = (v) => String(v);

  const W = 100;
  const H = 100;

  $: max = Math.max(1, ...series.flatMap((s) => s.points));
  $: count = series.reduce((m, s) => Math.max(m, s.points.length), 0);

  function xAt(i: number): number {
    return count <= 1 ? W : (i / (count - 1)) * W;
  }
  function yAt(v: number): number {
    return H - (Math.max(0, v) / max) * H;
  }
  function linePath(points: number[]): string {
    if (points.length === 0) return '';
    return points.map((v, i) => `${i === 0 ? 'M' : 'L'}${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ');
  }
  function areaPath(points: number[]): string {
    if (points.length === 0) return '';
    const body = points.map((v, i) => `L${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ');
    return `M0,${H} ${body} L${W},${H} Z`;
  }
</script>

<div class="relative w-full" style="height:{height}px">
  <svg width="100%" height="100%" viewBox="0 0 {W} {H}" preserveAspectRatio="none" role="img" aria-label={format(max)}>
    {#each [0, 25, 50, 75, 100] as g}
      <line
        x1="0"
        y1={g}
        x2={W}
        y2={g}
        stroke="currentColor"
        stroke-width="1"
        vector-effect="non-scaling-stroke"
        class="text-slate-200/70 dark:text-slate-800"
      />
    {/each}
    {#each series as s}
      <path d={areaPath(s.points)} fill={s.color} opacity="0.14" />
      <path
        d={linePath(s.points)}
        fill="none"
        stroke={s.color}
        stroke-width="2"
        stroke-linejoin="round"
        stroke-linecap="round"
        vector-effect="non-scaling-stroke"
      />
    {/each}
  </svg>
  <span class="pointer-events-none absolute right-1 top-0 text-[10px] tabular-nums text-slate-400 dark:text-slate-500">
    {format(max)}
  </span>
</div>
