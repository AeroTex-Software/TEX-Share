<script lang="ts">
  import { onMount } from 'svelte';
  import { get } from 'svelte/store';
  import { locale, t } from '../lib/i18n';
  import { isShareToken } from '../lib/links';
  import { route } from '../lib/router';
  import {
    bootstrapSession,
    loadNotifications,
    loadPublicConfig,
    loadPublicMeta,
    maintenanceMode,
    mustChangePassword,
    publicConfig,
    publicMeta,
    sessionError,
    sessionReady,
    setupCompleted,
    user
  } from '../lib/stores';
  import ConfirmHost from '../components/ConfirmHost.svelte';
  import Footer from '../components/Footer.svelte';
  import Icon from '../components/Icon.svelte';
  import LocaleTheme from '../components/LocaleTheme.svelte';
  import PublicShell from '../components/PublicShell.svelte';
  import ToastHost from '../components/ToastHost.svelte';
  import Activity from './views/Activity.svelte';
  import CreateShare from './views/CreateShare.svelte';
  import Dashboard from './views/Dashboard.svelte';
  import ForcedPassword from './views/ForcedPassword.svelte';
  import Login from './views/Login.svelte';
  import NotFound from './views/NotFound.svelte';
  import Notifications from './views/Notifications.svelte';
  import Profile from './views/Profile.svelte';
  import PublicShare from './views/PublicShare.svelte';
  import ShareDetail from './views/ShareDetail.svelte';
  import Shares from './views/Shares.svelte';

  onMount(async () => {
    await loadPublicMeta();
    await bootstrapSession();
    if (get(user)) {
      await loadPublicConfig();
      void loadNotifications();
    }
  });

  $: segments = $route.segments;
  $: shareToken = segments.length > 0 && isShareToken(segments[segments.length - 1]) ? segments[segments.length - 1] : '';
  $: section = segments[0] || '';
  $: sub = segments[1] || '';
  $: adminUrl = $publicConfig?.adminUrl || '';
  $: head = shellHead(segments, $locale);

  function shellHead(segs: string[], _loc: string): { title: string; subtitle: string; active: string } {
    const first = segs[0] || '';
    const second = segs[1] || '';
    switch (first) {
      case '':
        return { title: t('dash.title'), subtitle: t('dash.subtitle'), active: 'dashboard' };
      case 'shares':
        if (second === 'new') return { title: t('create.title'), subtitle: t('create.subtitle'), active: 'shares' };
        if (second) return { title: t('detail.title'), subtitle: '', active: 'shares' };
        return { title: t('shares.title'), subtitle: t('shares.subtitle'), active: 'shares' };
      case 'activity':
        return { title: t('activity.title'), subtitle: t('activity.subtitle'), active: 'activity' };
      case 'notifications':
        return { title: t('notifications.title'), subtitle: '', active: '' };
      case 'profile':
        return { title: t('profile.title'), subtitle: t('profile.subtitle'), active: 'profile' };
      default:
        return { title: '', subtitle: '', active: '' };
    }
  }
</script>

{#key $locale}
  <div class="min-h-full">
    {#if shareToken}
      <PublicShare token={shareToken} />
    {:else if !$sessionReady}
      <div class="flex min-h-screen flex-col">
        <div class="flex flex-1 flex-col items-center justify-center gap-4">
          <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-brand-600 text-white">
            <Icon name="shield" size={24} />
          </span>
          <p class="flex items-center gap-2 text-sm text-slate-500 dark:text-slate-400">
            <Icon name="spinner" size={16} class="animate-spin" />{t('common.loading')}
          </p>
        </div>
        <Footer version={$publicMeta?.version || ''} />
      </div>
    {:else if $maintenanceMode}
      <div class="flex min-h-screen flex-col">
        <div class="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
          <span class="flex h-14 w-14 items-center justify-center rounded-2xl bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300">
            <Icon name="warning" size={26} />
          </span>
          <h1 class="text-xl font-bold text-slate-900 dark:text-white">{t('err.maintenance')}</h1>
          <p class="max-w-sm text-sm text-slate-500 dark:text-slate-400">{t('admin.settings.maintenanceHint')}</p>
          <div class="mt-2"><LocaleTheme /></div>
        </div>
        <Footer version={$publicMeta?.version || ''} />
      </div>
    {:else if !$setupCompleted}
      <div class="flex min-h-screen flex-col">
        <div class="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center">
          <span class="flex h-14 w-14 items-center justify-center rounded-2xl bg-brand-100 text-brand-600 dark:bg-brand-500/15 dark:text-brand-300">
            <Icon name="settings" size={26} />
          </span>
          <h1 class="text-xl font-bold text-slate-900 dark:text-white">{t('admin.setup.title')}</h1>
          <p class="max-w-md text-sm text-slate-500 dark:text-slate-400">{t('admin.setup.subtitle')}</p>
          <div class="mt-2"><LocaleTheme /></div>
        </div>
        <Footer version={$publicMeta?.version || ''} />
      </div>
    {:else if !$user}
      <Login error={$sessionError} />
    {:else if $mustChangePassword}
      <ForcedPassword />
    {:else}
      <PublicShell active={head.active} title={head.title} subtitle={head.subtitle} {adminUrl}>
        {#if section === ''}
          <Dashboard />
        {:else if section === 'shares' && sub === 'new'}
          <CreateShare />
        {:else if section === 'shares' && sub}
          <ShareDetail id={sub} />
        {:else if section === 'shares'}
          <Shares />
        {:else if section === 'activity'}
          <Activity />
        {:else if section === 'notifications'}
          <Notifications />
        {:else if section === 'profile'}
          <Profile />
        {:else}
          <NotFound />
        {/if}
      </PublicShell>
    {/if}
  </div>
  <ToastHost />
  <ConfirmHost />
{/key}
