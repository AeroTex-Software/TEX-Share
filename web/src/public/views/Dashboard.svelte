<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorText } from '../../lib/api';
  import { formatBytes, formatDate, formatNumber, percent } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { navigate } from '../../lib/router';
  import { loadActivity } from '../../lib/stores';
  import type { Activity } from '../../lib/types';
  import BarChart from '../../components/BarChart.svelte';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import ProgressBar from '../../components/ProgressBar.svelte';
  import StatCard from '../../components/StatCard.svelte';
  import StatusBadge from '../../components/StatusBadge.svelte';

  interface RecentShare {
    id: string;
    type: string;
    title: string;
    slug: string;
    sizeBytes: number;
    downloadCount: number;
    viewCount: number;
    createdAt: number;
    status: string;
  }

  interface Stats {
    shares: number;
    activeShares: number;
    trashedShares: number;
    files: number;
    views: number;
    downloads: number;
    bytesServed: number;
    storage: { used: number; reserved: number; limit: number };
    recentShares: RecentShare[];
    now: number;
  }

  let loading = true;
  let error = '';
  let stats: Stats | null = null;
  let activity: Activity[] = [];

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      stats = await api<Stats>('/stats');
      activity = await loadActivity(8).catch(() => []);
    } catch (err) {
      error = errorText(err);
    } finally {
      loading = false;
    }
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

  function typeLabel(type: string): string {
    const key = `create.type.${type}`;
    const value = t(key);
    return value === key ? type : value;
  }

  function activityLabel(item: Activity): string {
    const key = `activity.${item.type}`;
    const value = t(key);
    return value === key ? item.type : value;
  }

  $: usedTotal = stats ? stats.storage.used + stats.storage.reserved : 0;
  $: storagePercent = stats ? percent(usedTotal, stats.storage.limit) : 0;
  $: chartData = stats
    ? [
        { label: t('dash.activeShares'), value: stats.activeShares },
        { label: t('shares.downloads'), value: stats.downloads },
        { label: t('shares.views'), value: stats.views },
        { label: t('shares.files'), value: stats.files }
      ]
    : [];
</script>

{#if loading}
  <div class="space-y-4">
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {#each Array(4) as _}
        <div class="skeleton h-28"></div>
      {/each}
    </div>
    <div class="skeleton h-48"></div>
  </div>
{:else if error}
  <div class="card card-pad">
    <p class="flex items-center gap-2 text-sm text-rose-600 dark:text-rose-400"><Icon name="error" size={18} />{error}</p>
    <button type="button" class="btn-secondary btn-sm mt-3" on:click={load}><Icon name="refresh" size={15} />{t('common.retry')}</button>
  </div>
{:else if stats}
  <div class="space-y-6">
    <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      <StatCard label={t('dash.activeShares')} value={formatNumber(stats.activeShares)} icon="shares" tone="brand" hint={`${formatNumber(stats.shares)} ${t('common.total')}`} />
      <StatCard label={t('dash.downloads')} value={formatNumber(stats.downloads)} icon="download" tone="emerald" hint={formatBytes(stats.bytesServed)} />
      <StatCard label={t('dash.views')} value={formatNumber(stats.views)} icon="eye" tone="indigo" />
      <StatCard label={t('dash.storageUsed')} value={stats.storage.limit > 0 ? formatBytes(usedTotal) : t('common.unlimited')} icon="harddrive" tone="amber" hint={stats.storage.limit > 0 ? formatBytes(stats.storage.limit) : ''} />
    </div>

    <div class="grid gap-6 lg:grid-cols-3">
      <section class="card card-pad lg:col-span-2">
        <div class="flex items-center justify-between gap-3">
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="shares" size={18} />{t('dash.recentShares')}</h2>
          <button type="button" class="btn-primary btn-sm" on:click={() => navigate('/shares/new')}><Icon name="plus" size={15} />{t('nav.newShare')}</button>
        </div>

        {#if stats.recentShares.length === 0}
          <EmptyState icon="inbox" title={t('dash.noShares')} message={t('dash.quickCreateHint')}>
            <button type="button" class="btn-primary btn-sm mt-1" on:click={() => navigate('/shares/new')}><Icon name="plus" size={15} />{t('dash.createFirst')}</button>
          </EmptyState>
        {:else}
          <ul class="mt-4 divide-y divide-slate-100 dark:divide-slate-800">
            {#each stats.recentShares as item}
              <li>
                <button type="button" class="flex w-full items-center gap-3 py-3 text-left transition hover:opacity-80" on:click={() => navigate(`/shares/${item.id}`)}>
                  <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                    <Icon name={typeIcon(item.type)} size={18} />
                  </span>
                  <span class="min-w-0 flex-1">
                    <span class="block truncate text-sm font-medium text-slate-800 dark:text-slate-100">{item.title || item.slug}</span>
                    <span class="block truncate text-xs text-slate-500 dark:text-slate-400">{typeLabel(item.type)} · {formatDate(item.createdAt)}</span>
                  </span>
                  <span class="hidden shrink-0 text-right text-xs text-slate-500 dark:text-slate-400 sm:block">
                    <span class="block">{formatNumber(item.downloadCount)} {t('shares.downloadsShort')}</span>
                    <span class="block">{formatBytes(item.sizeBytes)}</span>
                  </span>
                  <StatusBadge status={item.status} size="sm" />
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </section>

      <div class="space-y-6">
        <section class="card card-pad">
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="harddrive" size={18} />{t('dash.storage')}</h2>
          <div class="mt-4">
            {#if stats.storage.limit > 0}
              <ProgressBar value={usedTotal} max={stats.storage.limit} tone={storagePercent >= 90 ? 'rose' : storagePercent >= 75 ? 'amber' : 'brand'} label={`${formatBytes(usedTotal)} ${t('common.of')} ${formatBytes(stats.storage.limit)}`} />
              {#if stats.storage.reserved > 0}
                <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('common.reserved')}: {formatBytes(stats.storage.reserved)}</p>
              {/if}
            {:else}
              <p class="text-sm font-medium text-slate-700 dark:text-slate-200">{t('dash.storageUnlimited')} · {formatBytes(usedTotal)}</p>
            {/if}
          </div>
        </section>

        <section class="card card-pad">
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="chart" size={18} />{t('shares.stats')}</h2>
          <div class="mt-4">
            <BarChart data={chartData} format={(v) => formatNumber(v)} />
          </div>
        </section>
      </div>
    </div>

    <section class="card card-pad">
      <div class="flex items-center justify-between gap-3">
        <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="activity" size={18} />{t('dash.recentActivity')}</h2>
        <button type="button" class="btn-ghost btn-sm" on:click={() => navigate('/activity')}>{t('common.open')}<Icon name="arrowRight" size={14} /></button>
      </div>
      {#if activity.length === 0}
        <p class="mt-4 text-sm text-slate-500 dark:text-slate-400">{t('activity.empty')}</p>
      {:else}
        <ul class="mt-4 space-y-3">
          {#each activity as item}
            <li class="flex items-center gap-3 text-sm">
              <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg bg-slate-100 text-slate-500 dark:bg-slate-800 dark:text-slate-300">
                <Icon name="activity" size={15} />
              </span>
              <span class="min-w-0 flex-1 truncate text-slate-700 dark:text-slate-200">{activityLabel(item)}</span>
              <span class="shrink-0 text-xs text-slate-400 dark:text-slate-500">{formatDate(item.createdAt)}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
{/if}
