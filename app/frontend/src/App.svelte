<script lang="ts">
  import DevApp from './DevApp.svelte'
  import UserApp from './UserApp.svelte'

  const appMode = import.meta.env.VITE_NV_X_APP_MODE === 'dev' ? 'dev' : 'user'
</script>

{#if appMode === 'dev'}
  <DevApp />
{:else}
  <UserApp />
{/if}
