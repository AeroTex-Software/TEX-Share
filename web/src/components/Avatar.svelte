<script lang="ts">
  import { mediaURL } from '../lib/api';
  import { initials } from '../lib/format';
  import type { User } from '../lib/types';

  export let user: User | null = null;
  export let name = '';
  export let src = '';
  export let size = 40;
  export let rounded = 'full';

  let failed = false;

  let lastImage = '';

  $: displayName = name || user?.displayName || user?.username || '?';
  $: image = mediaURL(src || user?.avatarUrl || '');
  $: if (image !== lastImage) {
    lastImage = image;
    failed = false;
  }

  const palettes = [
    'from-brand-500 to-cyan-600',
    'from-indigo-500 to-violet-600',
    'from-amber-500 to-orange-600',
    'from-emerald-500 to-teal-600',
    'from-rose-500 to-pink-600',
    'from-sky-500 to-blue-600'
  ];

  $: hash = Array.from(displayName).reduce((acc, ch) => (acc * 31 + ch.charCodeAt(0)) >>> 0, 7);
  $: palette = palettes[hash % palettes.length];
  $: radius = rounded === 'full' ? '9999px' : rounded === 'xl' ? '0.75rem' : '0.5rem';
</script>

<span
  class="inline-flex shrink-0 items-center justify-center overflow-hidden bg-gradient-to-br font-semibold text-white {palette}"
  style="width:{size}px;height:{size}px;border-radius:{radius};font-size:{Math.max(10, size * 0.36)}px"
  title={displayName}
>
  {#if image && !failed}
    <img {src} alt={displayName} class="h-full w-full object-cover" on:error={() => (failed = true)} />
  {:else}
    {initials(displayName)}
  {/if}
</span>
