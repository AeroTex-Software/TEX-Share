<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { formatBytes, formatDateTime, formatNumber, fromDatetimeLocal, toDatetimeLocal } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { ownerShareUrl } from '../../lib/links';
  import { navigate, replaceQuery, route } from '../../lib/router';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { Share } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import StatusBadge from '../../components/StatusBadge.svelte';

  interface ShareSummary {
    id: string;
    type: string;
    title: string;
    slug: string;
    domainHostname: string;
    isE2ee: boolean;
    status: string;
    effectiveStatus: string;
    adminLocked: boolean;
    quarantine: boolean;
    sizeBytes: number;
    viewCount: number;
    downloadCount: number;
    downloadLimit: number;
    expiresAt: number;
    createdAt: number;
    updatedAt: number;
    purgeAfter: number;
    passwordSet: boolean;
    urlPath: string;
  }

  const filters = [
    { key: 'all', label: 'common.all' },
    { key: 'active', label: 'shares.active' },
    { key: 'paused', label: 'shares.paused' },
    { key: 'trash', label: 'shares.trash' }
  ];

  let items: ShareSummary[] = [];
  let total = 0;
  let page = 1;
  let limit = 20;
  let filter = 'all';
  let query = '';
  let loading = true;
  let busyId = '';

  let restoreTarget: ShareSummary | null = null;
  let restoreExpiry = '';
  let restoring = false;

  let searchTimer: ReturnType<typeof setTimeout> | null = null;

  onMount(() => {
    filter = $route.query.get('filter') || 'all';
    query = $route.query.get('q') || '';
    page = Number($route.query.get('page') || '1') || 1;
    void load();
  });

  async function load(): Promise<void> {
    loading = true;
    try {
      const params = new URLSearchParams();
      params.set('page', String(page));
      params.set('limit', String(limit));
      if (filter === 'trash') params.set('trash', '1');
      else if (filter !== 'all') params.set('status', filter);
      if (query.trim()) params.set('q', query.trim());
      const res = await api<{ shares: ShareSummary[]; total: number; page: number; limit: number }>(`/shares?${params.toString()}`);
      items = res.shares || [];
      total = res.total || 0;
      page = res.page || 1;
      limit = res.limit || limit;
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  function applyFilter(next: string): void {
    filter = next;
    page = 1;
    replaceQuery({ filter: next === 'all' ? null : next, page: null, q: query.trim() || null });
    void load();
  }

  function onSearchInput(): void {
    if (searchTimer) clearTimeout(searchTimer);
    searchTimer = setTimeout(() => {
      page = 1;
      replaceQuery({ q: query.trim() || null, page: null });
      void load();
    }, 350);
  }

  function onPage(next: number): void {
    page = next;
    replaceQuery({ page: next > 1 ? next : null });
    void load();
  }

  function typeIcon(type: string): string {
    switch (type) {
      case 'folder': return 'folder';
      case 'link': return 'link';
      case 'text': return 'text';
      case 'media': return 'media';
      default: return 'file';
    }
  }

  async function copyLink(share: ShareSummary): Promise<void> {
    const url = ownerShareUrl(share as unknown as Share);
    if (!url) {
      toast(t('shares.linkUnavailable'), 'error');
      return;
    }
    try {
      await copyText(url);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  async function setStatus(share: ShareSummary, status: 'active' | 'paused'): Promise<void> {
    busyId = share.id;
    try {
      await api(`/shares/${share.id}`, { method: 'PATCH', body: { status } });
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busyId = '';
    }
  }

  async function trashShare(share: ShareSummary): Promise<void> {
    const ok = await confirmDialog({
      title: t('shares.trashConfirm'),
      message: share.title || share.slug,
      confirmLabel: t('shares.trash'),
      danger: true
    });
    if (!ok) return;
    busyId = share.id;
    try {
      await api(`/shares/${share.id}`, { method: 'DELETE' });
      toast(t('shares.trashed'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busyId = '';
    }
  }

  function openRestore(share: ShareSummary): void {
    restoreTarget = share;
    restoreExpiry = '';
  }

  async function confirmRestore(): Promise<void> {
    if (!restoreTarget) return;
    restoring = true;
    try {
      await api(`/shares/${restoreTarget.id}/restore`, { method: 'POST', body: { expiresAt: fromDatetimeLocal(restoreExpiry) } });
      toast(t('common.saved'), 'success');
      restoreTarget = null;
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      restoring = false;
    }
  }

  async function purgeShare(share: ShareSummary): Promise<void> {
    const ok = await confirmDialog({
      title: t('shares.purgeConfirm'),
      message: share.title || share.slug,
      confirmLabel: t('shares.purge'),
      danger: true
    });
    if (!ok) return;
    busyId = share.id;
    try {
      await api(`/shares/${share.id}/purge`, { method: 'DELETE' });
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busyId = '';
    }
  }
</script>

<div class="space-y-4">
  <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
    <div class="flex flex-wrap gap-1.5">
      {#each filters as item}
        <button
          type="button"
          class="rounded-xl px-3 py-1.5 text-sm font-medium transition {filter === item.key
            ? 'bg-brand-600 text-white'
            : 'bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:hover:bg-slate-700'}"
          on:click={() => applyFilter(item.key)}
        >
          {t(item.label)}
        </button>
      {/each}
    </div>
    <div class="flex items-center gap-2">
      <div class="relative">
        <span class="pointer-events-none absolute inset-y-0 left-0 flex w-9 items-center justify-center text-slate-400"><Icon name="search" size={16} /></span>
        <input class="input pl-9" placeholder={t('shares.searchPlaceholder')} bind:value={query} on:input={onSearchInput} />
      </div>
      <button type="button" class="btn-primary shrink-0" on:click={() => navigate('/shares/new')}><Icon name="plus" size={16} /><span class="hidden sm:inline">{t('shares.new')}</span></button>
    </div>
  </div>

  {#if loading}
    <div class="space-y-2">
      {#each Array(5) as _}
        <div class="skeleton h-16"></div>
      {/each}
    </div>
  {:else if items.length === 0}
    <div class="card">
      <EmptyState icon="shares" title={filter === 'trash' ? t('shares.emptyTrash') : t('shares.empty')} message={t('dash.quickCreateHint')}>
        {#if filter !== 'trash'}
          <button type="button" class="btn-primary btn-sm mt-1" on:click={() => navigate('/shares/new')}><Icon name="plus" size={15} />{t('shares.new')}</button>
        {/if}
      </EmptyState>
    </div>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{t('shares.title')}</th>
            <th>{t('common.status')}</th>
            <th class="hidden md:table-cell">{t('common.size')}</th>
            <th class="hidden lg:table-cell">{t('shares.views')}</th>
            <th class="hidden lg:table-cell">{t('shares.downloads')}</th>
            <th class="hidden xl:table-cell">{t('shares.expires')}</th>
            <th class="text-right">{t('common.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {#each items as share}
            <tr class={busyId === share.id ? 'opacity-60' : ''}>
              <td>
                <button type="button" class="flex min-w-0 items-center gap-2.5 text-left" on:click={() => navigate(`/shares/${share.id}`)}>
                  <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                    <Icon name={typeIcon(share.type)} size={16} />
                  </span>
                  <span class="min-w-0">
                    <span class="flex items-center gap-1.5">
                      <span class="truncate font-medium text-slate-800 dark:text-slate-100">{share.title || share.slug}</span>
                      {#if share.isE2ee}<Icon name="lock" size={13} class="text-brand-500" />{/if}
                      {#if share.passwordSet}<Icon name="key" size={13} class="text-amber-500" />{/if}
                    </span>
                    <span class="block truncate text-xs text-slate-500 dark:text-slate-400">{share.domainHostname}{share.urlPath}</span>
                  </span>
                </button>
              </td>
              <td>
                <div class="flex flex-col gap-1">
                  <StatusBadge status={share.effectiveStatus || share.status} size="sm" />
                  {#if share.adminLocked}<StatusBadge status="locked" size="sm" />{/if}
                  {#if share.quarantine}<StatusBadge status="quarantined" size="sm" />{/if}
                </div>
              </td>
              <td class="hidden md:table-cell">{formatBytes(share.sizeBytes)}</td>
              <td class="hidden lg:table-cell">{formatNumber(share.viewCount)}</td>
              <td class="hidden lg:table-cell">{formatNumber(share.downloadCount)}{#if share.downloadLimit > 0}<span class="text-slate-400"> / {formatNumber(share.downloadLimit)}</span>{/if}</td>
              <td class="hidden xl:table-cell">{share.expiresAt ? formatDateTime(share.expiresAt) : t('shares.noExpiry')}</td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  {#if filter === 'trash'}
                    <button type="button" class="btn-ghost btn-sm" title={t('shares.restore')} on:click={() => openRestore(share)}><Icon name="restore" size={15} /></button>
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('shares.purge')} on:click={() => purgeShare(share)}><Icon name="trash" size={15} /></button>
                  {:else}
                    <button type="button" class="btn-ghost btn-sm" title={t('shares.copyLink')} on:click={() => copyLink(share)}><Icon name="link" size={15} /></button>
                    {#if share.status === 'paused'}
                      <button type="button" class="btn-ghost btn-sm" title={t('shares.resume')} on:click={() => setStatus(share, 'active')}><Icon name="play" size={15} /></button>
                    {:else}
                      <button type="button" class="btn-ghost btn-sm" title={t('shares.pause')} on:click={() => setStatus(share, 'paused')}><Icon name="pause" size={15} /></button>
                    {/if}
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('shares.trash')} on:click={() => trashShare(share)}><Icon name="trash" size={15} /></button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

    <Pagination {page} {limit} {total} on:change={(e) => onPage(e.detail)} />
  {/if}
</div>

<Modal open={Boolean(restoreTarget)} title={t('shares.restore')} subtitle={t('shares.restoreHint')} size="sm" on:close={() => (restoreTarget = null)}>
  <div class="space-y-4">
    <div>
      <label class="label" for="restore-expiry">{t('shares.restoreExpiry')}</label>
      <input id="restore-expiry" class="input" type="datetime-local" bind:value={restoreExpiry} min={toDatetimeLocal(Math.floor(Date.now() / 1000) + 60)} />
      <p class="hint">{t('shares.restoreNoExpiry')}</p>
    </div>
    <div class="flex justify-end gap-2">
      <button type="button" class="btn-secondary" on:click={() => (restoreTarget = null)}>{t('common.cancel')}</button>
      <button type="button" class="btn-primary" disabled={restoring} on:click={confirmRestore}>
        {#if restoring}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="restore" size={15} />{/if}
        {t('shares.restore')}
      </button>
    </div>
  </div>
</Modal>
