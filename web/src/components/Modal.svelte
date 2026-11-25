<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte';
  import Icon from './Icon.svelte';

  export let open = false;
  export let title = '';
  export let subtitle = '';
  export let size: 'sm' | 'md' | 'lg' | 'xl' = 'md';
  export let closable = true;

  const dispatch = createEventDispatcher<{ close: void }>();

  const widths: Record<string, string> = {
    sm: 'max-w-md',
    md: 'max-w-xl',
    lg: 'max-w-3xl',
    xl: 'max-w-5xl'
  };

  let previousOverflow = '';

  function close(): void {
    if (closable) dispatch('close');
  }

  function onKey(event: KeyboardEvent): void {
    if (open && closable && event.key === 'Escape') {
      event.preventDefault();
      close();
    }
  }

  $: if (typeof document !== 'undefined') {
    if (open && !previousOverflow) {
      previousOverflow = document.body.style.overflow || 'auto';
      document.body.style.overflow = 'hidden';
    } else if (!open && previousOverflow) {
      document.body.style.overflow = previousOverflow === 'auto' ? '' : previousOverflow;
      previousOverflow = '';
    }
  }

  onDestroy(() => {
    if (previousOverflow) document.body.style.overflow = previousOverflow === 'auto' ? '' : previousOverflow;
  });
</script>

<svelte:window on:keydown={onKey} />

{#if open}
  <div class="fixed inset-0 z-50 flex items-end justify-center p-0 sm:items-center sm:p-6" role="presentation">
    <div
      class="absolute inset-0 bg-slate-950/60 backdrop-blur-sm"
      role="button"
      tabindex="-1"
      aria-label="Close"
      on:click={close}
      on:keydown={(e) => e.key === 'Enter' && close()}
    />
    <div
      class="fade-in relative flex max-h-[92vh] w-full {widths[size]} flex-col overflow-hidden rounded-t-2xl border border-slate-200 bg-white shadow-2xl dark:border-slate-800 dark:bg-slate-900 sm:rounded-2xl"
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div class="flex items-start gap-3 border-b border-slate-100 px-5 py-4 dark:border-slate-800">
        <slot name="header">
          <div class="min-w-0 flex-1">
            <h2 class="truncate text-lg font-semibold text-slate-900 dark:text-white">{title}</h2>
            {#if subtitle}
              <p class="mt-0.5 text-sm text-slate-500 dark:text-slate-400">{subtitle}</p>
            {/if}
          </div>
        </slot>
        {#if closable}
          <button type="button" class="btn-ghost -mr-2 -mt-1 p-2" aria-label="Close" on:click={close}>
            <Icon name="close" size={18} />
          </button>
        {/if}
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto px-5 py-5">
        <slot />
      </div>
      <slot name="footer" />
    </div>
  </div>
{/if}
