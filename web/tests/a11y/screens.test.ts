import { test } from "vitest";
import { preferFixture } from "../../src/lib/api/mock";
import { resetHistory } from "../../src/routes/history/flow";
import {
  gate,
  mountApp,
  openAboutDialog,
  openDeleteDialog,
  openFixMenu,
  openSecondVisit,
  openShopSheet,
  useMock,
  waitUntil,
  withText,
  type Mode,
} from "./helpers";

const modes: Mode[] = ["light", "dark"];

interface ScreenState {
  name: string;
  hash: string;
  before?: () => void;
  ready?: () => Element | null;
  open?: () => Promise<void>;
}

/** Every screen the app serves, plus the dialog and sheet states a reader
 * reaches from them. Each state runs the gate in both forced themes at the
 * phone width. */
const states: ScreenState[] = [
  {
    name: "the home screen with the session read",
    hash: "",
    ready: () => withText("p", "You are here as a guest."),
  },
  {
    name: "the home screen with the about dialog open",
    hash: "",
    ready: () => withText("p", "You are here as a guest."),
    open: openAboutDialog,
  },
  {
    name: "the occasion screen with the fix menu open",
    hash: "#/occasion",
    ready: () => withText("label", "Family function"),
    open: openFixMenu,
  },
  {
    name: "the capture screen",
    hash: "#/capture",
    ready: () => withText("h2", "Your selfie"),
  },
  {
    name: "the verdict screen with the recorded look finished",
    hash: "#/look/verdict",
    ready: () => withText("h3", "A fix"),
  },
  {
    name: "the render screen with the swipe and the verdict buttons",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
  },
  {
    name: "the render screen with the shop sheet open",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
    open: openShopSheet,
  },
  {
    name: "the history screen with the switch off",
    hash: "#/history",
    before: () => resetHistory(),
    ready: () => withText("label", "Style history"),
  },
  {
    name: "the history screen with the second visit shown",
    hash: "#/history",
    before: () => {
      resetHistory();
      preferFixture("GET", "/api/history/recall?occasion=family-function", "recall.json");
    },
    ready: () => withText("label", "Style history"),
    open: openSecondVisit,
  },
  {
    name: "the account screen with the privacy text",
    hash: "#account",
    ready: () => withText("h2", "Who sees what"),
  },
  {
    name: "the account screen with the history dialog open",
    hash: "#account",
    ready: () => withText("h2", "Who sees what"),
    open: () => openDeleteDialog("Delete style history"),
  },
];

for (const state of states) {
  for (const mode of modes) {
    test(`${state.name} passes the accessibility gate, forced ${mode}, 375 px`, { timeout: 30000 }, async () => {
      useMock();
      state.before?.();
      const leave = await mountApp(mode, state.hash);
      try {
        if (state.ready !== undefined) await waitUntil(state.ready, `${state.name} is ready`);
        if (state.open !== undefined) await state.open();
        await gate(mode);
      } finally {
        await leave();
      }
    });
  }
}
