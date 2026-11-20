import { PearApi } from "./api";
import { startPear } from "./pear";
import { seed } from "./seed";
import { seedPdsAccounts } from "./pds";

/**
 * Starts pear and seeds it once for the whole run. The details are handed to
 * workers through environment variables (workers are spawned afterwards and
 * inherit them); see harness/fixtures.ts for where they are read.
 */
export default async function globalSetup() {
  const pear = await startPear();
  try {
    const seeded = await seed(new PearApi(pear.address));
    process.env.E2E_SEED = JSON.stringify(seeded);
    if (pear.pdsAddress)
      process.env.E2E_PDS_ACCOUNTS = JSON.stringify(
        await seedPdsAccounts(pear.pdsAddress),
      );
  } catch (err) {
    await pear.stop();
    throw err;
  }
  process.env.E2E_PEAR_ADDRESS = pear.address;
  process.env.E2E_PEAR_TLS_ADDRESS = pear.tlsAddress;
  if (pear.app) process.env.E2E_APP = "1";
  return () => pear.stop();
}
