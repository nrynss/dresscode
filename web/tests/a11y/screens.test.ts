import { test } from "vitest";
import { preferFixture } from "../../src/lib/api/mock";
import { resetHistory, writeHistory } from "../../src/routes/history/flow";
import { resetOccasionMenu } from "../../src/routes/occasion/menu";
import {
  gate,
  mountApp,
  openAboutDialog,
  openDeleteDialog,
  openEraseDrawer,
  openFixMenu,
  openFidelityExplanation,
  openHistoryNotice,
  openProductDrawer,
  openPrivacyDrawer,
  openReasonsDrawer,
  openSampleDrawer,
  openSecondVisit,
  openStagesDrawer,
  openShopSheet,
  toggleFixPill,
  useMock,
  waitUntil,
  watchWrites,
  withText,
  type Mode,
} from "./helpers";

const modes: Mode[] = ["light", "dark"];

interface ScreenState {
  name: string;
  hash: string;
  before?: () => void | Promise<void>;
  ready?: () => Element | null;
  open?: () => Promise<void>;
}

/** Every screen the app serves, plus the dialog, drawer, sheet and popover
 * states a reader reaches from them. Each state runs the gate in both forced
 * themes at the phone width.
 *
 * A disclosure gets both its closed and its open state: axe prunes a closed
 * `details` subtree whole, so content folded away from a reader who never taps
 * is content the gate would stop judging if the open state were not mounted
 * beside it. */
const states: ScreenState[] = [
  {
    name: "the home screen with the lead",
    hash: "",
    ready: () => withText("p", "See the dress code before they do."),
  },
  {
    name: "the home screen with the about dialog open",
    hash: "",
    ready: () => withText("p", "See the dress code before they do."),
    open: openAboutDialog,
  },
  {
    name: "the occasion screen with the fix menu open",
    hash: "#/occasion",
    ready: () => withText("label", "Family function"),
    open: openFixMenu,
  },
  {
    name: "the front door with the occasion cards and the judge samples loaded",
    hash: "",
    ready: () => withText("label", "Family function"),
    open: async () => {
      // The drawer hides the judge buttons until asked; open it so the gate
      // judges the buttons a reader who wants a sample sees, then the fix
      // menu beside them.
      await openSampleDrawer();
      await waitUntil(() => document.querySelector(".sample-row button"), "the sample buttons");
      await openFixMenu();
      // The tick auto-saves, and the decluttered screen draws no confirmation:
      // the write itself is the proof, on the wire.
      const writes = watchWrites();
      toggleFixPill("Hair tied back");
      await waitUntil(
        () =>
          writes.writes.find(
            (call) =>
              call.method === "PUT" &&
              call.path === "/api/occasions/family-function/opt-in" &&
              JSON.stringify(call.body) === JSON.stringify({ ticked: ["family-tie-hair"] }),
          ) ?? null,
        "the auto-save write",
      );
      writes.stop();
      await new Promise((resolve) => setTimeout(resolve, 50));
      resetOccasionMenu();
    },
  },
  {
    name: "the capture screen",
    hash: "#/capture",
    ready: () => withText("button", "Open the front camera"),
  },
  {
    name: "the capture screen with the product drawer open",
    hash: "#/capture",
    ready: () => withText("button", "Open the front camera"),
    open: openProductDrawer,
  },
  {
    name: "the verdict screen with the recorded look finished",
    hash: "#/look/verdict",
    ready: () => withText("h3", "A fix"),
  },
  {
    name: "the verdict screen with the finished stepper drawer open",
    hash: "#/look/verdict",
    ready: () => withText("h3", "A fix"),
    open: openStagesDrawer,
  },
  {
    name: "the verdict screen with the missed-criteria drawer open",
    hash: "#/look/verdict",
    ready: () => withText("h3", "A fix"),
    open: openReasonsDrawer,
  },
  {
    name: "the render screen with the swipe and the verdict buttons",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
  },
  {
    name: "the render screen with the missed-criteria drawer open",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
    open: openReasonsDrawer,
  },
  {
    name: "the render screen with the shop sheet open",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
    open: openShopSheet,
  },
  {
    name: "the render screen with a fidelity explanation open in the shop sheet",
    hash: "#/look/render",
    ready: () => withText("button", "Shop the look"),
    open: async () => {
      await openShopSheet();
      await openFidelityExplanation();
    },
  },
  {
    name: "the history screen with the switch off",
    hash: "#/history",
    before: () => resetHistory(),
    ready: () => withText("label", "Style history"),
  },
  {
    name: "the history screen with the notice disclosure open",
    hash: "#/history",
    before: () => resetHistory(),
    ready: () => withText("label", "Style history"),
    open: openHistoryNotice,
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
    name: "the occasion menu with the past-visit line above the fixes",
    hash: "#/occasion",
    before: async () => {
      resetHistory();
      preferFixture("GET", "/api/history/recall?occasion=family-function", "recall.json");
      await writeHistory(true);
    },
    ready: () => withText("label", "Family function"),
    open: async () => {
      await openFixMenu();
      await waitUntil(
        () => withText("p", "Last family function you kept the gold jhumkas and dismissed the red lip."),
        "the past-visit line",
      );
    },
  },
  {
    name: "the account screen with the disclosures closed",
    hash: "#account",
    ready: () => withText("h2", "Who sees what"),
  },
  {
    name: "the account screen with the who-sees-what disclosure open",
    hash: "#account",
    ready: () => withText("h2", "Who sees what"),
    open: openPrivacyDrawer,
  },
  {
    name: "the account screen with the erasure disclosure open",
    hash: "#account",
    ready: () => withText("h2", "Who sees what"),
    open: openEraseDrawer,
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
      await state.before?.();
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
