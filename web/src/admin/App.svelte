<script lang="ts">
  import { onMount } from 'svelte';
  import { locale, t } from '../lib/i18n';
  import { route } from '../lib/router';
  import {
    bootstrapSession,
    loadNotifications,
    maintenanceMode,
    mustChangePassword,
    sessionReady,
    setupCompleted,
    user
  } from '../lib/stores';
  import AdminShell from '../components/AdminShell.svelte';
  import ConfirmHost from '../components/ConfirmHost.svelte';
  import Footer from '../components/Footer.svelte';
  import Icon from '../components/Icon.svelte';
  import LocaleTheme from '../components/LocaleTheme.svelte';
  import ToastHost from '../components/ToastHost.svelte';
  import { adminMeta, loadAdminMeta, loadAdminShell } from './meta';
  import Audit from './views/Audit.svelte';
  import Backups from './views/Backups.svelte';
  import Dashboard from './views/Dashboard.svelte';
  import Domains from './views/Domains.svelte';
  import ForcedPassword from './views/ForcedPassword.svelte';
  import Login from './views/Login.svelte';
  import Monitoring from './views/Monitoring.svelte';
  import Network from './views/Network.svelte';
  import NotFound from './views/NotFound.svelte';
  import Profile from './views/Profile.svelte';
  import Settings from './views/Settings.svelte';
  import Setup from './views/Setup.svelte';
  import Shares from './views/Shares.svelte';
  import Users from './views/Users.svelte';

  let shellLoaded = false;

  onMount(async () => {
    await loadAdminMeta();
    await bootstrapSession();
  });

  $: if ($user && !shellLoaded) {
    shellLoaded = true;
    void loadAdminShell();
    void loadNotifications();
  }
  $: if (!$user) shellLoaded = false;

  $: segments = $route.segments;
  $: section = segments[0] || '';
  $: head = shellHead(section, $locale);

  function shellHead(key: string, _loc: string): { title: string; subtitle: string; active: string } {
    switch (key) {
      case '':
        return { title: t('admin.dashboard.title'), subtitle: t('admin.dashboard.subtitle'), active: 'dashboard' };
      case 'users':
        return { title: t('admin.users.title'), subtitle: t('admin.users.subtitle'), active: 'users' };
      case 'shares':
        return { title: t('admin.shares.title'), subtitle: t('admin.shares.subtitle'), active: 'shares' };
      case 'domains':
        return { title: t('admin.domains.title'), subtitle: t('admin.domains.subtitle'), active: 'domains' };
      case 'network':
        return { title: t('admin.network.title'), subtitle: t('admin.network.subtitle'), active: 'network' };
      case 'monitoring':
        return { title: t('admin.monitoring.title'), subtitle: t('admin.monitoring.subtitle'), active: 'monitoring' };
      case 'backups':
        return { title: t('admin.backups.title'), subtitle: t('admin.backups.subtitle'), active: 'backups' };
      case 'audit':
        return { title: t('admin.audit.title'), subtitle: t('admin.audit.subtitle'), active: 'audit' };
      case 'settings':
        return { title: t('admin.settings.title'), subtitle: t('admin.settings.subtitle'), active: 'settings' };
      case 'profile':
        return { title: t('profile.title'), subtitle: t('profile.subtitle'), active: 'profile' };
      default:
        return { title: '', subtitle: '', active: '' };
    }
  }
</script>

{#key $locale}
  <div class="min-h-full">
    {#if !$sessionReady}
      <div class="flex min-h-screen flex-col">
        <div class="flex flex-1 flex-col items-center justify-center gap-4">
          <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-brand-600 text-white">
            <Icon name="admin" size={24} />
          </span>
          <p class="flex items-center gap-2 text-sm text-slate-500 dark:text-slate-400">
            <Icon name="spinner" size={16} class="animate-spin" />{t('common.loading')}
          </p>
        </div>
        <Footer admin version={$adminMeta.version} />
      </div>
    {:else if !$setupCompleted}
      <Setup />
    {:else if !$user}
      <Login />
    {:else if $mustChangePassword}
      <ForcedPassword />
    {:else}
      <AdminShell
        active={head.active}
        title={head.title}
        subtitle={head.subtitle}
        appName={$adminMeta.appName}
        publicUrl={$adminMeta.publicUrl}
        logoUrl={$adminMeta.logoUrl}
        version={$adminMeta.version}
      >
        {#if section === ''}
          <Dashboard />
        {:else if section === 'users'}
          <Users />
        {:else if section === 'shares'}
          <Shares />
        {:else if section === 'domains'}
          <Domains />
        {:else if section === 'network'}
          <Network />
        {:else if section === 'monitoring'}
          <Monitoring />
        {:else if section === 'backups'}
          <Backups />
        {:else if section === 'audit'}
          <Audit />
        {:else if section === 'settings'}
          <Settings />
        {:else if section === 'profile'}
          <Profile />
        {:else}
          <NotFound />
        {/if}
      </AdminShell>
    {/if}
  </div>
  <ToastHost />
  <ConfirmHost />
{/key}
