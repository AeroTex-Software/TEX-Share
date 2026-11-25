<script lang="ts">
  import { publicMeta } from '../../lib/stores';
  import ProfilePanel from '../../components/ProfilePanel.svelte';
</script>

<ProfilePanel allow2fa={$publicMeta?.allow2fa !== false} />
