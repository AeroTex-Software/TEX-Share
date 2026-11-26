<script lang="ts">
  import { errorText } from '../../lib/api';
  import { t } from '../../lib/i18n';
  import { navigate } from '../../lib/router';
  import { changePassword, clearSession } from '../../lib/stores';
  import Footer from '../../components/Footer.svelte';
  import Icon from '../../components/Icon.svelte';
  import LocaleTheme from '../../components/LocaleTheme.svelte';
  import TexShareLogo from '../../components/TexShareLogo.svelte';
  import { adminMeta, loadAdminShell } from '../meta';

  let currentPassword = '';
  let newPassword = '';
  let confirmPassword = '';
  let busy = false;
  let error = '';

  async function submit(event: Event): Promise<void> {
    event.preventDefault();
    error = '';
    if (!currentPassword || !newPassword) {
      error = t('common.requiredField');
      return;
    }
    if (newPassword !== confirmPassword) {
      error = t('auth.passwordMismatch');
      return;
    }
    busy = true;
    try {
      await changePassword(currentPassword, newPassword);
      void loadAdminShell();
      navigate('/', true);
    } catch (err) {
      error = errorText(err);
    } finally {
      busy = false;
    }
  }

  function signOut(): void {
    clearSession();
    navigate('/login', true);
  }
</script>

<div class="flex min-h-screen flex-col bg-slate-50 dark:bg-slate-950">
  <div class="flex flex-1 items-center justify-center px-4 py-10">
    <div class="w-full max-w-md">
      <div class="mb-6 flex items-center justify-between">
        <span class="flex items-center gap-2.5">
          <TexShareLogo src={$adminMeta.logoUrl} size={30} />
          <span class="text-base font-bold tracking-tight text-slate-900 dark:text-white">{$adminMeta.appName}</span>
        </span>
        <LocaleTheme compact />
      </div>
      <div class="card card-pad fade-in">
        <span class="mb-3 flex h-11 w-11 items-center justify-center rounded-2xl bg-amber-100 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300">
          <Icon name="lock" size={22} />
        </span>
        <h1 class="text-xl font-bold text-slate-900 dark:text-white">{t('auth.mustChangeTitle')}</h1>
        <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('auth.mustChangeSubtitle')}</p>

        <form class="mt-6 space-y-4" on:submit={submit}>
          <div>
            <label class="label" for="cp-current">{t('auth.currentPassword')}</label>
            <input id="cp-current" class="input" type="password" autocomplete="current-password" bind:value={currentPassword} disabled={busy} />
          </div>
          <div>
            <label class="label" for="cp-new">{t('auth.newPassword')}</label>
            <input id="cp-new" class="input" type="password" autocomplete="new-password" bind:value={newPassword} disabled={busy} />
          </div>
          <div>
            <label class="label" for="cp-confirm">{t('auth.confirmPassword')}</label>
            <input id="cp-confirm" class="input" type="password" autocomplete="new-password" bind:value={confirmPassword} disabled={busy} />
          </div>

          {#if error}
            <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
              <Icon name="error" size={16} /><span>{error}</span>
            </p>
          {/if}

          <button type="submit" class="btn-primary w-full" disabled={busy}>
            {#if busy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('auth.changePassword')}
          </button>
          <button type="button" class="btn-ghost w-full" on:click={signOut}>
            <Icon name="logout" size={16} />{t('common.logout')}
          </button>
        </form>
      </div>
    </div>
  </div>
  <Footer admin version={$adminMeta.version} />
</div>
