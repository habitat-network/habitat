---
paths: ["**/*.go"]
---

# Go rules and conventions

## Project layout and architecture
- Go version: 1.26
- Standard Go project layout: All entrypoints live in cmd/, everything else is in internal/ with a package per-component and some shared utility packages.

## Go conventions
Follow these conventions when writing Go code.
When in doubt, follow the conventions outlined here: https://google.github.io/styleguide/go/best-practices and here https://google.github.io/styleguide/go/decisions. 

- For setup and initialization:
  - Expose constructors, not a global / singleton.
  - Take dependencies as interface arguments rather than config arguments.
  - Parse env/flags in main() and almost nowhere else. flag is usually better than environment variable. Some values can either be passed as a flag or an env var, refer to getSources() in cmd/pear/flags.go.
  - Never use dependency injection.
  - Switch behavior with explicit config, avoid if prod-like checks
```
// Good
// dev
--loglevel=debug

// prod
--loglevel=info

// Bad
if env==dev {
  setLogLevel('debug')
} else if env==prod {
  setLogLevel('info')
}
```

- For typing:
  - Use typedefs instead of the raw type
```
// Bad
func CreateOrg(id string) { ... }

// Good
type OrgID string
func CreateOrg(id OrgID) { ... }
```
  - Use an interface with an unexported method to make discriminated. If a type is one of several disjoint types, you can use an interface type with an unexported method:
```
type Thing interface {
  isThing()
}
```

- For errors:
  - Use `errors.New` and `errors.Is` to define and switch on errors that are reused within package and read by callers naming the error specifically like ErrMalformattedString. 
  - Use fmt.Errorf for one-oﬀs, or to add human readability, typically at the level before errors are returned over-network or an interface.
  - Handle or return errors, not both. e.g. don't log an error and return it.
  - Return an error or a usable value, not both.

- For larger feature implementations, anytime you are dealing with an API or interface, use test-driven development with the go-tdd skill.

- Library preferences:
  - For simple manipulation on slices and maps, if no helper is provided by the `slices` and `maps` libraries, then use `xmaps` and `xslices` from the `github.com/bradenaw/juniper` package.
  - When using the `context` package: Do use context cancellation for cancellation signals. Never store any values in the context (context.WithValue). Call contexts `ctx`