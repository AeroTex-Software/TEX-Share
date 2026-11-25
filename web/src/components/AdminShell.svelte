<script lang="ts">
  import { t } from '../lib/i18n';
  import { navigate } from '../lib/router';
  import { logout, unreadNotifications, user } from '../lib/stores';
  import Avatar from './Avatar.svelte';
  import Footer from './Footer.svelte';
  import Icon from './Icon.svelte';
  import LocaleTheme from './LocaleTheme.svelte';

  export let active = '';
  export let title = '';
  export let subtitle = '';
  export let appName = 'TEX Share';
  export let publicUrl = '';
  export let logoUrl = '';
  export let version = '';

  let open = false;

  const groups = [
    {
      label: 'admin.nav.group.overview',
      items: [
        { key: 'dashboard', href: '/', icon: 'dashboard', label: 'admin.nav.dashboard' },
        { key: 'monitoring', href: '/monitoring', icon: 'activity', label: 'admin.nav.monitoring' }
      ]
    },
    {
      label: 'admin.nav.group.management',
      items: [
        { key: 'users', href: '/users', icon: 'users', label: 'admin.nav.users' },
        { key: 'shares', href: '/shares', icon: 'shares', label: 'admin.nav.shares' },
        { key: 'domains', href: '/domains', icon: 'globe', label: 'admin.nav.domains' }
      ]
    },
    {
      label: 'admin.nav.group.system',
      items: [
        { key: 'backups', href: '/backups', icon: 'backup', label: 'admin.nav.backups' },
        { key: 'audit', href: '/audit', icon: 'shield', label: 'admin.nav.audit' },
        { key: 'settings', href: '/settings', icon: 'settings', label: 'admin.nav.settings' }
      ]
    },
    {
      label: 'admin.nav.group.account',
      items: [{ key: 'profile', href: '/profile', icon: 'user', label: 'admin.nav.profile' }]
    }
  ];

  function go(href: string): void {
    open = false;
    navigate(href);
  }

  function isCurrent(key: string): boolean {
    return active === key;
  }
</script>

<div class="min-h-screen lg:flex">
  <!-- Mobile top bar -->
  <div class="sticky top-0 z-50 flex h-16 items-center gap-3 border-b border-slate-200 bg-white/90 px-4 backdrop-blur dark:border-slate-800 dark:bg-slate-950/90 lg:hidden">
    <button
      type="button"
      class="rounded-xl p-2 text-slate-500 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
      aria-label={t('admin.title')}
      on:click={() => (open = !open)}
    >
      <Icon name={open ? 'close' : 'menu'} size={20} />
    </button>
    <span class="flex items-center gap-2 font-bold text-slate-900 dark:text-white">
      <Icon name="admin" size={18} />{t('admin.title')}
    </span>
  </div>

  <!-- Sidebar: sticky on desktop, off-canvas drawer on mobile -->
  <aside
    class="fixed inset-y-0 left-0 z-50 flex w-72 shrink-0 flex-col overflow-y-auto border-r border-slate-200 bg-white transition-transform duration-200 dark:border-slate-800 dark:bg-slate-900 lg:sticky lg:top-0 lg:bottom-auto lg:z-30 lg:h-dvh lg:translate-x-0 {open
      ? 'translate-x-0 shadow-2xl'
      : '-translate-x-full'}"
  >
    <div class="flex h-16 shrink-0 items-center gap-2.5 border-b border-slate-200 px-5 dark:border-slate-800">
      {#if logoUrl}
        <img src={logoUrl} alt="" class="h-8 w-auto max-w-[9rem] object-contain" />
      {:else}
        <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-brand-600 text-white">
          <Icon name="admin" size={18} />
        </span>
      {/if}
      <div class="min-w-0">
        <p class="truncate text-sm font-bold text-slate-900 dark:text-white">{appName}</p>
        <p class="text-[11px] font-medium uppercase tracking-wide text-slate-400 dark:text-slate-500">{t('admin.title')}</p>
      </div>
    </div>

    <nav class="flex-1 space-y-5 px-3 py-4" aria-label={t('admin.title')}>
      {#each groups as group}
        <div>
          <p class="px-3 pb-1.5 text-[11px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-500">{t(group.label)}</p>
          <div class="space-y-0.5">
            {#each group.items as item}
              <button
                type="button"
                aria-current={isCurrent(item.key) ? 'page' : undefined}
                class="flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition {isCurrent(item.key)
                  ? 'bg-brand-600 text-white shadow-sm shadow-brand-600/30 dark:bg-brand-500'
                  : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800/70'}"
                on:click={() => go(item.href)}
              >
                <Icon name={item.icon} size={18} />
                <span class="flex-1 text-left">{t(item.label)}</span>
                {#if item.key === 'dashboard' && $unreadNotifications > 0}
                  <span class={isCurrent(item.key) ? 'rounded-full bg-white/25 px-2 py-0.5 text-[11px] font-bold text-white' : 'badge-red'}>{$unreadNotifications > 9 ? '9+' : $unreadNotifications}</span>
                {/if}
              </button>
            {/each}
          </div>
        </div>
      {/each}

      {#if publicUrl}
        <div>
          <div class="space-y-0.5">
            <a class="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium text-slate-600 transition hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800/70" href={publicUrl} data-native>
              <Icon name="external" size={18} />
              <span class="flex-1 text-left">{t('admin.nav.public')}</span>
            </a>
          </div>
        </div>
      {/if}
    </nav>

    <div class="shrink-0 border-t border-slate-200 p-3 dark:border-slate-800">
      <div class="mb-2 flex items-center gap-3 rounded-xl px-1 py-1">
        <Avatar user={$user} size={34} />
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold text-slate-800 dark:text-slate-200">{$user?.displayName}</p>
          <p class="truncate text-xs text-slate-500 dark:text-slate-400">@{$user?.username}</p>
        </div>
      </div>
      <button
        type="button"
        class="btn-ghost w-full justify-start text-rose-600 hover:bg-rose-50 dark:text-rose-400 dark:hover:bg-rose-500/10"
        on:click={() => logout('/login')}
      >
        <Icon name="logout" size={17} />{t('common.logout')}
      </button>
    </div>
  </aside>

  {#if open}
    <button
      type="button"
      class="fixed inset-0 z-40 bg-slate-950/50 lg:hidden"
      aria-label={t('common.close')}
      on:click={() => (open = false)}
    />
  {/if}

  <div class="flex min-w-0 flex-1 flex-col">
    <header class="sticky top-0 z-30 hidden h-16 items-center justify-end gap-2 border-b border-slate-200 bg-white/85 px-6 backdrop-blur dark:border-slate-800 dark:bg-slate-950/85 lg:flex">
      <LocaleTheme compact />
    </header>
    <main class="mx-auto w-full max-w-7xl flex-1 px-4 py-6 sm:px-6 sm:py-8">
      {#if title}
        <div class="mb-6">
          <h1 class="text-2xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-3xl">{title}</h1>
          {#if subtitle}<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{subtitle}</p>{/if}
        </div>
      {/if}
      <slot />
    </main>
    <Footer admin version={version} />
  </div>
</div>
