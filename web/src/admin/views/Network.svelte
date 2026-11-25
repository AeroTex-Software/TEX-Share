<script lang="ts">
  import { onMount } from 'svelte';
  import { api, copyText, errorText } from '../../lib/api';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { toast } from '../../lib/stores';
  import type { DNSInstruction, NetworkStatus, ProxyGuide } from '../../lib/types';
  import Icon from '../../components/Icon.svelte';
  import Toggle from '../../components/Toggle.svelte';

  interface Zone {
    id: string;
    name: string;
  }

  interface DDNSStatus {
    enabled?: boolean;
    provider?: string;
    publicIPv4?: string;
    publicIPv6?: string;
    lastIPv4?: string;
    lastIPv6?: string;
    lastCheck?: number;
    lastUpdate?: number;
    lastError?: string;
    retries?: number;
  }

  interface DDNSLogEntry {
    id: number;
    ts: number;
    provider: string;
    recordType: string;
    hostname: string;
    oldValue: string;
    newValue: string;
    result: string;
    message: string;
  }

  let loading = true;
  let busy = '';
  let error = '';

  let status: NetworkStatus | null = null;
  let guide: ProxyGuide | null = null;

  // Wizard selections.
  let dnsProvider = 'manual';
  let connection = 'direct';
  let proxyKind = 'none';
  let proxyHostname = '';
  let proxyUpstream = '127.0.0.1';
  let adminExposed = false;
  let trustedProxies = '';

  // Cloudflare.
  let cfToken = '';
  let cfZoneId = '';
  let cfZoneName = '';
  let zones: Zone[] = [];

  // DynDNS.
  let ddnsEnabled = false;
  let ddnsProvider = 'cloudflare';
  let ddnsInterval = 5;
  let ddnsIpv4 = true;
  let ddnsIpv6 = false;
  let ddnsLog: DDNSLogEntry[] = [];

  // Manual DNS preview + detected IPs.
  let instructions: DNSInstruction[] = [];
  let detectBusy = false;
  let detected: { ipv4?: string; ipv6?: string; ipv4Error?: string; ipv6Error?: string } | null = null;

  const dnsOptions = [
    { id: 'manual', icon: 'server', label: 'admin.network.dnsManual', hint: 'admin.network.dnsManualHint' },
    { id: 'cloudflare', icon: 'cloud', label: 'admin.network.dnsCloudflare', hint: 'admin.network.dnsCloudflareHint' },
    { id: 'none', icon: 'ban', label: 'admin.network.dnsNone', hint: 'admin.network.dnsNoneHint' }
  ];

  const connOptions = [
    { id: 'direct', icon: 'network', label: 'admin.network.connDirect', hint: 'admin.network.connDirectHint' },
    { id: 'proxy', icon: 'server', label: 'admin.network.connProxy', hint: 'admin.network.connProxyHint' },
    { id: 'tunnel', icon: 'cloud', label: 'admin.network.connTunnel', hint: 'admin.network.connTunnelHint' },
    { id: 'static', icon: 'globe', label: 'admin.network.connStatic', hint: 'admin.network.connStaticHint' },
    { id: 'ddns', icon: 'refresh', label: 'admin.network.connDdns', hint: 'admin.network.connDdnsHint' }
  ];

  onMount(load);

  function cfg(key: string, fallback = ''): string {
    return status?.config?.[key] ?? fallback;
  }

  async function load(): Promise<void> {
    loading = true;
    error = '';
    try {
      status = await api<NetworkStatus>('/network/status');
      dnsProvider = cfg('network_dns_provider', 'manual') || 'manual';
      connection = cfg('network_connection', 'direct') || 'direct';
      proxyKind = cfg('network_proxy_kind', 'none') || 'none';
      proxyHostname = cfg('network_proxy_hostname');
      proxyUpstream = cfg('network_proxy_upstream', '127.0.0.1') || '127.0.0.1';
      adminExposed = cfg('network_admin_exposed') === '1';
      trustedProxies = cfg('trusted_proxies');
      cfZoneId = cfg('cf_zone_id');
      cfZoneName = cfg('cf_zone_name');
      ddnsEnabled = cfg('ddns_enabled') === '1';
      ddnsProvider = cfg('ddns_provider', 'cloudflare') || 'cloudflare';
      ddnsInterval = Number.parseInt(cfg('ddns_interval_minutes', '5'), 10) || 5;
      ddnsIpv4 = cfg('ddns_ipv4') === '1';
      ddnsIpv6 = cfg('ddns_ipv6') === '1';
      guide = status.guide || null;
      await Promise.all([loadGuide(), loadLog(), loadInstructions()]);
      if (dnsProvider === 'cloudflare' && status.tokenSet) {
        void loadZones();
      }
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      loading = false;
    }
  }

  async function save(extra: Record<string, string> = {}): Promise<void> {
    busy = 'save';
    try {
      const body: Record<string, string> = {
        network_dns_provider: dnsProvider,
        network_connection: connection,
        network_proxy_kind: proxyKind,
        network_proxy_hostname: proxyHostname.trim(),
        network_proxy_upstream: proxyUpstream.trim(),
        network_admin_exposed: adminExposed ? '1' : '0',
        trusted_proxies: trustedProxies.trim(),
        cf_zone_id: cfZoneId,
        cf_zone_name: cfZoneName,
        ddns_enabled: ddnsEnabled ? '1' : '0',
        ddns_provider: ddnsProvider,
        ddns_interval_minutes: String(ddnsInterval),
        ddns_ipv4: ddnsIpv4 ? '1' : '0',
        ddns_ipv6: ddnsIpv6 ? '1' : '0',
        ...extra
      };
      status = await api<NetworkStatus>('/network', { method: 'PATCH', body: { settings: body } });
      toast(t('admin.network.saved'), 'success');
      await Promise.all([loadGuide(), loadInstructions()]);
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      busy = '';
    }
  }

  async function pickDns(id: string): Promise<void> {
    dnsProvider = id;
    await save();
  }

  async function pickConnection(id: string): Promise<void> {
    connection = id;
    await save();
  }

  async function pickProxy(id: string): Promise<void> {
    proxyKind = id;
    await save();
  }

  async function loadGuide(): Promise<void> {
    try {
      const res = await api<{ guide: ProxyGuide }>(`/network/guide?kind=${encodeURIComponent(proxyKind)}&hostname=${encodeURIComponent(proxyHostname.trim())}`);
      guide = res.guide;
    } catch {
      // Guide is best effort.
    }
  }

  async function loadInstructions(): Promise<void> {
    if (dnsProvider === 'none') {
      instructions = [];
      return;
    }
    try {
      const res = await api<{ instructions: DNSInstruction[] }>(
        `/network/dns/instructions?hostname=${encodeURIComponent(proxyHostname.trim())}&mode=${encodeURIComponent(connection)}`
      );
      instructions = res.instructions || [];
    } catch {
      instructions = [];
    }
  }

  async function saveCloudflare(): Promise<void> {
    if (!cfToken.trim()) {
      toast(t('admin.network.cloudflareTokenMissing'), 'error');
      return;
    }
    busy = 'cf';
    try {
      const res = await api<{ ok: boolean; zones: Zone[] }>('/network/cloudflare/token', { method: 'POST', body: { token: cfToken.trim() } });
      zones = res.zones || [];
      cfToken = '';
      toast(t('admin.network.cloudflareTokenSet'), 'success');
      await load();
    } catch (err) {
      error = errorText(err);
      toast(error, 'error');
    } finally {
      busy = '';
    }
  }

  async function loadZones(): Promise<void> {
    try {
      const res = await api<{ zones: Zone[] }>('/network/cloudflare/zones');
      zones = res.zones || [];
    } catch {
      zones = [];
    }
  }

  async function pickZone(id: string): Promise<void> {
    cfZoneId = id;
    cfZoneName = zones.find((z) => z.id === id)?.name || '';
    await save();
  }

  async function detect(): Promise<void> {
    detectBusy = true;
    try {
      detected = await api<{ ipv4?: string; ipv6?: string; ipv4Error?: string; ipv6Error?: string }>('/network/detect-ip', { method: 'POST', body: {} });
      await save();
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      detectBusy = false;
    }
  }

  async function runDdns(): Promise<void> {
    busy = 'ddns';
    try {
      const res = await api<{ ok: boolean; error?: string }>('/network/ddns/run', { method: 'POST', body: {} });
      if (res.ok) toast(t('admin.network.ddnsRun'), 'success');
      else toast(res.error || t('common.error'), 'error');
      await Promise.all([load(), loadLog()]);
    } catch (err) {
      toast(errorText(err), 'error');
    } finally {
      busy = '';
    }
  }

  async function loadLog(): Promise<void> {
    try {
      const res = await api<{ entries: DDNSLogEntry[] }>('/network/ddns/log');
      ddnsLog = res.entries || [];
    } catch {
      ddnsLog = [];
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

  $: ddns = (status?.ddns || {}) as DDNSStatus;
  $: cfEnabled = dnsProvider === 'cloudflare';

  function tunnelRunning(): boolean {
    const tn = status?.tunnel as Record<string, unknown> | undefined;
    return !!tn && tn['status'] === 'running';
  }
</script>

{#if loading}
  <div class="space-y-4">{#each Array(3) as _}<div class="skeleton h-40"></div>{/each}</div>
{:else}
  <div class="space-y-5">
    {#if error}
      <p class="flex items-start gap-2 rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:bg-rose-500/10 dark:text-rose-300">
        <Icon name="error" size={16} /><span>{error}</span>
      </p>
    {/if}

    <!-- Step 1: DNS binding -->
    <section class="card card-pad">
      <div class="flex items-start gap-3">
        <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-brand-600 text-sm font-bold text-white">1</span>
        <div>
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="globe" size={18} />{t('admin.network.stepDns')}</h2>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.network.stepDnsHint')}</p>
        </div>
      </div>

      <div class="mt-4 grid gap-3 sm:grid-cols-3">
        {#each dnsOptions as opt}
          <button
            type="button"
            class="rounded-xl border p-4 text-left transition {dnsProvider === opt.id
              ? 'border-brand-500 bg-brand-50 dark:border-brand-500 dark:bg-brand-500/10'
              : 'border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700'}"
            on:click={() => pickDns(opt.id)}
            disabled={busy === 'save'}
          >
            <span class="flex items-center gap-2 text-sm font-semibold text-slate-900 dark:text-white"><Icon name={opt.icon} size={16} />{t(opt.label)}</span>
            <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">{t(opt.hint)}</span>
          </button>
        {/each}
      </div>

      {#if cfEnabled}
        <div class="mt-4 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="flex items-center gap-2 text-sm font-semibold text-slate-900 dark:text-white"><Icon name="cloud" size={16} />{t('admin.network.cloudflareToken')}</h3>
            {#if status?.tokenSet}<span class="badge-green"><Icon name="check" size={12} />{t('admin.network.cloudflareTokenSet')}</span>{:else}<span class="badge-amber">{t('admin.network.cloudflareTokenMissing')}</span>{/if}
          </div>
          <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">{t('admin.network.cloudflareTokenHint')}</p>
          <div class="mt-3 flex flex-wrap gap-2">
            <input class="input max-w-md flex-1" type="password" placeholder="API token" bind:value={cfToken} disabled={busy === 'cf'} autocomplete="off" />
            <button type="button" class="btn-primary" on:click={saveCloudflare} disabled={busy === 'cf'}>
              {#if busy === 'cf'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="shield" size={16} />{/if}
              {t('admin.network.cloudflareTest')}
            </button>
          </div>
          {#if zones.length > 0}
            <div class="mt-4">
              <label class="label" for="net-zone">{t('admin.network.cloudflareZone')}</label>
              <select id="net-zone" class="input max-w-md" bind:value={cfZoneId} on:change={() => pickZone(cfZoneId)}>
                <option value="">—</option>
                {#each zones as z}<option value={z.id}>{z.name}</option>{/each}
              </select>
            </div>
          {:else if status?.tokenSet}
            <p class="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('admin.network.cloudflareNoZones')}</p>
          {/if}
        </div>
      {/if}
    </section>

    <!-- Step 2: network path -->
    <section class="card card-pad">
      <div class="flex items-start gap-3">
        <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-brand-600 text-sm font-bold text-white">2</span>
        <div>
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="network" size={18} />{t('admin.network.stepConnection')}</h2>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.network.stepConnectionHint')}</p>
        </div>
      </div>
      <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {#each connOptions as opt}
          <button
            type="button"
            class="rounded-xl border p-4 text-left transition {connection === opt.id
              ? 'border-brand-500 bg-brand-50 dark:border-brand-500 dark:bg-brand-500/10'
              : 'border-slate-200 hover:border-slate-300 dark:border-slate-800 dark:hover:border-slate-700'}"
            on:click={() => pickConnection(opt.id)}
            disabled={busy === 'save'}
          >
            <span class="flex items-center gap-2 text-sm font-semibold text-slate-900 dark:text-white"><Icon name={opt.icon} size={16} />{t(opt.label)}</span>
            <span class="mt-1 block text-xs text-slate-500 dark:text-slate-400">{t(opt.hint)}</span>
          </button>
        {/each}
      </div>
      {#if connection === 'ddns' || connection === 'direct' || connection === 'static'}
        <p class="mt-3 flex items-start gap-2 rounded-xl bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-500/10 dark:text-amber-200"><Icon name="info" size={14} /><span>{t('admin.network.ddnsCgnat')}</span></p>
      {/if}
    </section>

    <!-- Step 3: reverse proxy -->
    <section class="card card-pad">
      <div class="flex items-start gap-3">
        <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-brand-600 text-sm font-bold text-white">3</span>
        <div>
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="server" size={18} />{t('admin.network.stepProxy')}</h2>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.network.stepProxyHint')}</p>
        </div>
      </div>

      <div class="mt-4 grid gap-3 sm:grid-cols-3 lg:grid-cols-4">
        {#each status?.proxyKinds || [] as k}
          <button
            type="button"
            class="rounded-xl border p-3 text-left text-sm font-semibold transition {proxyKind === k.id
              ? 'border-brand-500 bg-brand-50 text-slate-900 dark:border-brand-500 dark:bg-brand-500/10 dark:text-white'
              : 'border-slate-200 text-slate-700 hover:border-slate-300 dark:border-slate-800 dark:text-slate-200 dark:hover:border-slate-700'}"
            on:click={() => pickProxy(k.id)}
            disabled={busy === 'save'}
          >{k.label}</button>
        {/each}
      </div>

      {#if proxyKind !== 'none'}
        <div class="mt-4 grid gap-4 sm:grid-cols-2">
          <div>
            <label class="label" for="net-host">{t('admin.network.hostname')}</label>
            <input id="net-host" class="input" placeholder="share.example.com" bind:value={proxyHostname} />
            <p class="hint">{t('admin.network.hostnameHint')}</p>
          </div>
          <div>
            <label class="label" for="net-upstream">{t('admin.network.upstream')}</label>
            <input id="net-upstream" class="input" bind:value={proxyUpstream} />
          </div>
        </div>
        <div class="mt-3 flex flex-wrap items-center gap-3">
          <Toggle bind:checked={adminExposed} label={t('admin.network.adminExposed')} hint={t('admin.network.adminExposedHint')} />
        </div>
        <div class="mt-3 flex justify-end">
          <button type="button" class="btn-secondary" on:click={() => save()} disabled={busy === 'save'}>
            {#if busy === 'save'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('common.save')}
          </button>
        </div>
      {/if}
    </section>

    <!-- Required settings / guide -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="info" size={18} />{t('admin.network.requiredSettings')}</h2>
      <div class="mt-3 flex flex-wrap gap-2">
        <span class="badge-slate">{t('admin.network.publicPort')}: <strong>{status?.publicPort}</strong></span>
        <span class="badge-slate">{t('admin.network.adminPort')}: <strong>{status?.adminPort}</strong></span>
        {#if connection === 'tunnel'}
          <span class={tunnelRunning() ? 'badge-green' : 'badge-slate'}>
            {t('admin.network.tunnel')}: {tunnelRunning() ? t('admin.network.connected') : t('admin.network.disconnected')}
          </span>
        {/if}
      </div>

      {#if guide && proxyKind !== 'none'}
        <div class="mt-4">
          <div class="flex items-center justify-between">
            <h3 class="text-sm font-semibold text-slate-900 dark:text-white">{t('admin.network.guide')}: {guide.label}</h3>
            <button type="button" class="btn-ghost btn-sm" on:click={() => copy(guide?.config || '')}><Icon name="copy" size={14} />{t('admin.network.copyConfig')}</button>
          </div>
          <pre class="mt-2 max-h-96 overflow-auto rounded-xl bg-slate-900 p-4 text-xs leading-relaxed text-slate-100"><code>{guide.config}</code></pre>
          {#if guide.notes?.length}
            <p class="mt-3 text-xs font-semibold uppercase tracking-wide text-slate-400">{t('admin.network.notes')}</p>
            <ul class="mt-1 list-disc space-y-1 pl-5 text-sm text-slate-600 dark:text-slate-300">
              {#each guide.notes as note}<li>{note}</li>{/each}
            </ul>
          {/if}
        </div>
      {/if}

      {#if dnsProvider === 'manual' && instructions.length > 0}
        <div class="mt-4 rounded-xl border border-slate-200 p-4 dark:border-slate-800">
          <h3 class="text-sm font-semibold text-slate-900 dark:text-white">{t('admin.network.manualTitle')}</h3>
          <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">{t('admin.network.manualHint')}</p>
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
                {#each instructions as rec}
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
    </section>

    <!-- Public IP + trusted proxies -->
    <section class="card card-pad">
      <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="globe" size={18} />{t('admin.network.publicIpv4')} / {t('admin.network.publicIpv6')}</h2>
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.publicIpv4')}</p>
          <p class="mt-1 font-mono text-sm text-slate-800 dark:text-slate-100">{cfg('public_ipv4') || '—'}</p>
        </div>
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.publicIpv6')}</p>
          <p class="mt-1 font-mono text-sm text-slate-800 dark:text-slate-100">{cfg('public_ipv6') || '—'}</p>
        </div>
      </div>
      <div class="mt-3 flex flex-wrap items-center gap-3">
        <button type="button" class="btn-secondary btn-sm" on:click={detect} disabled={detectBusy}>
          {#if detectBusy}<Icon name="spinner" size={15} class="animate-spin" />{:else}<Icon name="refresh" size={15} />{/if}
          {detectBusy ? t('admin.network.detecting') : t('admin.network.detectIp')}
        </button>
        {#if detected?.ipv4Error}<span class="text-xs text-amber-700 dark:text-amber-300">IPv4: {detected.ipv4Error}</span>{/if}
        {#if detected?.ipv6Error}<span class="text-xs text-amber-700 dark:text-amber-300">IPv6: {detected.ipv6Error}</span>{/if}
      </div>

      <div class="mt-5 border-t border-slate-100 pt-4 dark:border-slate-800">
        <label class="label" for="net-trusted">{t('admin.network.trustedProxies')}</label>
        <input id="net-trusted" class="input" placeholder="127.0.0.1/32, 172.16.0.0/12" bind:value={trustedProxies} />
        <p class="hint">{t('admin.network.trustedProxiesHint')}</p>
        {#if status?.trustedProxies?.length}
          <div class="mt-2 flex flex-wrap gap-1.5">
            {#each status.trustedProxies as p}<span class="badge-slate font-mono">{p}</span>{/each}
          </div>
        {:else}
          <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('admin.network.trustedProxiesEmpty')}</p>
        {/if}
        <div class="mt-3 flex justify-end">
          <button type="button" class="btn-secondary" on:click={() => save()} disabled={busy === 'save'}>
            {#if busy === 'save'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="check" size={16} />{/if}
            {t('common.save')}
          </button>
        </div>
      </div>
    </section>

    <!-- DynDNS -->
    <section class="card card-pad">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 class="flex items-center gap-2 text-base font-semibold text-slate-900 dark:text-white"><Icon name="refresh" size={18} />{t('admin.network.ddns')}</h2>
          <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">{t('admin.network.ddnsHint')}</p>
        </div>
        <Toggle bind:checked={ddnsEnabled} label={t('admin.network.ddnsEnabled')} />
      </div>

      <div class="mt-4 grid gap-4 sm:grid-cols-2">
        <div>
          <label class="label" for="net-ddns-prov">{t('admin.network.ddnsProvider')}</label>
          <select id="net-ddns-prov" class="input" bind:value={ddnsProvider}>
            <option value="cloudflare">Cloudflare</option>
            <option value="manual">{t('admin.network.dnsManual')}</option>
          </select>
        </div>
        <div>
          <label class="label" for="net-ddns-int">{t('admin.network.ddnsInterval')}</label>
          <input id="net-ddns-int" class="input" type="number" min="1" max="1440" bind:value={ddnsInterval} />
        </div>
      </div>
      <div class="mt-3 grid gap-3 sm:grid-cols-2">
        <Toggle bind:checked={ddnsIpv4} label={t('admin.network.ddnsIpv4')} />
        <Toggle bind:checked={ddnsIpv6} label={t('admin.network.ddnsIpv6')} />
      </div>

      <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.ddnsLastCheck')}</p>
          <p class="mt-1 text-sm text-slate-800 dark:text-slate-100">{ddns.lastCheck ? formatDateTime(ddns.lastCheck) : t('common.never')}</p>
        </div>
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.ddnsLastUpdate')}</p>
          <p class="mt-1 text-sm text-slate-800 dark:text-slate-100">{ddns.lastUpdate ? formatDateTime(ddns.lastUpdate) : t('common.never')}</p>
        </div>
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.ddnsLastError')}</p>
          <p class="mt-1 break-all text-sm text-slate-800 dark:text-slate-100">{ddns.lastError || '—'}</p>
        </div>
        <div class="rounded-xl border border-slate-200 p-3 dark:border-slate-800">
          <p class="text-xs text-slate-500 dark:text-slate-400">{t('admin.network.ddnsRetries')}</p>
          <p class="mt-1 text-sm text-slate-800 dark:text-slate-100">{ddns.retries || 0}</p>
        </div>
      </div>

      <div class="mt-4 flex flex-wrap justify-end gap-2">
        <button type="button" class="btn-secondary" on:click={() => save()} disabled={busy === 'save'}><Icon name="check" size={16} />{t('common.save')}</button>
        <button type="button" class="btn-primary" on:click={runDdns} disabled={busy === 'ddns'}>
          {#if busy === 'ddns'}<Icon name="spinner" size={16} class="animate-spin" />{:else}<Icon name="refresh" size={16} />{/if}
          {t('admin.network.ddnsRun')}
        </button>
      </div>

      <div class="mt-5 border-t border-slate-100 pt-4 dark:border-slate-800">
        <h3 class="text-sm font-semibold text-slate-900 dark:text-white">{t('admin.network.ddnsLog')}</h3>
        {#if ddnsLog.length === 0}
          <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('admin.network.ddnsNoLog')}</p>
        {:else}
          <div class="mt-2 overflow-x-auto">
            <table class="w-full text-left text-xs">
              <thead class="uppercase tracking-wide text-slate-400">
                <tr>
                  <th class="py-1.5 pr-4">{t('admin.domains.lastCheck')}</th>
                  <th class="py-1.5 pr-4">{t('admin.network.recordType')}</th>
                  <th class="py-1.5 pr-4">{t('admin.domains.hostname')}</th>
                  <th class="py-1.5 pr-4">{t('admin.network.recordTarget')}</th>
                  <th class="py-1.5">{t('admin.network.status')}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100 dark:divide-slate-800">
                {#each ddnsLog as entry}
                  <tr>
                    <td class="py-1.5 pr-4 text-slate-500 dark:text-slate-400">{formatDateTime(entry.ts)}</td>
                    <td class="py-1.5 pr-4 font-mono text-slate-800 dark:text-slate-100">{entry.recordType}</td>
                    <td class="py-1.5 pr-4 font-mono text-slate-800 dark:text-slate-100">{entry.hostname}</td>
                    <td class="py-1.5 pr-4 font-mono text-slate-800 dark:text-slate-100">{entry.newValue}</td>
                    <td class="py-1.5">
                      {#if entry.result === 'ok'}<span class="badge-green">{t('admin.domains.checkOk')}</span>{:else}<span class="badge-red">{entry.message || t('common.error')}</span>{/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </section>
  </div>
{/if}
