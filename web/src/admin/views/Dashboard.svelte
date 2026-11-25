<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, errorText } from '../../lib/api';
  import { formatBytes, formatDuration, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { toast } from '../../lib/stores';
  import type { DailyStat } from '../../lib/types';
  import BarChart from '../../components/BarChart.svelte';
  import DonutChart from '../../components/DonutChart.svelte';
  import Icon from '../../components/Icon.svelte';
  import LineChart from '../../components/LineChart.svelte';
  import StatCard from '../../components/StatCard.svelte';

  interface DashboardResponse {
    counts: {
      users: number;
      suspendedUsers: number;
      admins: number;
      shares: number;
      activeShares: number;
      trashedShares: number;
      expiredShares: number;
      domains: number;
      files: number;
      pendingScans: number;
      backups: number;
      newUsers24h: number;
      newShares24h: number;
    };
    usage: {
      storageUsedBytes: number;
      storageReservedBytes: number;
      downloads24h: number;
      views24h: number;
      bytesServed24h: number;
      diskTotalBytes?: number;
      diskFreeBytes?: number;
      diskUsedBytes?: number;
    };
    system: {
      version: string;
      commit: string;
      go: string;
      os: string;
      arch: string;
      cpus: number;
      uptimeSeconds: number;
      heapBytes: number;
      sysBytes: number;
      goroutines: number;
      dbSizeBytes: number;
      errors: number;
      publicUrl?: string;
      adminUrl?: string;
    };
    monitoring: { tunnel: { status?: string; enabled?: boolean } | null; clamav: { status?: string; enabled?: boolean } | null };
  }

  interface TrafficSample {
    tx: number;
    rx: number;
    ts: number;
  }

  const MAX_SAMPLES = 60;
  const POLL_MS = 2000;

  let data: DashboardResponse | null = null;
  let daily: DailyStat[] = [];
  let loading = true;

  // Live traffic: keep the last minute or two of per-second rates.
  let traffic = { up: 0, down: 0 };
  let samples: { up: number; down: number }[] = [];
  let lastSample: TrafficSample | null = null;
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  onMount(async () => {
    await load();
    void pollTraffic();
    pollTimer = setInterval(() => void pollTraffic(), POLL_MS);
  });

  onDestroy(() => {
    if (pollTimer) clearInterval(pollTimer);
  });

  async function load(): Promise<void> {
    loading = true;
    try {
      const [dash, stats] = await Promise.all([
        api<DashboardResponse>('/dashboard'),
        api<{ daily: DailyStat[] }>('/stats?days=14').catch(() => ({ daily: [] }))
      ]);
      data = dash;
      daily = stats.daily || [];
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  async function pollTraffic(): Promise<void> {
    try {
      const next = await api<TrafficSample>('/traffic');
      if (lastSample) {
        const dt = (next.ts - lastSample.ts) / 1000;
        if (dt > 0) {
          // tx = bytes sent to clients (download), rx = bytes received (upload).
          const down = Math.max(0, (next.tx - lastSample.tx) / dt);
          const up = Math.max(0, (next.rx - lastSample.rx) / dt);
          traffic = { up, down };
          samples = [...samples, { up, down }].slice(-MAX_SAMPLES);
        }
      }
      lastSample = next;
    } catch {
      // Transient errors are ignored; the next tick retries.
    }
  }

  function serviceTone(state?: string): string {
    if (state === 'available' || state === 'running' || state === 'healthy' || state === 'up') return 'badge-green';
    if (state === 'starting' || state === 'pending') return 'badge-amber';
    if (!state) return 'badge-slate';
    return 'badge-red';
  }

  $: chart = daily.map((d) => ({ label: d.day, value: d.downloads }));

  $: storageUsed = data?.usage.storageUsedBytes ?? 0;
  $: storageReserved = data?.usage.storageReservedBytes ?? 0;
  $: diskTotal = data?.usage.diskTotalBytes ?? 0;
  $: diskFree = data?.usage.diskFreeBytes ?? 0;
  $: storagePercent = diskTotal > 0 ? Math.min(100, Math.round((storageUsed / diskTotal) * 100)) : 0;
  $: storageSegments =
    storageUsed > 0
      ? [{ label: t('admin.dashboard.storageUsed'), value: diskTotal > 0 ? Math.min(storageUsed, diskTotal) : storageUsed, color: '#6366f1' }]
      : [];

  $: trafficSeries = [
    { label: t('admin.dashboard.trafficDown'), color: '#10b981', points: samples.map((s) => s.down) },
    { label: t('admin.dashboard.trafficUp'), color: '#6366f1', points: samples.map((s) => s.up) }
  ];
  $: trafficActive = traffic.up > 0 || traffic.down > 0 || samples.length > 1;
</script>

{#if loading}
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {#each Array(4) as _}<div class="skeleton h-28"></div>{/each}
    </div>
    <div class="grid gap-5 lg:grid-cols-3">
      <div class="skeleton h-56"></div>
      <div class="skeleton h-56 lg:col-span-2"></div>
    </div>
    <div class="skeleton h-64"></div>
  </div>
{:else if data}
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard label={t('admin.dashboard.users')} value={formatNumber(data.counts.users)} hint={`${formatNumber(data.counts.admins)} ${t('profile.role.admin')} · ${formatNumber(data.counts.suspendedUsers)} ${t('admin.users.suspended')}`} icon="users" tone="brand" />
      <StatCard label={t('admin.dashboard.shares')} value={formatNumber(data.counts.shares)} hint={`${formatNumber(data.counts.activeShares)} ${t('shares.active')}`} icon="shares" tone="indigo" />
      <StatCard label={t('admin.dashboard.storage')} value={formatBytes(storageUsed)} hint={`${formatBytes(storageReserved)} ${t('common.reserved')}`} icon="harddrive" tone="emerald" />
      <StatCard label={t('admin.dashboard.downloads24')} value={formatNumber(data.usage.downloads24h)} hint={`${formatBytes(data.usage.bytesServed24h)}`} icon="download" tone="amber" />
    </div>

    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard label={t('admin.dashboard.newUsers')} value={formatNumber(data.counts.newUsers24h)} icon="user" tone="slate" />
      <StatCard label={t('admin.dashboard.newShares')} value={formatNumber(data.counts.newShares24h)} icon="plus" tone="slate" />
      <StatCard label={t('admin.dashboard.domains')} value={formatNumber(data.counts.domains)} icon="globe" tone="slate" />
      <StatCard label={t('admin.dashboard.pendingScans')} value={formatNumber(data.counts.pendingScans)} icon="shield" tone={data.counts.pendingScans > 0 ? 'amber' : 'slate'} />
    </div>

    <!-- Storage graphic + live traffic graphic -->
    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card card-pad">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="harddrive" size={18} />{t('admin.dashboard.storageTitle')}</h2>
        <div class="mt-4 flex flex-wrap items-center gap-5">
          <DonutChart
            segments={storageSegments}
            centerValue={formatBytes(storageUsed)}
            centerLabel={diskTotal > 0 ? `${storagePercent}%` : t('common.used')}
          />
          <dl class="min-w-[10rem] flex-1 space-y-2 text-sm">
            <div class="flex items-center justify-between gap-3">
              <dt class="flex items-center gap-1.5 text-slate-500 dark:text-slate-400"><span class="h-2.5 w-2.5 rounded-full" style="background:#6366f1"></span>{t('common.used')}</dt>
              <dd class="font-medium tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(storageUsed)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('common.reserved')}</dt>
              <dd class="font-medium tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(storageReserved)}</dd>
            </div>
            {#if diskTotal > 0}
              <div class="flex items-center justify-between gap-3">
                <dt class="text-slate-500 dark:text-slate-400">{t('common.free')}</dt>
                <dd class="font-medium tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(diskFree)}</dd>
              </div>
              <div class="flex items-center justify-between gap-3 border-t border-slate-100 pt-2 dark:border-slate-800">
                <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.diskTotal')}</dt>
                <dd class="font-medium tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(diskTotal)}</dd>
              </div>
            {/if}
          </dl>
        </div>
        <p class="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('admin.dashboard.storageHint')}</p>
      </div>

      <div class="card card-pad lg:col-span-2">
        <div class="mb-1 flex flex-wrap items-center justify-between gap-3">
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="activity" size={18} />{t('admin.dashboard.liveTraffic')}</h2>
          <div class="flex items-center gap-4 text-xs text-slate-500 dark:text-slate-400">
            <span class="flex items-center gap-1.5">
              <span class="h-2.5 w-2.5 rounded-full" style="background:#10b981"></span>{t('admin.dashboard.trafficDown')}
              <strong class="tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(traffic.down)}/s</strong>
            </span>
            <span class="flex items-center gap-1.5">
              <span class="h-2.5 w-2.5 rounded-full" style="background:#6366f1"></span>{t('admin.dashboard.trafficUp')}
              <strong class="tabular-nums text-slate-800 dark:text-slate-100">{formatBytes(traffic.up)}/s</strong>
            </span>
          </div>
        </div>
        {#if trafficActive}
          <div class="mt-3">
            <LineChart series={trafficSeries} height={168} format={(v) => `${formatBytes(v)}/s`} />
          </div>
        {:else}
          <div class="flex items-center justify-center text-xs text-slate-400 dark:text-slate-500" style="height:168px">{t('admin.dashboard.trafficIdle')}</div>
        {/if}
        <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('admin.dashboard.trafficHint')}</p>
      </div>
    </div>

    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card card-pad lg:col-span-2">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-base font-semibold text-slate-900 dark:text-white">{t('shares.statsLast30')}</h2>
          <button type="button" class="btn-ghost btn-sm" on:click={load}><Icon name="refresh" size={15} />{t('common.refresh')}</button>
        </div>
        <BarChart data={chart} tone="brand" format={(v) => formatNumber(v)} />
      </div>

      <div class="space-y-5">
        <div class="card card-pad">
          <h2 class="mb-3 text-base font-semibold text-slate-900 dark:text-white">{t('admin.dashboard.system')}</h2>
          <dl class="space-y-2.5 text-sm">
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.uptime')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatDuration(data.system.uptimeSeconds)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.memory')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatBytes(data.system.heapBytes)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.dbSize')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatBytes(data.system.dbSizeBytes)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.monitoring.goroutines')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(data.system.goroutines)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.errors')}</dt>
              <dd class="font-medium {data.system.errors > 0 ? 'text-rose-600 dark:text-rose-400' : 'text-slate-800 dark:text-slate-100'}">{formatNumber(data.system.errors)}</dd>
            </div>
            <div class="flex items-center justify-between gap-3">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.dashboard.backups')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(data.counts.backups)}</dd>
            </div>
          </dl>
          <div class="mt-4 flex flex-wrap gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
            <span class="badge-slate">{data.system.go}</span>
            <span class="badge-slate">{data.system.os}/{data.system.arch}</span>
            <span class="badge-slate">v{data.system.version}</span>
          </div>
        </div>

        <div class="card card-pad">
          <h2 class="mb-3 text-base font-semibold text-slate-900 dark:text-white">{t('admin.monitoring.services')}</h2>
          <div class="space-y-3">
            <div class="flex items-center justify-between gap-3 rounded-xl border border-slate-100 px-3 py-2.5 dark:border-slate-800">
              <span class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200"><Icon name="shield" size={16} />{t('admin.dashboard.clamav')}</span>
              <span class={serviceTone(data.monitoring?.clamav?.status)}>{data.monitoring?.clamav?.status || t('common.disabled')}</span>
            </div>
            <div class="flex items-center justify-between gap-3 rounded-xl border border-slate-100 px-3 py-2.5 dark:border-slate-800">
              <span class="flex items-center gap-2 text-sm font-medium text-slate-700 dark:text-slate-200"><Icon name="network" size={16} />{t('admin.dashboard.tunnel')}</span>
              <span class={serviceTone(data.monitoring?.tunnel?.status)}>{data.monitoring?.tunnel?.status || t('common.disabled')}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}
