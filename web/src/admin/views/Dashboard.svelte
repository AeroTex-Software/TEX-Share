<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorText } from '../../lib/api';
  import { docLang, formatBytes, formatNumber, timeAgo } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { navigate } from '../../lib/router';
  import { dashboardRefreshTick, dashboardUpdatedAt, toast } from '../../lib/stores';
  import type { Backup } from '../../lib/types';
  import DonutChart from '../../components/DonutChart.svelte';
  import Icon from '../../components/Icon.svelte';
  import Sparkline from '../../components/Sparkline.svelte';
  import TrafficChart from '../../components/TrafficChart.svelte';

  interface DayPoint {
    day: string;
    users: number;
    shares: number;
    downBytes: number;
    upBytes: number;
  }

  interface ActivityItem {
    id: number;
    username: string;
    shareId: string;
    type: string;
    meta: Record<string, unknown>;
    createdAt: number;
  }

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
    series: DayPoint[];
    activities: ActivityItem[];
    backup: { count: number; latest: Backup | null };
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
      cpuPercent?: number;
      memUsedBytes?: number;
      memTotalBytes?: number;
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

  const POLL_MS = 2000;

  let data: DashboardResponse | null = null;
  let loading = true;
  let traffic = { up: 0, down: 0 };
  let lastSample: TrafficSample | null = null;
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    void load();
    pollTimer = setInterval(() => void pollTraffic(), POLL_MS);
    // The refresh button lives in the shell header; it bumps this tick.
    const unsub = dashboardRefreshTick.subscribe((v) => {
      if (v > 0) void load();
    });
    return () => {
      unsub();
      if (pollTimer) clearInterval(pollTimer);
    };
  });

  async function load(): Promise<void> {
    loading = true;
    try {
      data = await api<DashboardResponse>('/dashboard');
      dashboardUpdatedAt.set(Math.floor(Date.now() / 1000));
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
          traffic = {
            down: Math.max(0, (next.tx - lastSample.tx) / dt),
            up: Math.max(0, (next.rx - lastSample.rx) / dt)
          };
        }
      }
      lastSample = next;
    } catch {
      // Transient errors are ignored; the next tick retries.
    }
  }

  function weekday(day: string): string {
    if (!day) return '';
    const label = new Intl.DateTimeFormat(docLang(), { weekday: 'short', timeZone: 'UTC' }).format(new Date(day + 'T00:00:00Z'));
    return label.replace(/\.$/, '');
  }

  function activityLabel(type: string): string {
    const key = `activity.${type}`;
    const value = t(key);
    return value === key ? type : value;
  }

  function activityIcon(type: string): string {
    switch (type) {
      case 'upload_complete':
        return 'upload';
      case 'download':
        return 'download';
      case 'share_created':
      case 'share_updated':
        return 'link';
      case 'share_trashed':
        return 'trash';
      case 'share_restored':
        return 'restore';
      case 'share_expired':
        return 'clock';
      case 'login':
        return 'user';
      case 'password_changed':
        return 'key';
      case '2fa_enabled':
      case '2fa_disabled':
        return 'shield';
      default:
        return 'info';
    }
  }

  function activityTone(type: string): string {
    switch (type) {
      case 'upload_complete':
        return 'bg-brand-500/15 text-brand-600 dark:text-brand-300';
      case 'download':
        return 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-300';
      case 'share_created':
      case 'share_updated':
        return 'bg-indigo-500/15 text-indigo-600 dark:text-indigo-300';
      case 'share_trashed':
      case 'share_expired':
        return 'bg-amber-500/15 text-amber-600 dark:text-amber-300';
      default:
        return 'bg-slate-500/15 text-slate-600 dark:text-slate-300';
    }
  }

  function gaugeSegments(value: number | undefined, color: string, label: string): { label: string; value: number; color: string }[] {
    if (value === undefined || value === null) return [];
    return [{ label, value: Math.max(0, Math.min(100, value)), color }];
  }

  function pctLabel(value: number | undefined): string {
    return value === undefined || value === null ? '—' : `${Math.round(value)}%`;
  }

  $: series = data?.series ?? [];
  $: usersSpark = series.map((d) => d.users);
  $: sharesSpark = series.map((d) => d.shares);
  $: trafficSpark = series.map((d) => d.downBytes + d.upBytes);
  $: todayTraffic = trafficSpark.length ? trafficSpark[trafficSpark.length - 1] : 0;
  $: hasTraffic = series.some((d) => d.downBytes > 0 || d.upBytes > 0);

  $: storageUsed = data?.usage.storageUsedBytes ?? 0;
  $: storageReserved = data?.usage.storageReservedBytes ?? 0;
  $: diskTotal = data?.usage.diskTotalBytes ?? 0;
  $: diskFree = data?.usage.diskFreeBytes ?? 0;
  $: diskUsed = data?.usage.diskUsedBytes ?? 0;
  $: storagePct = diskTotal > 0 ? Math.min(100, (storageUsed / diskTotal) * 100) : 0;
  $: diskPct = diskTotal > 0 ? Math.min(100, (diskUsed / diskTotal) * 100) : 0;

  $: cpuPct = data?.system.cpuPercent;
  $: memTotal = data?.system.memTotalBytes ?? 0;
  $: memUsed = data?.system.memUsedBytes ?? 0;
  $: memPct = memTotal > 0 ? (memUsed / memTotal) * 100 : undefined;

  $: trafficSeries = [
    { label: t('admin.dashboard.trafficDown'), color: '#3b82f6', points: series.map((d) => d.downBytes) },
    { label: t('admin.dashboard.trafficUp'), color: '#a855f7', points: series.map((d) => d.upBytes) }
  ];
  $: trafficLabels = series.map((d) => weekday(d.day));

  $: loadGauges = [
    { key: 'cpu', label: t('admin.dashboard.cpu'), icon: 'cpu', color: '#3b82f6', value: cpuPct },
    { key: 'ram', label: t('admin.dashboard.ram'), icon: 'memory', color: '#a855f7', value: memPct },
    { key: 'disk', label: t('admin.dashboard.disk'), icon: 'harddrive', color: '#22c55e', value: diskTotal > 0 ? diskPct : undefined }
  ];
</script>

{#if loading && !data}
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">{#each Array(4) as _}<div class="skeleton h-40"></div>{/each}</div>
    <div class="grid gap-5 lg:grid-cols-3">
      <div class="skeleton h-72 lg:col-span-2"></div>
      <div class="skeleton h-72"></div>
    </div>
    <div class="grid gap-5 lg:grid-cols-3">{#each Array(3) as _}<div class="skeleton h-48"></div>{/each}</div>
  </div>
{:else if data}
  <div class="space-y-5">
    <!-- Stat tiles -->
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div class="card flex flex-col p-5">
        <div class="flex items-start justify-between gap-3">
          <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-blue-500/15 text-blue-600 dark:text-blue-400"><Icon name="users" size={20} /></span>
          {#if data.counts.newUsers24h > 0}<span class="badge-green"><Icon name="trendingUp" size={13} />+{formatNumber(data.counts.newUsers24h)}</span>{/if}
        </div>
        <p class="mt-3 text-2xl font-bold tracking-tight text-slate-900 dark:text-white">{formatNumber(data.counts.users)}</p>
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.dashboard.users')}</p>
        <div class="mt-auto pt-4"><Sparkline points={usersSpark} color="#3b82f6" height={44} /></div>
      </div>

      <div class="card flex flex-col p-5">
        <div class="flex items-start justify-between gap-3">
          <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-purple-500/15 text-purple-600 dark:text-purple-400"><Icon name="shares" size={20} /></span>
          {#if data.counts.newShares24h > 0}<span class="badge-green"><Icon name="trendingUp" size={13} />+{formatNumber(data.counts.newShares24h)}</span>{/if}
        </div>
        <p class="mt-3 text-2xl font-bold tracking-tight text-slate-900 dark:text-white">{formatNumber(data.counts.shares)}</p>
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.dashboard.shares')}</p>
        <div class="mt-auto pt-4"><Sparkline points={sharesSpark} color="#a855f7" height={44} /></div>
      </div>

      <div class="card flex flex-col p-5">
        <div class="flex items-start justify-between gap-3">
          <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"><Icon name="harddrive" size={20} /></span>
        </div>
        <div class="mt-auto flex items-end justify-between gap-3 pt-3">
          <div class="min-w-0">
            <p class="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">{formatBytes(storageUsed)}</p>
            <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.dashboard.storage')}</p>
            {#if diskTotal > 0}<p class="mt-1 text-xs text-slate-400 dark:text-slate-500">{t('common.of')} {formatBytes(diskTotal)}</p>{/if}
          </div>
          <DonutChart
            size={84}
            thickness={9}
            max={100}
            segments={[{ label: t('admin.dashboard.storageUsed'), value: storagePct, color: '#10b981' }]}
            centerValue={pctLabel(storagePct)}
          />
        </div>
      </div>

      <div class="card flex flex-col p-5">
        <div class="flex items-start justify-between gap-3">
          <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-orange-500/15 text-orange-600 dark:text-orange-400"><Icon name="activity" size={20} /></span>
          <span class="inline-flex items-center gap-1.5 rounded-full border border-slate-200 bg-white/70 px-2 py-1 text-[11px] font-medium text-slate-500 dark:border-slate-800 dark:bg-slate-900/70 dark:text-slate-400">
            <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>{t('admin.dashboard.liveNow')}
            <Icon name="download" size={12} />{formatBytes(traffic.down)}/s
            <Icon name="upload" size={12} />{formatBytes(traffic.up)}/s
          </span>
        </div>
        <p class="mt-3 text-2xl font-bold tracking-tight text-slate-900 dark:text-white">{formatBytes(todayTraffic)}</p>
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.dashboard.traffic24')}</p>
        <div class="mt-auto pt-4"><Sparkline points={trafficSpark} color="#f97316" height={44} /></div>
      </div>
    </div>

    <!-- 7-day traffic + system load -->
    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card card-pad lg:col-span-2">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="chart" size={18} />{t('admin.dashboard.traffic7d')}</h2>
          <div class="flex items-center gap-4 text-xs font-medium text-slate-500 dark:text-slate-400">
            <span class="flex items-center gap-1.5"><span class="h-2.5 w-2.5 rounded-full" style="background:#3b82f6"></span>{t('admin.dashboard.trafficDown')}</span>
            <span class="flex items-center gap-1.5"><span class="h-2.5 w-2.5 rounded-full" style="background:#a855f7"></span>{t('admin.dashboard.trafficUp')}</span>
          </div>
        </div>
        {#if hasTraffic}
          <TrafficChart series={trafficSeries} labels={trafficLabels} height={250} format={(v) => formatBytes(v)} />
        {:else}
          <div class="flex items-center justify-center text-sm text-slate-400 dark:text-slate-500" style="height:250px">{t('admin.dashboard.trafficIdle')}</div>
        {/if}
      </div>

      <div class="card card-pad">
        <h2 class="mb-4 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="activity" size={18} />{t('admin.dashboard.systemLoad')}</h2>
        <div class="grid grid-cols-3 gap-2">
          {#each loadGauges as gauge (gauge.key)}
            <div class="flex flex-col items-center gap-2.5">
              <DonutChart
                size={100}
                thickness={11}
                max={100}
                segments={gaugeSegments(gauge.value, gauge.color, gauge.label)}
                centerValue={pctLabel(gauge.value)}
              />
              <span class="flex items-center gap-1 text-xs font-medium text-slate-600 dark:text-slate-300 sm:text-sm"><Icon name={gauge.icon} size={15} />{gauge.label}</span>
            </div>
          {/each}
        </div>
        {#if memTotal > 0}
          <p class="mt-4 border-t border-slate-100 pt-4 text-center text-xs text-slate-500 dark:border-slate-800 dark:text-slate-400">
            {t('admin.dashboard.ram')}: {formatBytes(memUsed)} {t('common.of')} {formatBytes(memTotal)}
          </p>
        {/if}
      </div>
    </div>

    <!-- Active shares, recent activity, backup status -->
    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card card-pad relative overflow-hidden">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="shares" size={18} />{t('admin.dashboard.activeShares')}</h2>
        <div class="mt-5 flex items-end gap-3">
          <p class="text-4xl font-bold tracking-tight text-slate-900 dark:text-white">{formatNumber(data.counts.activeShares)}</p>
          {#if data.counts.newShares24h > 0}<span class="badge-green mb-1"><Icon name="trendingUp" size={13} />+{formatNumber(data.counts.newShares24h)}</span>{/if}
        </div>
        <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('common.of')} {formatNumber(data.counts.shares)} {t('admin.dashboard.shares')}</p>
        <div class="pointer-events-none absolute -bottom-3 right-2 text-brand-500/10"><Icon name="folder" size={92} /></div>
      </div>

      <div class="card card-pad">
        <h2 class="mb-4 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="activity" size={18} />{t('admin.dashboard.recentActivity')}</h2>
        {#if data.activities.length === 0}
          <p class="text-sm text-slate-500 dark:text-slate-400">{t('activity.empty')}</p>
        {:else}
          <ul class="space-y-3">
            {#each data.activities as item (item.id)}
              <li class="flex items-center gap-3">
                <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full {activityTone(item.type)}"><Icon name={activityIcon(item.type)} size={15} /></span>
                <span class="min-w-0 flex-1 truncate text-sm font-medium text-slate-700 dark:text-slate-200">{activityLabel(item.type)}</span>
                <span class="shrink-0 text-xs tabular-nums text-slate-400 dark:text-slate-500">{timeAgo(item.createdAt)}</span>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="card card-pad flex flex-col">
        <h2 class="mb-4 flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="database" size={18} />{t('admin.dashboard.backupStatus')}</h2>
        {#if data.backup.latest}
          <div class="flex items-center gap-4">
            <span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full {data.backup.latest.status === 'complete' ? 'bg-emerald-500/15 text-emerald-600 dark:text-emerald-400' : 'bg-amber-500/15 text-amber-600 dark:text-amber-400'}">
              <Icon name={data.backup.latest.status === 'complete' ? 'check' : 'warning'} size={24} />
            </span>
            <div class="min-w-0">
              <p class="text-sm font-semibold text-slate-900 dark:text-white">
                {data.backup.latest.status === 'complete' ? t('admin.dashboard.backupCurrent') : data.backup.latest.status}
              </p>
              <p class="text-xs text-slate-500 dark:text-slate-400">{timeAgo(data.backup.latest.createdAt)}</p>
            </div>
          </div>
        {:else}
          <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.dashboard.backupNone')}</p>
        {/if}
        <button type="button" class="btn-secondary btn-sm mt-auto self-start" on:click={() => navigate('/backups')}>
          {t('admin.dashboard.backupManage')}<Icon name="arrowRight" size={15} />
        </button>
      </div>
    </div>
  </div>
{/if}
