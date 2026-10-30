<script lang="ts">
  import { theme, type ThemeMode } from "@nrynss/chaaya/theme";

  const modes = [
    { id: "light", label: "Light" },
    { id: "dark", label: "Dark" },
    { id: "system", label: "System" },
  ] as const;

  const selected = $derived(theme.mode);

  function choose(mode: ThemeMode): void {
    theme.set(mode);
  }
</script>

<fieldset class="appearance">
  <legend>Appearance</legend>
  {#each modes as mode (mode.id)}
    <label>
      <input
        type="radio"
        name="appearance"
        value={mode.id}
        checked={selected === mode.id}
        onchange={() => choose(mode.id)}
      />
      {mode.label}
    </label>
  {/each}
</fieldset>
