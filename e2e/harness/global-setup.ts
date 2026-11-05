import { PearApi } from "./api";
import { startPear } from "./pear";
import { seed } from "./seed";

/**
 * Starts pear and seeds it once for the whole run. The details are handed to
 * workers through environment variables (workers are spawned afterwards and
 * inherit them); see harness/fixtures.ts for where they are read.
 */
export default async function globalSetup() {
  const pear = await startPear();
  const seeded = await seed(new PearApi(pear.address));
  process.env.E2E_PEAR_ADDRESS = pear.address;
  process.env.E2E_PEAR_TLS_ADDRESS = pear.tlsAddress;
  process.env.E2E_SEED = JSON.stringify(seeded);
  return () => pear.stop();
}
