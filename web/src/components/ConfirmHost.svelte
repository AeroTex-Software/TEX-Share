<script lang="ts">
  import { confirmRequest, resolveConfirm } from '../lib/stores';
  import { t } from '../lib/i18n';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';

  let typed = '';

  $: request = $confirmRequest;
  $: if (request) typed = '';
  $: matches = !request?.requireText || typed.trim() === request.requireText;

  function cancel(): void {
    resolveConfirm(false);
  }

  function accept(): void {
    if (!matches) return;
    resolveConfirm(true);
  }
</script>

<Modal
  open={Boolean(request)}
  title={request?.title || t('admin.common.confirmTitle')}
  size="sm"
  on:close={cancel}
>
  {#if request}
    <div class="flex items-start gap-3">
      <span
        class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full {request.danger
          ? 'bg-rose-100 text-rose-600 dark:bg-rose-500/15 dark:text-rose-300'
          : 'bg-brand-100 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300'}"
      >
        <Icon name={request.danger ? 'warning' : 'help'} size={20} />
      </span>
      <p class="whitespace-pre-line text-sm leading-relaxed text-slate-600 dark:text-slate-300">{request.message}</p>
    </div>
    {#if request.requireText}
      <div class="mt-5">
        <label class="label" for="confirm-text">{request.requireTextHint || request.requireText}</label>
        <input id="confirm-text" class="input" bind:value={typed} autocomplete="off" spellcheck="false" />
      </div>
    {/if}
    <div class="mt-6 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
      <button type="button" class="btn-secondary" on:click={cancel}>
        {request.cancelLabel || t('common.cancel')}
      </button>
      <button
        type="button"
        class={request.danger ? 'btn-danger' : 'btn-primary'}
        disabled={!matches}
        on:click={accept}
      >
        {request.confirmLabel || t('common.confirm')}
      </button>
    </div>
  {/if}
</Modal>
