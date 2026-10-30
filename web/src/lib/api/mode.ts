/** True when the shell should answer from testdata/api instead of the network.
 * Dev and tests do. A production build does not, unless it is told to.
 * `VITE_DRESSCODE_API=live` forces the network. `mock` forces the fixtures. */
export function mockEnabled(): boolean {
  const flag = import.meta.env.VITE_DRESSCODE_API;
  if (flag === "live") return false;
  if (flag === "mock") return true;
  return import.meta.env.DEV;
}
