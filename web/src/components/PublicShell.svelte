<script lang="ts">
  import { onMount } from 'svelte';
  import { t } from '../lib/i18n';
  import { navigate } from '../lib/router';
  import { logout, publicMeta, unreadNotifications, user } from '../lib/stores';
  import { mediaURL } from '../lib/api';
  import Avatar from './Avatar.svelte';
  import Footer from './Footer.svelte';
  import Icon from './Icon.svelte';
  import LocaleTheme from './LocaleTheme.svelte';

  export let active = '';
  export let title = '';
  export let subtitle = '';
  export let adminUrl = '';

  let mobileOpen = false;

  const nav = [
    { key: 'dashboard', href: '/', icon: 'dashboard', label: 'nav.dashboard' },
    { key: 'shares', href: '/shares', icon: 'shares', label: 'nav.shares' },
    { key: 'activity', href: '/activity', icon: 'activity', label: 'nav.activity' },
    { key: 'profile', href: '/profile', icon: 'user', label: 'nav.profile' }
  ];

  $: brandName = $publicMeta?.appName || t('app.name');
  $: logo = mediaURL($publicMeta?.branding?.logo ? $publicMeta.branding.logoUrl : '');
  $: isAdmin = $user?.role === 'admin';

  onMount(() => {
    mobileOpen = false;
  });

  function go(href: string): void {
    mobileOpen = false;
    navigate(href);
  }

  function isCurrent(key: string): boolean {
    if (key === 'dashboard') return active === 'dashboard';
    return active === key;
  }
</script>

<div class="flex min-h-screen flex-col">
  <header class="sticky top-0 z-40 border-b border-slate-200 bg-white/85 backdrop-blur dark:border-slate-800 dark:bg-slate-950/85">
    <div class="mx-auto flex h-16 max-w-6xl items-center gap-3 px-4">
      <a href="/" class="flex min-w-0 items-center gap-2.5" on:click|preventDefault={() => go('/')}>
        {#if logo}
          <img src={logo} alt="" class="h-8 w-auto max-w-[9rem] object-contain" />
        {:else}
          <span class="flex h-8 w-8 items-center justify-center rounded-lg bg-brand-600 text-white">
            <Icon name="shield" size={18} />
          </span>
        {/if}
        <span class="truncate text-base font-bold tracking-tight text-slate-900 dark:text-white">{brandName}</span>
      </a>

      <nav class="ml-3 hidden items-center gap-1 md:flex" aria-label={t('nav.dashboard')}>
        {#each nav as item}
          <button
            type="button"
            class="inline-flex items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium transition {isCurrent(item.key)
              ? 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300'
              : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'}"
            on:click={() => go(item.href)}
          >
            <Icon name={item.icon} size={17} />
            {t(item.label)}
          </button>
        {/each}
      </nav>

      <div class="ml-auto flex items-center gap-1.5">
        <button
          type="button"
          class="relative rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
          title={t('nav.notifications')}
          aria-label={t('nav.notifications')}
          on:click={() => go('/notifications')}
        >
          <Icon name="bell" size={19} />
          {#if $unreadNotifications > 0}
            <span class="absolute -right-0.5 -top-0.5 flex h-4 min-w-[1rem] items-center justify-center rounded-full bg-rose-500 px-1 text-[10px] font-bold text-white">
              {$unreadNotifications > 9 ? '9+' : $unreadNotifications}
            </span>
          {/if}
        </button>

        <div class="hidden sm:block"><LocaleTheme compact /></div>

        <details class="relative">
          <summary class="flex cursor-pointer list-none items-center gap-2 rounded-xl p-1 pr-2 transition hover:bg-slate-100 dark:hover:bg-slate-800 [&::-webkit-details-marker]:hidden">
            <Avatar user={$user} size={32} />
            <span class="hidden max-w-[8rem] truncate text-sm font-medium text-slate-700 dark:text-slate-200 lg:block">
              {$user?.displayName || $user?.username}
            </span>
            <Icon name="chevronDown" size={15} />
          </summary>
          <div class="absolute right-0 z-50 mt-2 w-60 overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-xl dark:border-slate-800 dark:bg-slate-900">
            <div class="border-b border-slate-100 px-4 py-3 dark:border-slate-800">
              <p class="truncate text-sm font-semibold text-slate-900 dark:text-white">{$user?.displayName}</p>
              <p class="truncate text-xs text-slate-500 dark:text-slate-400">@{$user?.username}</p>
            </div>
            <div class="p-1.5">
              <button type="button" class="btn-ghost w-full justify-start" on:click={() => go('/profile')}>
                <Icon name="user" size={17} />{t('nav.profile')}
              </button>
              <button type="button" class="btn-ghost w-full justify-start" on:click={() => go('/activity')}>
                <Icon name="activity" size={17} />{t('nav.activity')}
              </button>
              {#if isAdmin && adminUrl}
                <a class="btn-ghost w-full justify-start" href={adminUrl} data-native>
                  <Icon name="admin" size={17} />{t('nav.admin')}
                </a>
              {/if}
              <button type="button" class="btn-ghost w-full justify-start text-rose-600 hover:bg-rose-50 dark:text-rose-400 dark:hover:bg-rose-500/10" on:click={() => logout('/login')}>
                <Icon name="logout" size={17} />{t('common.logout')}
              </button>
            </div>
          </div>
        </details>

        <button
          type="button"
          class="rounded-xl p-2 text-slate-500 transition hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800 md:hidden"
          aria-label={t('nav.dashboard')}
          on:click={() => (mobileOpen = !mobileOpen)}
        >
          <Icon name={mobileOpen ? 'close' : 'menu'} size={20} />
        </button>
      </div>
    </div>

    {#if mobileOpen}
      <div class="border-t border-slate-200 bg-white px-4 py-3 dark:border-slate-800 dark:bg-slate-950 md:hidden">
        <nav class="flex flex-col gap-1">
          {#each nav as item}
            <button
              type="button"
              class="flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition {isCurrent(item.key)
                ? 'bg-brand-50 text-brand-700 dark:bg-brand-500/15 dark:text-brand-300'
                : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800'}"
              on:click={() => go(item.href)}
            >
              <Icon name={item.icon} size={18} />{t(item.label)}
            </button>
          {/each}
        </nav>
        <div class="mt-3 flex items-center justify-between border-t border-slate-100 pt-3 dark:border-slate-800">
          <LocaleTheme compact />
          {#if isAdmin && adminUrl}
            <a class="link text-sm" href={adminUrl} data-native>{t('nav.admin')}</a>
          {/if}
        </div>
      </div>
    {/if}
  </header>

  <main class="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:py-8">
    {#if title}
      <div class="mb-6">
        <h1 class="text-2xl font-bold tracking-tight text-slate-900 dark:text-white sm:text-3xl">{title}</h1>
        {#if subtitle}<p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{subtitle}</p>{/if}
      </div>
    {/if}
    <slot />
  </main>

  <Footer version={$publicMeta?.version || ''} />
</div>
