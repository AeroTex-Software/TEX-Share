<script lang="ts">
  // Official TEX Share brand mark. The bundled icon is always used unless a
  // custom logo has been uploaded by an administrator, in which case `src`
  // points at that upload. Only the height is fixed so any uploaded wordmark
  // keeps its own aspect ratio instead of being stretched.
  export let src = '';
  export let size = 32;
  export let maxWidth = 0;
  export let alt = 'TEX Share Logo';
  export let extra = '';

  const bundled = '/assets/tex-share-icon.png';
  $: source = src || bundled;
  $: style = `height:${size}px;width:auto;` + (maxWidth > 0 ? `max-width:${maxWidth}px;` : '');
</script>

<img src={source} {alt} {style} decoding="async" class="shrink-0 object-contain {extra}" />
