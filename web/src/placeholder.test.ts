import { packageName } from "@nrynss/chaaya";
import { expect, test } from "vitest";

test("placeholder route uses the chaaya root export", () => {
  expect(packageName).toBe("@nrynss/chaaya");
});
