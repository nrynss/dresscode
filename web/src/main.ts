import { mount } from "svelte";
import App from "./app/app.svelte";
import { mockEnabled } from "./lib/api/mode";

const target = document.getElementById("app");
if (!target) {
  throw new Error("missing #app");
}

if (mockEnabled()) {
  const { installMock } = await import("./lib/api/mock");
  installMock();
}

mount(App, { target });
