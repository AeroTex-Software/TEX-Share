<script lang="ts">
  import { onMount } from 'svelte';
  import { api, downloadHref, errorText } from '../../lib/api';
  import { formatDateTime, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { toast } from '../../lib/stores';
  import type { AuditEntry } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Pagination from '../../components/Pagination.svelte';

  let entries: AuditEntry[] = [];
  let total = 0;
  let page = 1;
  const limit = 50;
  let loading = true;

  let filterAction = '';
  let filterActor = '';
  let filterResult = '';
  let filterFrom = '';
  let filterTo = '';

  let verifying = false;
  let verifyResult: { ok: boolean; checked: number; brokenAt?: number; message?: string } | null = null;

  let detailOpen = false;
  let detailEntry: AuditEntry | null = null;

  onMount(load);

  function toUnix(value: string): number {
    if (!value) return 0;
    const ms = Date.parse(value);
    return Number.isFinite(ms) ? Math.floor(ms / 1000) : 0;
  }

  async function load(): Promise<void> {
    loading = true;
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) });
      if (filterAction.trim()) params.set('action', filterAction.trim());
      if (filterActor.trim()) params.set('actor', filterActor.trim());
      if (filterResult) params.set('result', filterResult);
      const from = toUnix(filterFrom);
      const to = toUnix(filterTo);
      if (from) params.set('from', String(from));
      if (to) params.set('to', String(to));
      const res = await api<{ entries: AuditEntry[]; total: number }>(`/audit?${params.toString()}`);
      entries = res.entries || [];
      total = res.total || 0;
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  function search(): void {
    page = 1;
    void load();
  }

  function reset(): void {
    filterAction = '';
    filterActor = '';
    filterResult = '';
    filterFrom = '';
    filterTo = '';
    page = 1;
    void load();
  }

  function changePage(next: number): void {
    page = next;
    void load();
  }

  async function verify(): Promise<void> {
    verifying = true;
    verifyResult = null;
    try {
      verifyResult = await api<{ ok: boolean; checked: number; brokenAt?: number; message?: string }>('/audit/verify');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      verifying = false;
    }
  }

  function exportCsv(): void {
    window.location.href = downloadHref('/audit/export');
  }

  function resultTone(result: string): string {
    if (result === 'success') return 'badge-green';
    if (result === 'failure') return 'badge-red';
    if (result === 'denied') return 'badge-amber';
    return 'badge-slate';
  }

  function detailsText(value: unknown): string {
    if (value === null || value === undefined) return '';
    if (typeof value === 'string') return value;
    try {
      return JSON.stringify(value, null, 2);
    } catch {
      return String(value);
    }
  }

  function openDetail(entry: AuditEntry): void {
    detailEntry = entry;
    detailOpen = true;
  }
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.audit.subtitle')}</p>
    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn-secondary btn-sm" on:click={verify} disabled={verifying}>
        {#if verifying}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="shield" size={15} />{/if}
        {t('admin.audit.verify')}
      </button>
      <button type="button" class="btn-secondary btn-sm" on:click={exportCsv}><Icon name="download" size={15} />{t('admin.audit.export')}</button>
    </div>
  </div>

  {#if verifyResult}
    <p class="flex items-start gap-2 rounded-xl px-3 py-2 text-sm {verifyResult.ok ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300' : 'bg-rose-50 text-rose-700 dark:bg-rose-500/10 dark:text-rose-300'}">
      <Icon name={verifyResult.ok ? 'check' : 'warning'} size={16} />
      <span>
        {verifyResult.ok ? t('admin.audit.verified', { count: formatNumber(verifyResult.checked) }) : t('admin.audit.broken', { id: String(verifyResult.brokenAt ?? '') })}
        {#if verifyResult.message}<span class="block text-xs opacity-80">{verifyResult.message}</span>{/if}
      </span>
    </p>
  {/if}

  <div class="card card-pad">
    <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
      <div>
        <label class="label" for="a-action">{t('admin.audit.filterAction')}</label>
        <input id="a-action" class="input" bind:value={filterAction} on:keydown={(e) => e.key === 'Enter' && search()} />
      </div>
      <div>
        <label class="label" for="a-actor">{t('admin.audit.filterActor')}</label>
        <input id="a-actor" class="input" bind:value={filterActor} on:keydown={(e) => e.key === 'Enter' && search()} />
      </div>
      <div>
        <label class="label" for="a-result">{t('admin.audit.result')}</label>
        <select id="a-result" class="input" bind:value={filterResult}>
          <option value="">{t('common.all')}</option>
          <option value="success">success</option>
          <option value="failure">failure</option>
          <option value="denied">denied</option>
        </select>
      </div>
      <div>
        <label class="label" for="a-from">{t('common.from')}</label>
        <input id="a-from" class="input" type="date" bind:value={filterFrom} />
      </div>
      <div>
        <label class="label" for="a-to">{t('common.to')}</label>
        <input id="a-to" class="input" type="date" bind:value={filterTo} />
      </div>
    </div>
    <div class="mt-3 flex justify-end gap-2">
      <button type="button" class="btn-ghost btn-sm" on:click={reset}><Icon name="refresh" size={15} />{t('common.refresh')}</button>
      <button type="button" class="btn-primary btn-sm" on:click={search}><Icon name="search" size={15} />{t('common.search')}</button>
    </div>
  </div>

  {#if loading}
    <div class="space-y-2">{#each Array(8) as _}<div class="skeleton h-12"></div>{/each}</div>
  {:else if entries.length === 0}
    <div class="card"><EmptyState icon="shield" title={t('admin.audit.empty')} /></div>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{t('admin.audit.time')}</th>
            <th>{t('admin.audit.actor')}</th>
            <th>{t('admin.audit.action')}</th>
            <th>{t('admin.audit.object')}</th>
            <th>{t('admin.audit.result')}</th>
            <th class="text-right">{t('admin.audit.details')}</th>
          </tr>
        </thead>
        <tbody>
          {#each entries as entry (entry.id)}
            <tr>
              <td class="whitespace-nowrap text-xs text-slate-500 dark:text-slate-400">{formatDateTime(entry.ts)}</td>
              <td class="text-sm text-slate-700 dark:text-slate-200">{entry.actor || t('common.unknownUser')}</td>
              <td class="text-sm font-medium text-slate-800 dark:text-slate-100">{entry.action}</td>
              <td class="text-xs text-slate-500 dark:text-slate-400">{entry.objectType}{entry.objectId ? ` ${entry.objectId}` : ''}</td>
              <td><span class={resultTone(entry.result)}>{entry.result}</span></td>
              <td class="text-right">
                <button type="button" class="btn-ghost btn-sm" on:click={() => openDetail(entry)} title={t('admin.audit.details')}><Icon name="eye" size={15} /></button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <Pagination {page} {limit} {total} on:change={(e) => changePage(e.detail)} />
  {/if}
</div>

{#if detailOpen && detailEntry}
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-slate-900/50 p-4" on:click={() => (detailOpen = false)}>
    <div class="card card-pad w-full max-w-lg" on:click|stopPropagation>
      <div class="flex items-start justify-between gap-3">
        <h3 class="text-base font-semibold text-slate-900 dark:text-white">{detailEntry.action}</h3>
        <button type="button" class="btn-ghost btn-sm" on:click={() => (detailOpen = false)}><Icon name="close" size={16} /></button>
      </div>
      <dl class="mt-4 space-y-2 text-sm">
        <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.audit.time')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatDateTime(detailEntry.ts)}</dd></div>
        <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.audit.actor')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{detailEntry.actor || t('common.unknownUser')}</dd></div>
        <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.audit.object')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{detailEntry.objectType} {detailEntry.objectId}</dd></div>
        <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.audit.result')}</dt><dd><span class={resultTone(detailEntry.result)}>{detailEntry.result}</span></dd></div>
        <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">ID</dt><dd class="font-mono text-xs text-slate-600 dark:text-slate-300">{detailEntry.id}</dd></div>
      </dl>
      {#if detailEntry.details}
        <div class="mt-4">
          <span class="label">{t('admin.audit.details')}</span>
          <pre class="max-h-60 overflow-auto rounded-xl bg-slate-100 p-3 font-mono text-xs text-slate-700 dark:bg-slate-800 dark:text-slate-200">{detailsText(detailEntry.details)}</pre>
        </div>
      {/if}
      <div class="mt-4 border-t border-slate-100 pt-3 dark:border-slate-800">
        <p class="break-all font-mono text-[10px] text-slate-400 dark:text-slate-500">{detailEntry.entryHash}</p>
      </div>
    </div>
  </div>
{/if}
