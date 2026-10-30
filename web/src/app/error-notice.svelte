<script lang="ts">
  import { humanMessage } from "../lib/api/errors";
  import type { ShownError } from "../lib/api/types";

  let { error }: { error: ShownError } = $props();
  const text = $derived(humanMessage(error));
</script>

<p class="notice" role="alert">{text}</p>
