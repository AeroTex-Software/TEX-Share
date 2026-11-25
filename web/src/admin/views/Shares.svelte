<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, downloadHref, errorText } from '../../lib/api';
  import { formatBytes, formatDateTime, formatNumber, toDatetimeLocal, fromDatetimeLocal } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { DailyStat, Share, ShareFile } from '../../lib/types';
  import BarChart from '../../components/BarChart.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import Pagination from '../../components/Pagination.svelte';
  import StatusBadge from '../../components/StatusBadge.svelte';

  let shares: Share[] = [];
  let total = 0;
  let page = 1;
  const limit = 25;
  let query = '';
  let statusFilter = 'all';
  let typeFilter = 'all';
  let trash = false;
  let loading = true;

  let detailOpen = false;
  let detail: Share | null = null;
  let detailLoading = false;
  let detailError = '';
  let editTitle = '';
  let editDescription = '';
  let editExpiry = '';
  let editDownloadLimit = 0;
  let saving = false;

  let lockOpen = false;
  let lockTarget: Share | null = null;
  let lockReason = '';
  let lockBusy = false;

  let tokenOpen = false;
  let tokenShare: Share | null = null;
  let newToken = '';
  let newUrlPath = '';

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    try {
      const params = new URLSearchParams({ page: String(page), limit: String(limit) });
      if (query.trim()) params.set('q', query.trim());
      if (statusFilter !== 'all') params.set('status', statusFilter);
      if (typeFilter !== 'all') params.set('type', typeFilter);
      if (trash) params.set('trash', '1');
      const res = await api<{ shares: Share[]; total: number }>(`/shares?${params.toString()}`);
      shares = res.shares || [];
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

  function changePage(next: number): void {
    page = next;
    void load();
  }

  function toggleTrash(): void {
    trash = !trash;
    statusFilter = 'all';
    page = 1;
    void load();
  }

  function canManage(share: Share): boolean {
    return share.status !== 'deleted' && share.status !== 'expired';
  }

  async function openDetail(share: Share): Promise<void> {
    detailOpen = true;
    detail = null;
    detailLoading = true;
    detailError = '';
    try {
      const res = await api<{ share: Share }>(`/shares/${share.id}/content`);
      detail = res.share;
      editTitle = res.share.title || '';
      editDescription = res.share.description || '';
      editExpiry = toDatetimeLocal(res.share.expiresAt);
      editDownloadLimit = res.share.downloadLimit || 0;
    } catch (err) {
      detailError = errorText(err);
    } finally {
      detailLoading = false;
    }
  }

  async function saveDetail(): Promise<void> {
    if (!detail) return;
    saving = true;
    detailError = '';
    try {
      const body: Record<string, unknown> = {
        expiresAt: editExpiry ? fromDatetimeLocal(editExpiry) : 0,
        downloadLimit: Number(editDownloadLimit) || 0
      };
      if (!detail.isE2ee) {
        body.title = editTitle;
        body.description = editDescription;
      }
      const res = await api<{ share: Share }>(`/shares/${detail.id}`, { method: 'PATCH', body });
      detail = res.share;
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      detailError = errorText(err);
    } finally {
      saving = false;
    }
  }

  async function togglePause(share: Share): Promise<void> {
    const next = share.status === 'paused' ? 'active' : 'paused';
    try {
      await api(`/shares/${share.id}`, { method: 'PATCH', body: { status: next } });
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  function openLock(share: Share): void {
    lockTarget = share;
    lockReason = share.adminLockReason || '';
    lockOpen = true;
  }

  async function submitLock(): Promise<void> {
    if (!lockTarget) return;
    lockBusy = true;
    try {
      await api(`/shares/${lockTarget.id}`, {
        method: 'PATCH',
        body: { adminLocked: !lockTarget.adminLocked, adminLockReason: lockReason }
      });
      lockOpen = false;
      toast(t('common.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      lockBusy = false;
    }
  }

  async function rotateToken(share: Share): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.shares.rotateToken'), message: t('admin.shares.rotateTokenConfirm'), danger: true });
    if (!ok) return;
    try {
      const res = await api<{ token: string; urlPath: string }>(`/shares/${share.id}/rotate-token`, { method: 'POST', body: {} });
      tokenShare = share;
      newToken = res.token;
      newUrlPath = res.urlPath;
      tokenOpen = true;
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function copyToken(): Promise<void> {
    const url = newUrlPath ? `${window.location.origin.replace(/:\d+$/, '')}${newUrlPath}` : newToken;
    try {
      await copyText(url);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }

  async function trashShare(share: Share): Promise<void> {
    const ok = await confirmDialog({ title: t('shares.trash'), message: t('shares.trashConfirm'), danger: true });
    if (!ok) return;
    try {
      await api(`/shares/${share.id}`, { method: 'DELETE' });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function restoreShare(share: Share): Promise<void> {
    const ok = await confirmDialog({ title: t('shares.restore'), message: t('shares.restoreHint') });
    if (!ok) return;
    try {
      await api(`/shares/${share.id}/restore`, { method: 'POST', body: { expiresAt: 0 } });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function purgeShare(share: Share): Promise<void> {
    const ok = await confirmDialog({ title: t('shares.purge'), message: t('shares.purgeConfirm'), danger: true });
    if (!ok) return;
    try {
      await api(`/shares/${share.id}/purge`, { method: 'DELETE' });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  function downloadFile(file: ShareFile): void {
    if (!detail) return;
    window.location.href = downloadHref(`/shares/${detail.id}/files/${file.id}/download`);
  }

  $: detailChart = (detail?.daily || []).map((d: DailyStat) => ({ label: d.day, value: d.downloads })).reverse();
</script>

<div class="space-y-4">
  <div class="flex flex-wrap items-center gap-2">
    <div class="relative min-w-[12rem] flex-1">
      <span class="pointer-events-none absolute inset-y-0 left-0 flex w-10 items-center justify-center text-slate-400"><Icon name="search" size={16} /></span>
      <input class="input pl-10" placeholder={t('admin.shares.search')} bind:value={query} on:keydown={(e) => e.key === 'Enter' && search()} />
    </div>
    {#if !trash}
      <select class="input w-auto" bind:value={statusFilter} on:change={search}>
        <option value="all">{t('common.all')}</option>
        <option value="active">{t('shares.status.active')}</option>
        <option value="paused">{t('shares.status.paused')}</option>
        <option value="expired">{t('shares.status.expired')}</option>
      </select>
    {/if}
    <select class="input w-auto" bind:value={typeFilter} on:change={search}>
      <option value="all">{t('common.all')}</option>
      <option value="file">{t('create.type.file')}</option>
      <option value="folder">{t('create.type.folder')}</option>
      <option value="media">{t('create.type.media')}</option>
      <option value="link">{t('create.type.link')}</option>
      <option value="text">{t('create.type.text')}</option>
    </select>
    <button type="button" class="btn-secondary" on:click={search}><Icon name="search" size={16} />{t('common.search')}</button>
    <button type="button" class="btn-ghost {trash ? 'text-brand-600 dark:text-brand-400' : ''}" on:click={toggleTrash}><Icon name="trash" size={16} />{t('shares.filter.trash')}</button>
  </div>

  {#if loading}
    <div class="space-y-2">{#each Array(6) as _}<div class="skeleton h-16"></div>{/each}</div>
  {:else if shares.length === 0}
    <div class="card"><EmptyState icon="shares" title={trash ? t('shares.emptyTrash') : t('shares.empty')} /></div>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{t('common.name')}</th>
            <th>{t('admin.shares.owner')}</th>
            <th>{t('common.type')}</th>
            <th>{t('common.size')}</th>
            <th>{t('shares.downloads')}</th>
            <th>{t('common.status')}</th>
            <th class="text-right">{t('common.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {#each shares as share (share.id)}
            <tr>
              <td class="max-w-[16rem]">
                <button type="button" class="block w-full truncate text-left font-medium text-slate-800 hover:text-brand-600 dark:text-slate-100 dark:hover:text-brand-400" on:click={() => openDetail(share)}>
                  {share.title || share.slug}
                </button>
                <p class="truncate text-xs text-slate-500 dark:text-slate-400">{share.domainHostname}{share.namespace ? `/${share.namespace}` : ''}/{share.slug}</p>
              </td>
              <td class="text-sm text-slate-600 dark:text-slate-300">@{share.ownerUsername || t('common.nobody')}</td>
              <td><span class="badge-slate">{share.type}</span>{#if share.isE2ee}<span class="badge-brand ml-1"><Icon name="lock" size={11} /></span>{/if}</td>
              <td class="text-sm text-slate-600 dark:text-slate-300">{formatBytes(share.sizeBytes)}</td>
              <td class="text-sm text-slate-600 dark:text-slate-300">{formatNumber(share.downloadCount)}{share.downloadLimit > 0 ? ` / ${formatNumber(share.downloadLimit)}` : ''}</td>
              <td>
                <div class="flex flex-wrap gap-1">
                  <StatusBadge status={share.adminLocked ? 'locked' : share.effectiveStatus} size="sm" />
                  {#if share.quarantine}<StatusBadge status="quarantined" size="sm" />{/if}
                </div>
              </td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  {#if canManage(share)}
                    <button type="button" class="btn-ghost btn-sm" title={t('shares.pause')} on:click={() => togglePause(share)}>
                      <Icon name={share.status === 'paused' ? 'play' : 'pause'} size={15} />
                    </button>
                    <button type="button" class="btn-ghost btn-sm {share.adminLocked ? 'text-amber-600 dark:text-amber-400' : ''}" title={share.adminLocked ? t('admin.shares.unlock') : t('admin.shares.lock')} on:click={() => openLock(share)}>
                      <Icon name={share.adminLocked ? 'unlock' : 'lock'} size={15} />
                    </button>
                    <button type="button" class="btn-ghost btn-sm" title={t('admin.shares.rotateToken')} on:click={() => rotateToken(share)}><Icon name="rotate" size={15} /></button>
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('shares.trash')} on:click={() => trashShare(share)}><Icon name="trash" size={15} /></button>
                  {:else}
                    <button type="button" class="btn-ghost btn-sm" title={t('shares.restore')} on:click={() => restoreShare(share)}><Icon name="restore" size={15} /></button>
                    <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('shares.purge')} on:click={() => purgeShare(share)}><Icon name="trash" size={15} /></button>
                  {/if}
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <Pagination {page} {limit} {total} on:change={(e) => changePage(e.detail)} />
  {/if}
</div>

<!-- Detail -->
<Modal open={detailOpen} title={t('admin.shares.content')} size="xl" on:close={() => (detailOpen = false)}>
  {#if detailLoading}
    <div class="py-10"><div class="skeleton mx-auto h-8 w-1/2"></div></div>
  {:else if detailError && !detail}
    <p class="flex items-center gap-2 text-sm text-rose-600 dark:text-rose-400"><Icon name="error" size={18} />{detailError}</p>
  {:else if detail}
    <div class="space-y-5">
      {#if detail.encryptedOnly}
        <p class="flex items-start gap-2 rounded-xl bg-brand-50 px-3 py-2 text-sm text-brand-800 dark:bg-brand-500/10 dark:text-brand-200">
          <Icon name="lock" size={16} /><span>{t('admin.shares.e2eeNotice')}</span>
        </p>
      {/if}

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <span class="label">{t('common.status')}</span>
          <div class="flex flex-wrap gap-1"><StatusBadge status={detail.adminLocked ? 'locked' : detail.effectiveStatus} /></div>
        </div>
        <div>
          <span class="label">{t('admin.shares.owner')}</span>
          <p class="text-sm text-slate-700 dark:text-slate-200">@{detail.ownerUsername || t('common.nobody')}</p>
        </div>
        <div>
          <span class="label">{t('common.size')}</span>
          <p class="text-sm text-slate-700 dark:text-slate-200">{formatBytes(detail.sizeBytes)}</p>
        </div>
        <div>
          <span class="label">{t('shares.downloads')}</span>
          <p class="text-sm text-slate-700 dark:text-slate-200">{formatNumber(detail.downloadCount)} / {detail.downloadLimit > 0 ? formatNumber(detail.downloadLimit) : t('common.unlimited')}</p>
        </div>
      </div>

      {#if detailChart.length > 0}
        <div>
          <span class="label">{t('shares.statsLast30')}</span>
          <BarChart data={detailChart} tone="indigo" format={(v) => formatNumber(v)} />
        </div>
      {/if}

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="sh-expiry">{t('detail.expiry')}</label>
          <input id="sh-expiry" class="input" type="datetime-local" bind:value={editExpiry} disabled={saving} />
        </div>
        <div>
          <label class="label" for="sh-limit">{t('detail.downloadLimit')}</label>
          <input id="sh-limit" class="input" type="number" min="0" bind:value={editDownloadLimit} disabled={saving} />
        </div>
      </div>

      {#if !detail.isE2ee}
        <div>
          <label class="label" for="sh-title">{t('detail.titleField')}</label>
          <input id="sh-title" class="input" bind:value={editTitle} disabled={saving} />
        </div>
        <div>
          <label class="label" for="sh-desc">{t('detail.descriptionField')}</label>
          <textarea id="sh-desc" class="input min-h-[5rem]" bind:value={editDescription} disabled={saving}></textarea>
        </div>
      {/if}

      <div>
        <span class="label">{t('admin.shares.files')}</span>
        {#if !detail.files || detail.files.length === 0}
          <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.shares.noFiles')}</p>
        {:else}
          <ul class="divide-y divide-slate-100 rounded-xl border border-slate-200 dark:divide-slate-800 dark:border-slate-800">
            {#each detail.files as file (file.id)}
              <li class="flex items-center gap-3 px-4 py-2.5">
                <Icon name="file" size={16} />
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm text-slate-700 dark:text-slate-200">{file.name || t('detail.encryptedFile')}</span>
                  <span class="block text-xs text-slate-500 dark:text-slate-400">{formatBytes(file.sizeBytes)}</span>
                </span>
                <button type="button" class="btn-secondary btn-sm" title={t('admin.shares.downloadFile')} on:click={() => downloadFile(file)}><Icon name="download" size={15} /></button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      {#if detailError}
        <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300"><Icon name="error" size={16} /><span>{detailError}</span></p>
      {/if}

      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={() => (detailOpen = false)}>{t('common.close')}</button>
        <button type="button" class="btn-primary" disabled={saving} on:click={saveDetail}>
          {#if saving}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </div>
  {/if}
</Modal>

<!-- Lock -->
<Modal open={lockOpen} title={lockTarget?.adminLocked ? t('admin.shares.unlock') : t('admin.shares.lock')} size="sm" on:close={() => (lockOpen = false)}>
  {#if lockTarget}
    <p class="text-sm text-slate-500 dark:text-slate-400">{lockTarget.adminLocked ? t('admin.shares.unlockConfirm') : t('admin.shares.lockConfirm')}</p>
    {#if !lockTarget.adminLocked}
      <div class="mt-4">
        <label class="label" for="lock-reason">{t('admin.shares.lockReason')}</label>
        <input id="lock-reason" class="input" bind:value={lockReason} disabled={lockBusy} />
      </div>
    {/if}
    <div class="mt-5 flex justify-end gap-2">
      <button type="button" class="btn-secondary" on:click={() => (lockOpen = false)}>{t('common.cancel')}</button>
      <button type="button" class={lockTarget.adminLocked ? 'btn-primary' : 'btn-danger'} disabled={lockBusy} on:click={submitLock}>
        {#if lockBusy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="lock" size={16} />{/if}
        {t('common.confirm')}
      </button>
    </div>
  {/if}
</Modal>

<!-- New token -->
<Modal open={tokenOpen} title={t('admin.shares.newToken')} size="sm" on:close={() => (tokenOpen = false)}>
  <p class="text-sm text-slate-500 dark:text-slate-400">{t('detail.tokenUnavailable')}</p>
  <div class="mt-3 flex items-center gap-2 rounded-xl bg-slate-100 px-3 py-3 dark:bg-slate-800">
    <code class="min-w-0 flex-1 break-all font-mono text-xs text-slate-800 dark:text-slate-100">{newUrlPath}{newToken ? `/${newToken}` : ''}</code>
    <button type="button" class="btn-secondary btn-sm" on:click={copyToken}><Icon name="copy" size={15} />{t('common.copy')}</button>
  </div>
  <div class="mt-4 flex justify-end"><button type="button" class="btn-primary" on:click={() => (tokenOpen = false)}>{t('common.close')}</button></div>
</Modal>
