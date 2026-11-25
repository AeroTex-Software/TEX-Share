<script lang="ts">
  import { t } from '../../lib/i18n';
  import { navigate } from '../../lib/router';
  import EmptyState from '../../components/EmptyState.svelte';
  import Icon from '../../components/Icon.svelte';
</script>

<div class="card">
  <EmptyState icon="help" title={t('err.not_found')} message={t('share.notFound')}>
    <button type="button" class="btn-primary btn-sm mt-1" on:click={() => navigate('/')}>
      <Icon name="arrowLeft" size={15} />{t('nav.dashboard')}
    </button>
  </EmptyState>
</div>
