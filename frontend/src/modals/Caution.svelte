<script>
  // A caution inside a pane or a dialog: one line in the warning ink behind an
  // alert icon, and the reasoning, if there is any, one gesture away behind the
  // ⓘ. Cautions used to be tinted paragraphs (one of them in the danger red,
  // which is for things that destroy something); a settings page that shouts in
  // three colours has nothing left to say when something is actually wrong.
  import Icon from "../Icon.svelte";
  import InfoDot from "./InfoDot.svelte";

  let { text, detail = "", icon = "alert", class: cls = "" } = $props();
</script>

<p class="warn-line {cls}">
  <Icon name={icon} size={13} />
  <span>{text}{#if detail}<InfoDot text={detail} label="More about this" />{/if}</span>
</p>
