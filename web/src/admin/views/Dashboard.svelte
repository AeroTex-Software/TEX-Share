<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorText } from '../../lib/api';
  import { formatBytes, formatDateTime, formatDuration, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { toast } from '../../lib/stores';
  import type { DailyStat } from '../../lib/types';
  import BarChart from '../../components/BarChart.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
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
    };
    recentAudit: { id: number; ts: number; actor: string; action: string; objectType: string; objectId: string; result: string }[];
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

  let data: DashboardResponse | null = null;
  let daily: DailyStat[] = [];
  let loading = true;

  onMount(load);

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

  function resultTone(result: string): string {
    if (result === 'success') return 'badge-green';
    if (result === 'failure') return 'badge-red';
    return 'badge-slate';
  }

  function serviceTone(state?: string): string {
    if (state === 'available' || state === 'running' || state === 'healthy' || state === 'up') return 'badge-green';
    if (state === 'starting' || state === 'pending') return 'badge-amber';
    if (!state) return 'badge-slate';
    return 'badge-red';
  }

  $: chart = daily.map((d) => ({ label: d.day, value: d.downloads }));
</script>

{#if loading}
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      {#each Array(4) as _}<div class="skeleton h-28"></div>{/each}
    </div>
    <div class="skeleton h-64"></div>
  </div>
{:else if data}
  <div class="space-y-5">
    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard label={t('admin.dashboard.users')} value={formatNumber(data.counts.users)} hint={`${formatNumber(data.counts.admins)} ${t('profile.role.admin')} · ${formatNumber(data.counts.suspendedUsers)} ${t('admin.users.suspended')}`} icon="users" tone="brand" />
      <StatCard label={t('admin.dashboard.shares')} value={formatNumber(data.counts.shares)} hint={`${formatNumber(data.counts.activeShares)} ${t('shares.active')}`} icon="shares" tone="indigo" />
      <StatCard label={t('admin.dashboard.storage')} value={formatBytes(data.usage.storageUsedBytes)} hint={`${formatBytes(data.usage.storageReservedBytes)} ${t('common.reserved')}`} icon="harddrive" tone="emerald" />
      <StatCard label={t('admin.dashboard.downloads24')} value={formatNumber(data.usage.downloads24h)} hint={`${formatBytes(data.usage.bytesServed24h)}`} icon="download" tone="amber" />
    </div>

    <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <StatCard label={t('admin.dashboard.newUsers')} value={formatNumber(data.counts.newUsers24h)} icon="user" tone="slate" />
      <StatCard label={t('admin.dashboard.newShares')} value={formatNumber(data.counts.newShares24h)} icon="plus" tone="slate" />
      <StatCard label={t('admin.dashboard.domains')} value={formatNumber(data.counts.domains)} icon="globe" tone="slate" />
      <StatCard label={t('admin.dashboard.pendingScans')} value={formatNumber(data.counts.pendingScans)} icon="shield" tone={data.counts.pendingScans > 0 ? 'amber' : 'slate'} />
    </div>

    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card card-pad lg:col-span-2">
        <div class="mb-3 flex items-center justify-between">
          <h2 class="text-base font-semibold text-slate-900 dark:text-white">{t('shares.statsLast30')}</h2>
          <button type="button" class="btn-ghost btn-sm" on:click={load}><Icon name="refresh" size={15} />{t('common.refresh')}</button>
        </div>
        <BarChart data={chart} tone="brand" format={(v) => formatNumber(v)} />
      </div>

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
    </div>

    <div class="grid gap-5 lg:grid-cols-3">
      <div class="card lg:col-span-2">
        <div class="flex items-center justify-between border-b border-slate-100 px-5 py-4 dark:border-slate-800">
          <h2 class="text-base font-semibold text-slate-900 dark:text-white">{t('admin.dashboard.recentAudit')}</h2>
        </div>
        {#if data.recentAudit.length === 0}
          <EmptyState icon="shield" title={t('admin.audit.empty')} />
        {:else}
          <ul class="divide-y divide-slate-100 dark:divide-slate-800">
            {#each data.recentAudit as item (item.id)}
              <li class="flex items-center gap-3 px-5 py-3">
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-medium text-slate-800 dark:text-slate-100">{item.action}</span>
                  <span class="block truncate text-xs text-slate-500 dark:text-slate-400">
                    {item.actor || t('common.unknownUser')} · {item.objectType} {item.objectId}
                  </span>
                </span>
                <span class="{resultTone(item.result)}">{item.result}</span>
                <span class="hidden shrink-0 text-xs text-slate-400 dark:text-slate-500 sm:block">{formatDateTime(item.ts)}</span>
              </li>
            {/each}
          </ul>
        {/if}
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
{/if}
