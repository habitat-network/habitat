---
paths: ["**/*_test.go", "**/*_tests.go"]
---

## Testing Conventions
- Use `testing` library, avoid `testify/suite` and other libraries.
- Use `require` library for most assertions. Only use `assert` in non-critical assertions where test run should continue to give user additional context on the failure.
- Test internal functions by placing test files in the same package
- For test setup and teardown, write a single setup() function that returns a teardown() function:
```
  // Example
  func setupTest(t *testing.T, db *gorm.DB) func() {
    // Do setup
    db.Create(...)

    return func() {
      db.Drop(...)
    }
  }
```
- Do interface-based fakes rather than mocks
  - Fakes implement the contracts of an interface typically with some in-memory component rather than spinning up all the dependencies. Mocks are controlled by the test and behave as told and less useful.
- Test behavior, not implementation. Typically, you should write one end-to-end test for a given API flow or interface, and a few unit tests testing basic behavior and negative / error pathways. Avoid repetition across tests.
- Never use `time.Sleep()`, use `synctest.Test` when dealing with multiple go-routines.
- Test names should be like: TestComponentWhatItTests()
