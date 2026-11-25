<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { formatDateTime, formatNumber } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { confirmDialog, toast } from '../../lib/stores';
  import type { DNSInstruction, Domain, NetworkProvider, NetworkStatus } from '../../lib/types';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
  import Modal from '../../components/Modal.svelte';
  import Toggle from '../../components/Toggle.svelte';

  interface VerificationStatus {
    dnsResolved: boolean;
    addresses: string[];
    cname?: string;
    targetMatches: boolean;
    reachable: boolean;
    https: boolean;
    certValid: boolean;
    certError?: string;
    proxyDetected: boolean;
    publicUiOk: boolean;
    adminUiReachable: boolean;
    errors: string[];
    fixes: string[];
  }

  let domains: Domain[] = [];
  let loading = true;
  let busy = '';

  let providers: string[] = ['manual', 'cloudflare'];
  let proxyKinds: NetworkProvider[] = [];

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
  let dnsInstructions: DNSInstruction[] = [];
  let dnsStatus: VerificationStatus | null = null;

  // Binding editor state.
  let bindProvider = '';
  let bindRecordType = '';
  let bindTarget = '';
  let bindNetwork = '';
  let bindProxy = '';

  const networkModes = ['direct', 'proxy', 'tunnel', 'static', 'ddns'];

  onMount(load);

  async function load(): Promise<void> {
    loading = true;
    try {
      const res = await api<{ domains: Domain[] }>('/domains');
      domains = res.domains || [];
      try {
        const net = await api<NetworkStatus>('/network/status');
        if (net.providers?.length) providers = net.providers;
        proxyKinds = net.proxyKinds || [];
      } catch {
        // Network view may be unavailable; keep defaults.
      }
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

  function openDns(domain: Domain): void {
    dnsDomain = domain;
    bindProvider = domain.dnsProvider || '';
    bindRecordType = domain.dnsRecordType || 'A';
    bindTarget = domain.dnsTarget || '';
    bindNetwork = domain.networkMode || '';
    bindProxy = domain.proxyKind || '';
    dnsVerified = domain.verified;
    dnsReachable = domain.lastCheckOk === true;
    dnsTls = domain.tlsStatus;
    dnsError = domain.dnsError || '';
    dnsInstructions = [];
    dnsStatus = null;
    dnsOpen = true;
    void refreshInstructions();
  }

  async function refreshInstructions(): Promise<void> {
    if (!dnsDomain) return;
    try {
      const res = await api<{ instructions: DNSInstruction[] }>(
        `/network/dns/instructions?hostname=${encodeURIComponent(dnsDomain.hostname)}&mode=${encodeURIComponent(bindNetwork)}&type=${encodeURIComponent(bindRecordType || 'A')}`
      );
      dnsInstructions = res.instructions || [];
    } catch {
      dnsInstructions = [];
    }
  }

  async function saveBinding(): Promise<void> {
    if (!dnsDomain) return;
    busy = dnsDomain.hostname;
    try {
      await api(`/domains/${dnsDomain.id}`, {
        method: 'PATCH',
        body: {
          dnsProvider: bindProvider,
          dnsRecordType: bindRecordType,
          dnsTarget: bindTarget,
          networkMode: bindNetwork,
          proxyKind: bindProxy
        }
      });
      toast(t('admin.domains.saved'), 'success');
      await load();
      const updated = domains.find((d) => d.id === dnsDomain?.id);
      if (updated) dnsDomain = updated;
      await refreshInstructions();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function applyDns(): Promise<void> {
    if (!dnsDomain) return;
    busy = dnsDomain.hostname;
    try {
      const res = await api<{ domain: Domain }>('/network/dns/apply', {
        method: 'POST',
        body: { domainId: dnsDomain.id, type: bindRecordType, content: bindTarget }
      });
      dnsDomain = res.domain;
      toast(t('admin.domains.dnsApplied'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function deleteDns(): Promise<void> {
    if (!dnsDomain) return;
    const ok = await confirmDialog({ title: t('admin.domains.dnsDelete'), message: t('admin.domains.dnsDelete') + '?', danger: true });
    if (!ok) return;
    busy = dnsDomain.hostname;
    try {
      const res = await api<{ domain: Domain }>('/network/dns/delete', { method: 'POST', body: { domainId: dnsDomain.id } });
      dnsDomain = res.domain;
      toast(t('admin.domains.dnsDeleted'), 'success');
      await load();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function verify(domain: Domain): Promise<void> {
    busy = domain.hostname;
    try {
      const res = await api<{ domain: Domain; dnsVerified: boolean; reachable: boolean; tlsStatus: string; status: VerificationStatus; instructions: DNSInstruction[] }>(`/domains/${domain.id}/verify`, { method: 'POST', body: {} });
      dnsDomain = res.domain;
      dnsVerified = res.dnsVerified;
      dnsReachable = res.reachable;
      dnsTls = res.tlsStatus;
      dnsStatus = res.status;
      dnsInstructions = res.instructions || [];
      dnsError = (res.status?.errors || []).join('; ');
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
      const res = await api<{ domain: Domain; reachable: boolean; tlsStatus: string; error: string; status: VerificationStatus }>(`/domains/${domain.id}/check`, { method: 'POST', body: {} });
      dnsDomain = res.domain;
      dnsVerified = res.domain.verified;
      dnsReachable = res.reachable;
      dnsTls = res.tlsStatus;
      dnsStatus = res.status;
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

  function providerLabel(id: string): string {
    if (id === 'manual') return t('admin.network.dnsManual');
    if (id === 'cloudflare') return 'Cloudflare';
    if (id === 'none') return t('admin.network.dnsNone');
    return id;
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
                <span class="badge-slate">{t('admin.domains.dnsProvider')}: {providerLabel(domain.dnsProvider || 'manual')}</span>
                {#if domain.dnsStatus && domain.dnsStatus !== 'unknown'}
                  <span class={domain.dnsStatus === 'ok' ? 'badge-green' : 'badge-red'}>{t('admin.domains.dnsStatus')}: {domain.dnsStatus}</span>
                {/if}
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
            <button type="button" class="btn-secondary btn-sm" disabled={busy === domain.hostname} on:click={() => openDns(domain)}><Icon name="network" size={15} />{t('admin.domains.binding')}</button>
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

<!-- DNS & network binding -->
<Modal open={dnsOpen} title={t('admin.domains.binding')} on:close={() => (dnsOpen = false)}>
  {#if dnsDomain}
    <div class="space-y-5">
      <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.domains.bindingHint')}</p>

      <!-- Binding editor -->
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="bind-prov">{t('admin.domains.dnsProvider')}</label>
          <select id="bind-prov" class="input" bind:value={bindProvider} on:change={refreshInstructions}>
            <option value="">{t('admin.domains.inherit')}</option>
            {#each providers as p}<option value={p}>{providerLabel(p)}</option>{/each}
          </select>
        </div>
        <div>
          <label class="label" for="bind-type">{t('admin.domains.recordType')}</label>
          <select id="bind-type" class="input" bind:value={bindRecordType} on:change={refreshInstructions}>
            <option value="">{t('admin.domains.inherit')}</option>
            <option value="A">A</option>
            <option value="AAAA">AAAA</option>
            <option value="CNAME">CNAME</option>
          </select>
        </div>
        <div>
          <label class="label" for="bind-net">{t('admin.domains.networkMode')}</label>
          <select id="bind-net" class="input" bind:value={bindNetwork} on:change={refreshInstructions}>
            <option value="">{t('admin.domains.inherit')}</option>
            {#each networkModes as m}<option value={m}>{m}</option>{/each}
          </select>
        </div>
        <div>
          <label class="label" for="bind-proxy">{t('admin.domains.proxyKind')}</label>
          <select id="bind-proxy" class="input" bind:value={bindProxy}>
            <option value="">{t('admin.domains.inherit')}</option>
            {#each proxyKinds as k}<option value={k.id}>{k.label}</option>{/each}
          </select>
        </div>
        <div class="sm:col-span-2">
          <label class="label" for="bind-target">{t('admin.domains.target')}</label>
          <input id="bind-target" class="input" bind:value={bindTarget} placeholder="203.0.113.10" on:blur={refreshInstructions} />
          <p class="hint">{t('admin.domains.targetHint')}</p>
        </div>
      </div>

      <div class="flex flex-wrap justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={saveBinding} disabled={busy === dnsDomain.hostname}><Icon name="check" size={16} />{t('common.save')}</button>
        {#if (bindProvider || 'manual') !== 'manual' && (bindProvider || 'manual') !== 'none'}
          <button type="button" class="btn-primary" on:click={applyDns} disabled={busy === dnsDomain.hostname}><Icon name="upload" size={16} />{t('admin.domains.dnsApply')}</button>
          {#if dnsDomain.dnsRecordId}
            <button type="button" class="btn-ghost text-rose-600 dark:text-rose-400" on:click={deleteDns} disabled={busy === dnsDomain.hostname}><Icon name="trash" size={16} />{t('admin.domains.dnsDelete')}</button>
          {/if}
        {:else}
          <span class="self-center text-xs text-slate-500 dark:text-slate-400">{t('admin.domains.dnsManualNote')}</span>
        {/if}
      </div>

      <!-- Manual instructions -->
      {#if dnsInstructions.length > 0}
        <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <h3 class="text-sm font-semibold text-slate-900 dark:text-white">{t('admin.domains.dnsInstructions')}</h3>
          <div class="mt-3 overflow-x-auto">
            <table class="w-full text-left text-sm">
              <thead class="text-xs uppercase tracking-wide text-slate-400">
                <tr>
                  <th class="py-1.5 pr-4">{t('admin.network.recordType')}</th>
                  <th class="py-1.5 pr-4">{t('admin.network.recordName')}</th>
                  <th class="py-1.5 pr-4">{t('admin.network.recordTarget')}</th>
                  <th class="py-1.5">{t('admin.network.recordHint')}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100 dark:divide-slate-800">
                {#each dnsInstructions as rec}
                  <tr>
                    <td class="py-2 pr-4 font-mono text-xs text-slate-800 dark:text-slate-100">{rec.type}</td>
                    <td class="py-2 pr-4 font-mono text-xs text-slate-800 dark:text-slate-100">{rec.name}</td>
                    <td class="py-2 pr-4 font-mono text-xs text-slate-800 dark:text-slate-100">{rec.content}</td>
                    <td class="py-2 text-xs text-slate-500 dark:text-slate-400">{rec.hint || ''}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      {/if}

      <!-- TXT verification -->
      <div class="space-y-3 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
        <p class="text-sm text-slate-500 dark:text-slate-400">{t('admin.domains.dnsHint')}</p>
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

      <!-- Status matrix -->
      {#if dnsStatus}
        <div class="rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <h3 class="text-sm font-semibold text-slate-900 dark:text-white">{t('admin.domains.statusTitle')}</h3>
          <div class="mt-3 grid gap-2 sm:grid-cols-2">
            <span class={dnsStatus.dnsResolved ? 'badge-green' : 'badge-red'}>{t('admin.domains.stDns')}</span>
            <span class={dnsStatus.targetMatches ? 'badge-green' : 'badge-amber'}>{t('admin.domains.stTarget')}</span>
            <span class={dnsStatus.reachable ? 'badge-green' : 'badge-red'}>{t('admin.domains.stReachable')}</span>
            <span class={dnsStatus.https ? 'badge-green' : 'badge-amber'}>{t('admin.domains.stHttps')}</span>
            <span class={dnsStatus.certValid ? 'badge-green' : 'badge-amber'}>{t('admin.domains.stCert')}</span>
            <span class={dnsStatus.proxyDetected ? 'badge-green' : 'badge-slate'}>{t('admin.domains.stProxy')}</span>
            <span class={dnsStatus.publicUiOk ? 'badge-green' : 'badge-red'}>{t('admin.domains.stPublic')}</span>
            <span class={dnsStatus.adminUiReachable ? 'badge-green' : 'badge-slate'}>{t('admin.domains.stAdmin')}</span>
          </div>
          {#if dnsStatus.addresses?.length}
            <p class="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('admin.domains.addresses')}: <span class="font-mono">{dnsStatus.addresses.join(', ')}</span></p>
          {/if}
          {#if dnsStatus.fixes?.length}
            <div class="mt-3">
              <p class="text-xs font-semibold uppercase tracking-wide text-slate-400">{t('admin.domains.fixes')}</p>
              <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-600 dark:text-slate-300">
                {#each dnsStatus.fixes as fix}<li>{fix}</li>{/each}
              </ul>
            </div>
          {/if}
        </div>
      {/if}

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
        <button type="button" class="btn-primary" on:click={() => dnsDomain && verify(dnsDomain)} disabled={busy === dnsDomain.hostname}>
          {#if busy === dnsDomain.hostname}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="shield" size={16} />{/if}
          {t('admin.domains.verifyFull')}
        </button>
      </div>
    </div>
  {/if}
</Modal>
