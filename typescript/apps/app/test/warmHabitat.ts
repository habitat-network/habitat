// warmHabitat loads the modules createHabitatClient imports lazily (see
// src/server/habitatClient.ts), so a room's first flush doesn't pay for the
// cold import inside a test's 5s budget. Under a parallel run that import is
// the slow part. Call it from beforeAll, whose timeout is separate, after the file's
// vi.mock of ../src/server/oauth, so the fake is what gets loaded.
export async function warmHabitat(): Promise<void> {
  await Promise.all([
    import("../src/server/effect/services"),
    import("../src/server/oauth"),
  ]);
}
