<script lang="ts">
  import { t } from '../lib/i18n';
  import Icon from './Icon.svelte';

  export let status = '';
  export let size: 'sm' | 'md' = 'md';

  const map: Record<string, { tone: string; key: string; icon: string }> = {
    active: { tone: 'badge-green', key: 'shares.status.active', icon: 'check' },
    paused: { tone: 'badge-amber', key: 'shares.status.paused', icon: 'pause' },
    expired: { tone: 'badge-slate', key: 'shares.status.expired', icon: 'clock' },
    deleted: { tone: 'badge-slate', key: 'shares.status.deleted', icon: 'trash' },
    locked: { tone: 'badge-red', key: 'shares.locked', icon: 'lock' },
    suspended: { tone: 'badge-amber', key: 'shares.suspended', icon: 'ban' },
    quarantined: { tone: 'badge-red', key: 'shares.quarantined', icon: 'warning' },
    disabled: { tone: 'badge-slate', key: 'shares.disabled', icon: 'ban' }
  };

  $: info = map[status] || { tone: 'badge-slate', key: '', icon: 'info' };
</script>

<span class="{info.tone} {size === 'sm' ? 'px-2 py-0.5 text-[11px]' : ''}">
  <Icon name={info.icon} size={size === 'sm' ? 11 : 13} />
  {info.key ? t(info.key) : status}
</span>
