<script lang="ts">
  import Icon from './Icon.svelte';

  export let icon = 'inbox';
  export let title = '';
  export let message = '';
</script>

<div class="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
  <span class="flex h-14 w-14 items-center justify-center rounded-2xl bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500">
    <Icon name={icon} size={26} />
  </span>
  {#if title}
    <p class="text-base font-semibold text-slate-800 dark:text-slate-200">{title}</p>
  {/if}
  {#if message}
    <p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{message}</p>
  {/if}
  <slot />
</div>
