<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { formatDateTime, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { Domain } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';

  let domains: Domain[] = [];
  let loading = true;
  let busy = '';

  let createOpen = false;
  let newHostname = '';
  let newDefault = false;
  let createBusy = false;
  let createError = '';

  let dnsOpen = false;
  let dnsDomain: Domain | null = null;
  let dnsVerified = false;
  let dnsReachable = false;
  let dnsTls = '';
  let dnsError = '';

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    try {
      const res = await api<{ domains: Domain[] }>('/domains');
      domains = res.domains || [];
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      loading = false;
    }
  }

  async function create(): Promise<void> {
    createError = '';
    if (!newHostname.trim()) {
      createError = t('common.requiredField');
      return;
    }
    createBusy = true;
    try {
      await api('/domains', { method: 'POST', body: { hostname: newHostname.trim(), isDefault: newDefault } });
      createOpen = false;
      newHostname = '';
      newDefault = false;
      toast(t('admin.domains.saved'), 'success');
      await load();
    } catch (err) {
      createError = errorText(err);
    } finally {
      createBusy = false;
    }
  }

  async function patch(domain: Domain, body: Record<string, unknown>): Promise<void> {
    busy = domain.hostname;
    try {
      await api(`/domains/${domain.id}`, { method: 'PATCH', body });
      toast(t('admin.domains.saved'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function remove(domain: Domain): Promise<void> {
    const ok = await confirmDialog({ title: t('admin.domains.delete'), message: t('admin.domains.deleteConfirm', { hostname: domain.hostname }), danger: true });
    if (!ok) return;
    try {
      await api(`/domains/${domain.id}`, { method: 'DELETE' });
      toast(t('admin.common.success'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    }
  }

  async function verify(domain: Domain): Promise<void> {
    busy = domain.hostname;
    try {
      const res = await api<{ domain: Domain; dnsVerified: boolean; reachable: boolean; tlsStatus: string }>(`/domains/${domain.id}/verify`, { method: 'POST', body: {} });
      dnsDomain = res.domain;
      dnsVerified = res.dnsVerified;
      dnsReachable = res.reachable;
      dnsTls = res.tlsStatus;
      dnsError = '';
      dnsOpen = true;
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function check(domain: Domain): Promise<void> {
    busy = domain.hostname;
    try {
      const res = await api<{ domain: Domain; reachable: boolean; tlsStatus: string; error: string }>(`/domains/${domain.id}/check`, { method: 'POST', body: {} });
      dnsDomain = res.domain;
      dnsVerified = res.domain.verified;
      dnsReachable = res.reachable;
      dnsTls = res.tlsStatus;
      dnsError = res.error || '';
      dnsOpen = true;
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function copy(value: string): Promise<void> {
    try {
      await copyText(value);
      toast(t('common.copied'), 'success');
    } catch {
      toast(t('common.error'), 'error');
    }
  }
</script>

<div class="space-y-4">
  <div class="flex justify-end">
    <button type="button" class="btn-primary" on:click={() => (createOpen = true)}><Icon name="plus" size={16} />{t('admin.domains.new')}</button>
  </div>

  {#if loading}
    <div class="space-y-2">{#each Array(4) as _}<div class="skeleton h-20"></div>{/each}</div>
  {:else if domains.length === 0}
    <div class="card"><EmptyState icon="globe" title={t('admin.domains.title')} message={t('admin.domains.subtitle')} /></div>
  {:else}
    <div class="grid gap-4 lg:grid-cols-2">
      {#each domains as domain (domain.id)}
        <div class="card card-pad">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <span class="truncate text-base font-semibold text-slate-900 dark:text-white">{domain.hostname}</span>
                {#if domain.isDefault}<span class="badge-brand">{t('admin.domains.default')}</span>{/if}
              </div>
              <div class="mt-2 flex flex-wrap gap-1.5">
                {#if domain.verified}<span class="badge-green">{t('admin.domains.verified')}</span>{:else}<span class="badge-amber">{t('common.unverified')}</span>{/if}
                {#if domain.enabled}<span class="badge-slate">{t('common.enabled')}</span>{:else}<span class="badge-red">{t('common.disabled')}</span>{/if}
                <span class="badge-slate">{t('admin.domains.tls')}: {domain.tlsStatus}</span>
              </div>
            </div>
          </div>

          <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
            <div>
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.domains.users')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(domain.userCount || 0)}</dd>
            </div>
            <div>
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.domains.shares')}</dt>
              <dd class="font-medium text-slate-800 dark:text-slate-100">{formatNumber(domain.shareCount || 0)}</dd>
            </div>
            <div class="col-span-2">
              <dt class="text-slate-500 dark:text-slate-400">{t('admin.domains.lastCheck')}</dt>
              <dd class="flex items-center gap-2 font-medium text-slate-800 dark:text-slate-100">
                {domain.lastCheckedAt ? formatDateTime(domain.lastCheckedAt) : t('common.never')}
                {#if domain.lastCheckOk === true}<span class="badge-green">{t('admin.domains.checkOk')}</span>{:else if domain.lastCheckOk === false}<span class="badge-red">{t('admin.domains.checkFailed')}</span>{/if}
              </dd>
            </div>
          </dl>

          <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-slate-100 pt-4 dark:border-slate-800">
            <div on:click={() => busy !== domain.hostname && patch(domain, { enabled: !domain.enabled })}>
              <Toggle checked={domain.enabled} label={t('admin.domains.enabled')} disabled={busy === domain.hostname} />
            </div>
          </div>

          <div class="mt-3 flex flex-wrap gap-2">
            {#if !domain.isDefault}
              <button type="button" class="btn-secondary btn-sm" disabled={busy === domain.hostname} on:click={() => patch(domain, { isDefault: true })}><Icon name="star" size={15} />{t('admin.domains.default')}</button>
            {/if}
            <button type="button" class="btn-secondary btn-sm" disabled={busy === domain.hostname} on:click={() => verify(domain)}><Icon name="shield" size={15} />{t('admin.domains.verify')}</button>
            <button type="button" class="btn-secondary btn-sm" disabled={busy === domain.hostname} on:click={() => check(domain)}><Icon name="refresh" size={15} />{t('admin.domains.check')}</button>
            <button type="button" class="btn-ghost btn-sm text-rose-600 dark:text-rose-400" on:click={() => remove(domain)}><Icon name="trash" size={15} />{t('admin.domains.delete')}</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- Create -->
<Modal open={createOpen} title={t('admin.domains.new')} size="sm" on:close={() => (createOpen = false)}>
  <div class="space-y-4">
    <div>
      <label class="label" for="dom-host">{t('admin.domains.hostname')}</label>
      <input id="dom-host" class="input" placeholder="files.example.com" bind:value={newHostname} disabled={createBusy} />
      <p class="hint">{t('admin.domains.hostnameHint')}</p>
    </div>
    <Toggle bind:checked={newDefault} label={t('admin.domains.default')} hint={t('admin.domains.defaultHint')} />
    {#if createError}
      <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300"><Icon name="error" size={16} /><span>{createError}</span></p>
    {/if}
    <div class="flex justify-end gap-2">
      <button type="button" class="btn-secondary" on:click={() => (createOpen = false)}>{t('common.cancel')}</button>
      <button type="button" class="btn-primary" disabled={createBusy} on:click={create}>
        {#if createBusy}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="plus" size={16} />{/if}
        {t('common.create')}
      </button>
    </div>
  </div>
</Modal>

<!-- DNS / verify -->
<Modal open={dnsOpen} title={t('admin.domains.dnsTitle')} on:close={() => (dnsOpen = false)}>
  {#if dnsDomain}
    <div class="space-y-4">
      <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.domains.dnsHint')}</p>

      <div class="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
        <div>
          <span class="label">{t('admin.domains.dnsName')}</span>
          <div class="flex items-center gap-2 rounded-lg bg-slate-100 px-3 py-2 dark:bg-slate-800">
            <code class="min-w-0 flex-1 break-all font-mono text-xs text-slate-800 dark:text-slate-100">_texshare-verify.{dnsDomain.hostname}</code>
            <button type="button" class="btn-ghost btn-sm" on:click={() => copy(`_texshare-verify.${dnsDomain?.hostname}`)}><Icon name="copy" size={14} /></button>
          </div>
        </div>
        <div>
          <span class="label">{t('admin.domains.dnsValue')}</span>
          <div class="flex items-center gap-2 rounded-lg bg-slate-100 px-3 py-2 dark:bg-slate-800">
            <code class="min-w-0 flex-1 break-all font-mono text-xs text-slate-800 dark:text-slate-100">{dnsDomain.verificationToken}</code>
            <button type="button" class="btn-ghost btn-sm" on:click={() => copy(dnsDomain?.verificationToken || '')}><Icon name="copy" size={14} /></button>
          </div>
        </div>
      </div>

      <div class="flex flex-wrap gap-2">
        {#if dnsVerified}<span class="badge-green"><Icon name="check" size={12} />{t('admin.domains.verifiedMsg')}</span>{:else}<span class="badge-amber">{t('admin.domains.verifyFailed')}</span>{/if}
        {#if dnsReachable}<span class="badge-green">{t('admin.domains.checkOk')}</span>{:else}<span class="badge-red">{t('admin.domains.checkFailed')}</span>{/if}
        {#if dnsTls}<span class="badge-slate">{t('admin.domains.tls')}: {dnsTls}</span>{/if}
      </div>

      {#if dnsError}
        <p class="rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-200">{dnsError}</p>
      {/if}

      <div class="flex justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={() => (dnsOpen = false)}>{t('common.close')}</button>
        <button type="button" class="btn-primary" on:click={() => dnsDomain && verify(dnsDomain)}><Icon name="shield" size={16} />{t('admin.domains.verify')}</button>
      </div>
    </div>
  {/if}
</Modal>
