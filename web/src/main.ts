import { mount } from "svelte";
import Placeholder from "./placeholder.svelte";

const target = document.getElementById("app");
if (!target) {
  throw new Error("missing #app");
}

mount(Placeholder, { target });
