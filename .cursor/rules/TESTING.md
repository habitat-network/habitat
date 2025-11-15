## Writing tests in Go

You are expected to write tests at the level of a seasoned software professional with decades of experience.

- Write tests that verify behavior, not implementation details.
- Aim for test coverage above 70%. Use `make test-coverage` and go test coverage tools to verify
- Minimize the number of lines of testing code, and make sure the purpose of each test is clearl and readable
- To help minimize testing code, reuse setup and teardown functions as much as possible

Some stylistic preferences:

- Never use any sort of patching mechanism.
- Fake implemenations (i.e. simplified working implementations) are vastly preferred over mock implementations
- Use testify.require instead of testify.assert
- Dependency injection is your friend

Recognize the pitfalls of coding agents:

- Do not just disable tests or flip assertions so that a test passes if you get stuck
- Do not just blindly increase coverage. Think about what useful assertions tests can make that test behavior, not implementation.
