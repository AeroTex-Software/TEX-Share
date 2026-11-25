<script lang="ts">
  export let checked = false;
  export let disabled = false;
  export let label = '';
  export let hint = '';
  export let id = `toggle-${Math.random().toString(36).slice(2, 9)}`;
</script>

<label class="flex cursor-pointer items-start gap-3 {disabled ? 'opacity-60' : ''}" for={id}>
  <button
    {id}
    type="button"
    role="switch"
    aria-checked={checked}
    {disabled}
    class="relative mt-0.5 inline-flex h-6 w-11 shrink-0 items-center rounded-full transition focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed dark:focus-visible:ring-offset-slate-950 {checked
      ? 'bg-brand-600'
      : 'bg-slate-300 dark:bg-slate-700'}"
    on:click={() => !disabled && (checked = !checked)}
  >
    <span
      class="inline-block h-5 w-5 transform rounded-full bg-white shadow transition {checked ? 'translate-x-5' : 'translate-x-0.5'}"
    />
  </button>
  {#if label || hint}
    <span class="min-w-0">
      {#if label}<span class="block text-sm font-medium text-slate-800 dark:text-slate-200">{label}</span>{/if}
      {#if hint}<span class="mt-0.5 block text-xs text-slate-500 dark:text-slate-400">{hint}</span>{/if}
    </span>
  {/if}
</label>
