<script lang="ts">
  import { onMount } from 'svelte';
  import { api, downloadHref, errorText } from '../../lib/api';
  import { formatBytes, formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { Backup } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Toggle from '../../components/Toggle.svelte';

  let backups: Backup[] = [];
  let directory = '';
  let loading = true;
  let creating = false;
  let encryptManual = true;
  let busyId = '';

  // Settings
  let schedule = 'off';
  let hour = 3;
  let retention = 14;
  let path = '';
  let encryptAuto = true;
  let savingSettings = false;

  let restoreOpen = false;
  let restoreId = '';
  let restoreFilename = '';
  let restoreBusy = false;
  let restoreError = '';

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    try {
      const [list, settings] = await Promise.all([
        api<{ backups: Backup[]; directory: string }>('/backups'),
        api<{ backup: { schedule: string; hour: number; retention: number; directory: string; encrypt: boolean } }>('/settings')
      ]);
      backups = list.backups || [];
      directory = list.directory || '';
      const b = settings.backup;
      if (b) {
        schedule = b.schedule || 'off';
        hour = b.hour ?? 3;
        retention = b.retention ?? 14;
        encryptAuto = Boolean(b.encrypt);
      }
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  function kindLabel(kind: string): string {
    if (kind === 'auto') return t('admin.backups.kind.auto');
    if (kind === 'pre-restore') return t('admin.backups.kind.pre-restore');
    return t('admin.backups.kind.manual');
  }

  async function createBackup(): Promise<void> {
    creating = true;
    try {
      await api('/backups', { method: 'POST', body: { encrypt: encryptManual } });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      creating = false;
    }
  }

  async function removeBackup(item: Backup): Promise<void> {
    const ok = await confirmDialog({
      title: t('admin.backups.delete'),
      message: t('admin.backups.deleteConfirm', { filename: item.filename }),
      danger: true
    });
    if (!ok) return;
    busyId = item.id;
    try {
      await api(`/backups/${item.id}`, { method: 'DELETE' });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busyId = '';
    }
  }

  async function prune(): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.backups.prune'), message: t('admin.backups.pruneConfirm'), danger: true });
    if (!ok) return;
    try {
      await api('/backups/prune', { method: 'POST', body: {} });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  function openRestore(item: Backup): void {
    restoreId = item.id;
    restoreFilename = item.filename;
    restoreError = '';
    restoreOpen = true;
  }

  async function confirmRestore(): Promise<void> {
    restoreBusy = true;
    restoreError = '';
    try {
      const fd = new FormData();
      fd.append('id', restoreId);
      await api('/backups/restore', { method: 'POST', body: fd });
      restoreOpen = false;
      toast(t('admin.backups.restored'), 'success');
      setTimeout(() => window.location.reload(), 2500);
    } catch (err) {
      restoreError = errorText(err);
    } finally {
      restoreBusy = false;
    }
  }

  async function uploadRestore(event: Event): Promise<void> {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    restoreBusy = true;
    restoreError = '';
    try {
      const fd = new FormData();
      fd.append('file', file);
      await api('/backups/restore', { method: 'POST', body: fd });
      restoreOpen = false;
      toast(t('admin.backups.restored'), 'success');
      setTimeout(() => window.location.reload(), 2500);
    } catch (err) {
      restoreError = errorText(err);
    } finally {
      restoreBusy = false;
    }
  }

  async function saveSettings(): Promise<void> {
    savingSettings = true;
    try {
      await api('/settings', {
        method: 'PATCH',
        body: {
          settings: {
            backup_schedule: schedule,
            backup_hour: String(hour),
            backup_retention_count: String(retention),
            backup_encrypt_auto: encryptAuto ? '1' : '0'
          }
        }
      });
      toast(t('admin.settings.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      savingSettings = false;
    }
  }

  function download(item: Backup): void {
    window.location.href = downloadHref(`/backups/${item.id}/download`);
  }
</script>

<div class="space-y-5">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.backups.subtitle')}</p>
    <div class="flex flex-wrap items-center gap-2">
      <Toggle bind:checked={encryptManual} label={t('admin.backups.encrypt')} />
      <button type="button" class="btn-primary" on:click={createBackup} disabled={creating}>
        {#if creating}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="plus" size={16} />{/if}
        {creating ? t('admin.backups.creating') : t('admin.backups.create')}
      </button>
    </div>
  </div>

  {#if loading}
    <div class="space-y-2">{#each Array(4) as _}<div class="skeleton h-16"></div>{/each}</div>
  {:else if backups.length === 0}
    <div class="card"><EmptyState icon="backup" title={t('admin.backups.empty')} message={directory} /></div>
  {:else}
    <div class="table-wrap">
      <table class="table">
        <thead>
          <tr>
            <th>{t('common.name')}</th>
            <th>{t('admin.backups.schedule')}</th>
            <th>{t('common.size')}</th>
            <th>{t('common.status')}</th>
            <th>{t('common.date')}</th>
            <th class="text-right">{t('common.actions')}</th>
          </tr>
        </thead>
        <tbody>
          {#each backups as item (item.id)}
            <tr>
              <td class="max-w-[18rem]">
                <p class="truncate font-medium text-slate-800 dark:text-slate-100">{item.filename}</p>
                {#if item.error}<p class="truncate text-xs text-rose-600 dark:text-rose-400">{item.error}</p>{/if}
              </td>
              <td>
                <span class="badge-slate">{kindLabel(item.kind)}</span>
                {#if item.encrypted}<span class="badge-brand ml-1"><Icon name="lock" size={11} /></span>{/if}
              </td>
              <td class="text-sm text-slate-600 dark:text-slate-300">{formatBytes(item.sizeBytes)}</td>
              <td>
                {#if item.status === 'complete'}<span class="badge-green">{t('admin.common.success')}</span>{:else}<span class="badge-red">{item.status}</span>{/if}
              </td>
              <td class="text-xs text-slate-500 dark:text-slate-400">{formatDateTime(item.createdAt)}</td>
              <td>
                <div class="flex items-center justify-end gap-1">
                  <button type="button" class="btn-ghost btn-sm" title={t('admin.backups.download')} on:click={() => download(item)}><Icon name="download" size={15} /></button>
                  <button type="button" class="btn-ghost btn-sm text-amber-600 dark:text-amber-400" title={t('admin.backups.restore')} on:click={() => openRestore(item)}><Icon name="restore" size={15} /></button>
                  <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" title={t('admin.backups.delete')} on:click={() => removeBackup(item)} disabled={busyId === item.id}><Icon name="trash" size={15} /></button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  <!-- Settings -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="settings" size={18} />{t('admin.settings.backups')}</h2>
    <div class="mt-4 grid gap-4 sm:grid-cols-3">
      <div>
        <label class="label" for="bk-sched">{t('admin.backups.schedule')}</label>
        <select id="bk-sched" class="input" bind:value={schedule} disabled={savingSettings}>
          <option value="off">{t('admin.backups.schedule.off')}</option>
          <option value="daily">{t('admin.backups.schedule.daily')}</option>
          <option value="weekly">{t('admin.backups.schedule.weekly')}</option>
        </select>
      </div>
      <div>
        <label class="label" for="bk-hour">{t('admin.backups.hour')}</label>
        <input id="bk-hour" class="input" type="number" min="0" max="23" bind:value={hour} disabled={savingSettings} />
      </div>
      <div>
        <label class="label" for="bk-ret">{t('admin.backups.retention')}</label>
        <input id="bk-ret" class="input" type="number" min="1" bind:value={retention} disabled={savingSettings} />
      </div>
    </div>
    <div class="mt-4"><Toggle bind:checked={encryptAuto} label={t('admin.backups.encryptAuto')} /></div>
    <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
      <p class="hint">{t('admin.backups.directory')}: <code class="font-mono">{directory}</code></p>
      <div class="flex gap-2">
        <button type="button" class="btn-secondary" on:click={prune}><Icon name="trash" size={16} />{t('admin.backups.prune')}</button>
        <button type="button" class="btn-primary" on:click={saveSettings} disabled={savingSettings}>
          {#if savingSettings}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
          {t('common.save')}
        </button>
      </div>
    </div>
  </section>

  <!-- Upload restore -->
  <section class="card card-pad">
    <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="restore" size={18} />{t('admin.backups.restoreUpload')}</h2>
    <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.backups.restoreUploadHint')}</p>
    <label class="btn-secondary btn-sm mt-3 cursor-pointer">
      {#if restoreBusy}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="upload" size={15} />{/if}
      {t('admin.backups.restoreUpload')}
      <input type="file" class="hidden" accept=".tar.gz,.tgz,application/gzip,application/octet-stream" on:change={uploadRestore} disabled={restoreBusy} />
    </label>
    {#if restoreError}<p class="mt-3 flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300"><Icon name="error" size={16} /><span>{restoreError}</span></p>{/if}
  </section>
</div>

<!-- Restore confirm -->
{#if restoreOpen}
  <div class="fixed inset-0 z-40 flex items-center justify-center bg-slate-900/50 p-4">
    <div class="card card-pad w-full max-w-md">
      <h3 class="text-base font-semibold text-slate-900 dark:text-white">{t('admin.backups.restore')}</h3>
      <p class="mt-2 flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">
        <Icon name="warning" size={16} /><span>{t('admin.backups.warning')}</span>
      </p>
      <p class="mt-3 text-sm text-slate-600 dark:text-slate-300">{t('admin.backups.restoreConfirm', { filename: restoreFilename })}</p>
      {#if restoreError}<p class="mt-3 text-sm text-rose-600 dark:text-rose-400">{restoreError}</p>{/if}
      <div class="mt-5 flex justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={() => (restoreOpen = false)} disabled={restoreBusy}>{t('common.cancel')}</button>
        <button type="button" class="btn-danger" on:click={confirmRestore} disabled={restoreBusy}>
          {#if restoreBusy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="restore" size={16} />{/if}
          {t('admin.backups.restore')}
        </button>
      </div>
    </div>
  </div>
{/if}
