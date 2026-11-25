<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api, errorText } from '../../lib/api';
  import { formatBytes, formatDuration, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { toast } from '../../lib/stores';
  import Icon from '../../components/Icon.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';
  import StatCard from '../../components/StatCard.svelte';

  interface MonitoringResponse {
    monitoring: {
      uptimeSeconds: number;
      goroutines: number;
      heapBytes: number;
      heapSysBytes: number;
      stackBytes: number;
      gcCycles: number;
      cpus: number;
      dbSizeBytes: number;
      errors: number;
      activeUploads: number;
      activeZips: number;
      diskTotalBytes?: number;
      diskFreeBytes?: number;
      diskUsedBytes?: number;
      counts: { users: number; shares: number; activeShares: number; files: number; storageUsedBytes: number; storageReservedBytes: number };
      clamav?: { enabled: boolean; status: string; message?: string; version?: string; lastUpdate?: number; lastScan?: number } | null;
      tunnel?: { enabled: boolean; status: string; message?: string; pid?: number; restarts?: number } | null;
      cloudflare?: { enabled: boolean; configured: boolean; accountId?: string; tunnelName?: string } | null;
    };
    system: { version: string; go: string; os: string; arch: string; cpus: number; startedAt: number };
  }

  let data: MonitoringResponse | null = null;
  let loading = true;
  let refreshSeconds = 5;
  let timer: number | undefined;

  onMount(() => {
    void load();
    schedule();
    return () => stop();
  });

  onDestroy(stop);

  function stop(): void {
    if (timer) window.clearInterval(timer);
    timer = undefined;
  }

  function schedule(): void {
    stop();
    if (refreshSeconds > 0) timer = window.setInterval(load, refreshSeconds * 1000);
  }

  async function load(): Promise<void> {
    try {
      data = await api<MonitoringResponse>('/monitoring');
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  function setRefresh(value: number): void {
    refreshSeconds = value;
    schedule();
  }

  function onRefreshChange(event: Event): void {
    setRefresh(Number((event.currentTarget as HTMLSelectElement).value));
  }

  function serviceTone(state?: string): string {
    if (state === 'available' || state === 'running' || state === 'healthy' || state === 'up') return 'badge-green';
    if (state === 'starting' || state === 'pending') return 'badge-amber';
    if (!state) return 'badge-slate';
    return 'badge-red';
  }
</script>

<div class="space-y-5">
  <div class="flex flex-wrap items-center justify-between gap-3">
    <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.monitoring.subtitle')}</p>
    <div class="flex items-center gap-2">
      <select class="input w-auto" value={refreshSeconds} on:change={onRefreshChange}>
        <option value={0}>{t('common.never')}</option>
        <option value={2}>2s</option>
        <option value={5}>5s</option>
        <option value={10}>10s</option>
        <option value={30}>30s</option>
      </select>
      <button type="button" class="btn-secondary btn-sm" on:click={load}><Icon name="refresh" size={15} />{t('admin.monitoring.refresh')}</button>
    </div>
  </div>

  {#if loading && !data}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{#each Array(4) as _}<div class="skeleton h-28"></div>{/each}</div>
  {:else if data}
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard label={t('admin.monitoring.uptime')} value={formatDuration(data.monitoring.uptimeSeconds)} icon="clock" tone="brand" />
      <StatCard label={t('admin.monitoring.heap')} value={formatBytes(data.monitoring.heapBytes)} hint={`Sys ${formatBytes(data.monitoring.heapSysBytes)}`} icon="memory" tone="indigo" />
      <StatCard label={t('admin.monitoring.goroutines')} value={formatNumber(data.monitoring.goroutines)} hint={`${formatNumber(data.monitoring.cpus)} ${t('admin.monitoring.cpu')}`} icon="cpu" tone="emerald" />
      <StatCard label={t('admin.monitoring.errors')} value={formatNumber(data.monitoring.errors)} icon="warning" tone={data.monitoring.errors > 0 ? 'rose' : 'slate'} />
    </div>

    <div class="grid gap-5 lg:grid-cols-2">
      <div class="card card-pad">
        <h2 class="mb-3 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="database" size={18} />{t('admin.monitoring.counts')}</h2>
        <dl class="space-y-2.5 text-sm">
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.users')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(data.monitoring.counts.users)}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.shares')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(data.monitoring.counts.shares)}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('shares.active')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(data.monitoring.counts.activeShares)}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('admin.monitoring.db')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatBytes(data.monitoring.dbSizeBytes)}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('common.used')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatBytes(data.monitoring.counts.storageUsedBytes)}</dd></div>
          <div class="flex justify-between gap-3"><dt class="text-slate-500 dark:text-slate-400">{t('common.reserved')}</dt><dd class="font-medium text-slate-800 dark:text-slate-100">{formatBytes(data.monitoring.counts.storageReservedBytes)}</dd></div>
        </dl>
      </div>

      <div class="card card-pad">
        <h2 class="mb-3 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="activity" size={18} />{t('admin.monitoring.services')}</h2>
        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3 rounded-xl border border-slate-100 px-3 py-2.5 dark:border-slate-800">
            <span class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200"><Icon name="shield" size={16} />{t('admin.monitoring.clamav')}</span>
            <span class={serviceTone(data.monitoring.clamav?.status)}>{data.monitoring.clamav?.enabled ? data.monitoring.clamav?.status : t('common.disabled')}</span>
          </div>
          <div class="flex items-center justify-between gap-3 rounded-xl border border-slate-100 px-3 py-2.5 dark:border-slate-800">
            <span class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200"><Icon name="network" size={16} />{t('admin.monitoring.tunnel')}</span>
            <span class={serviceTone(data.monitoring.tunnel?.status)}>{data.monitoring.tunnel?.enabled ? data.monitoring.tunnel?.status : t('common.disabled')}</span>
          </div>
          <div class="flex items-center justify-between gap-3 rounded-xl border border-slate-100 px-3 py-2.5 dark:border-slate-800">
            <span class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200"><Icon name="cloud" size={16} />Cloudflare</span>
            <span class={data.monitoring.cloudflare?.configured ? 'badge-green' : 'badge-slate'}>{data.monitoring.cloudflare?.configured ? t('common.enabled') : t('admin.settings.cfNotConfigured')}</span>
          </div>
        </div>

        <div class="mt-4 grid grid-cols-2 gap-3 border-t border-slate-100 pt-4 text-sm dark:border-slate-800">
          <div><p class="text-slate-500 dark:text-slate-400">{t('admin.monitoring.activeUploads')}</p><p class="font-semibold text-slate-800 dark:text-slate-100">{formatNumber(data.monitoring.activeUploads)}</p></div>
          <div><p class="text-slate-500 dark:text-slate-400">{t('admin.monitoring.activeZips')}</p><p class="font-semibold text-slate-800 dark:text-slate-100">{formatNumber(data.monitoring.activeZips)}</p></div>
        </div>
      </div>
    </div>

    {#if data.monitoring.diskTotalBytes}
      <div class="card card-pad">
        <h2 class="mb-3 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="harddrive" size={18} />{t('admin.monitoring.disk')}</h2>
        <ProgressBar
          value={data.monitoring.diskUsedBytes || 0}
          max={data.monitoring.diskTotalBytes || 1}
          label={`${formatBytes(data.monitoring.diskUsedBytes)} / ${formatBytes(data.monitoring.diskTotalBytes)}`}
          tone={(data.monitoring.diskUsedBytes || 0) / (data.monitoring.diskTotalBytes || 1) > 0.9 ? 'rose' : 'brand'}
        />
        <p class="hint">{t('admin.monitoring.diskUsed')}: {formatBytes(data.monitoring.diskUsedBytes)} · {t('common.free')}: {formatBytes(data.monitoring.diskFreeBytes)}</p>
      </div>
    {/if}

    <p class="text-center text-xs text-slate-400 dark:text-slate-500">{data.system.go} · {data.system.os}/{data.system.arch} · v{data.system.version}</p>
  {/if}
</div>
