<script context="module" lang="ts">
  let sparkSeq = 0;
</script>

<script lang="ts">
  // A compact area sparkline. Points are normalised to the box; the baseline is
  // pinned to the bottom so small trends stay readable.
  export let points: number[] = [];
  export let color = '#38bdf8';
  export let height = 40;
  export let width = 120;

  const PAD = 2;
  const gradId = 'spark-' + ++sparkSeq;

  $: max = Math.max(1, ...points);
  $: min = Math.min(0, ...points);
  $: span = Math.max(1, max - min);

  function xAt(i: number): number {
    return points.length <= 1 ? 0 : (i / (points.length - 1)) * width;
  }
  function yAt(v: number): number {
    return height - PAD - ((Math.max(0, v) - min) / span) * (height - PAD * 2);
  }
  $: line = points.map((v, i) => `${i === 0 ? 'M' : 'L'}${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ');
  $: area =
    points.length > 1
      ? `M0,${height} ${points.map((v, i) => `L${xAt(i).toFixed(2)},${yAt(v).toFixed(2)}`).join(' ')} L${width},${height} Z`
      : '';
</script>

{#if points.length > 1}
  <svg width="100%" height={height} viewBox="0 0 {width} {height}" preserveAspectRatio="none" aria-hidden="true">
    <defs>
      <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" stop-color={color} stop-opacity="0.35" />
        <stop offset="100%" stop-color={color} stop-opacity="0" />
      </linearGradient>
    </defs>
    <path d={area} fill="url(#{gradId})" />
    <path
      d={line}
      fill="none"
      stroke={color}
      stroke-width="2"
      stroke-linejoin="round"
      stroke-linecap="round"
      vector-effect="non-scaling-stroke"
    />
  </svg>
{:else}
  <div style="height:{height}px"></div>
{/if}
