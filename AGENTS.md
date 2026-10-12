# AGENTS.md

### Data and files

- MUST NOT edit generated files (see Generated Code section below).

### Secrets

- MUST NOT print, log, commit, or hardcode secrets, API keys, tokens, or credentials.
- MUST NOT display contents of sensitive files (`.env`, `*.pem`, `*.key`). Redact if referencing.

---

## Task Runner

All commands use the `taskfile` script. To see available commands:

See the full list of available tasks by running `task -l`, or, see the [Taskfile.yml](./Taskfile.yml) script.

```sh
# Build the tool into bin/land-commits
task build
# Run all static checks
task check
# Auto-fix static checks
task fix
# Run all the tests
task test

# Run the quick tests
task test -- -short ./...
# Run the tests for one package
task test -- ./acceptance/...

# Format all the code
task fmt
# Tidy go.mod
task mod-tidy
```

## Code style

### Where does go code live?

| What          | Where                    |
|---------------|--------------------------|
| APIs          | internal/api/...         |
| Clients       | internal/client/...      |
| Model / Store | internal/xxx.Store       |
| Tests helpers | internal/testing/...     |
| Fakes         | internal/testing/fakexxx |

### Rules

- Use `internal/httpcl` for all HTTP requests, including in tests.
- Helpers go at the bottom of the file, not the top.
- Watch out for stuttering in naming.
- Prefer `package_test` external tests for test files.
- Use common fakes in all tests for external dependencies (e.g. GitHub).
- Avoid raw go channels and `sync.WaitGroup` where possible, preferring x/sync/errgroup.
- Use x/sync/errgroup `SetLimit` to limit parallelism in e.g. HTTP requests.

## Test Assertions

Use `gotest.tools/v3/assert` for test assertions:

- **Prefer `assert.Check`** to keep the test running and collect as many
  failures as possible in a single run
- **Use `assert.Assert` / `assert.NilError` as gates** — only when failure means
  the remaining assertions are pointless or unsafe (e.g. a nil pointer would
  panic, or a missing resource means nothing else can be verified)
- Group test stages into logical blocks using `t.Run()`.
- **Use `cmp` comparisons** from `gotest.tools/v3/assert/cmp` for semantic
  matchers over raw boolean expressions

`assert.Assert` and `assert.Check` both accept three kinds of argument: a `bool`
expression, a `cmp.Comparison`, or an `error`.

## Assert vs Check

`assert.Check` calls `t.Fail` and returns `false`, allowing the test to continue
collecting failures — **prefer it by default**. `assert.Assert` calls
`t.FailNow` and stops immediately — use it only as a gate.

The canonical pattern: use `assert.NilError` (or `assert.Assert`) to gate on
preconditions, then use `assert.Check` for everything else:

```go
result, err := doSomething()
assert.NilError(t, err)                              // gate: no point checking result if err != nil
assert.Check(t, result.OK)
assert.Check(t, cmp.Equal(result.Status, "ready"))
assert.Check(t, cmp.Len(result.Items, 3))
assert.Check(t, cmp.Contains(result.Name, "prefix"))
```

**Named functions are all fatal.** `assert.Equal`, `assert.DeepEqual`,
`assert.Error`, `assert.ErrorContains`, and `assert.ErrorIs` all call
`t.FailNow`. To get the non-fatal equivalent, use `assert.Check` with the
corresponding `cmp` comparison:

| Fatal (gate only)                     | Non-fatal equivalent                             |
| ------------------------------------- | ------------------------------------------------ |
| `assert.Equal(t, a, b)`               | `assert.Check(t, cmp.Equal(a, b))`               |
| `assert.DeepEqual(t, a, b)`           | `assert.Check(t, cmp.DeepEqual(a, b))`           |
| `assert.Error(t, err, "msg")`         | `assert.Check(t, cmp.Error(err, "msg"))`         |
| `assert.ErrorContains(t, err, "sub")` | `assert.Check(t, cmp.ErrorContains(err, "sub"))` |
| `assert.ErrorIs(t, err, target)`      | `assert.Check(t, cmp.ErrorIs(err, target))`      |
| `assert.Assert(t, <bool or cmp>)`     | `assert.Check(t, <bool or cmp>)`                 |

Note: `assert.Assert` must be called from the goroutine running the test
function. `assert.Check` is safe to call from any goroutine.

### Message argument

`assert.Check` (and `assert.Assert`) accept a trailing
`msgAndArgs ...interface{}` that is appended to the failure output. Pass a
message when the comparison alone does not make the intent obvious — for
example, when checking a boolean derived from non-obvious logic, when the
variable name is ambiguous, or when the test loops over cases and you need to
identify which iteration failed.

```go
// ❌ Opaque — failure says "false" with no context
assert.Check(t, got.ExpiresAt.Before(deadline))

// ✅ Clear — failure says what the check was verifying
assert.Check(t, got.ExpiresAt.Before(deadline), "token must expire before session deadline")

// ❌ In a loop — impossible to tell which item failed
for _, item := range items {
    assert.Check(t, cmp.Equal(item.State, "ready"))
}

// ✅ In a loop — failure identifies the offending item
for _, item := range items {
    assert.Check(t, cmp.Equal(item.State, "ready"), "item %q", item.ID)
}
```

Skip the message when the comparison is already self-documenting — `cmp.Equal`,
`cmp.DeepEqual`, `cmp.Len`, and `cmp.ErrorIs` all produce structured failure
messages that include the values involved, so they rarely need extra annotation.

### Test Caching

`task test` runs every test with `-count=1`, so nothing is cached. The acceptance tests run a
compiled binary, which `go test`'s cache can't see changing, and the tests are fast enough
not to need caching.

---

### Examples

```go
import (
    "gotest.tools/v3/assert"
    "gotest.tools/v3/assert/cmp"
)

func TestSomething(t *testing.T) {
    // Gate: fail immediately if setup fails — nothing else can run
    err := startContainer(ctx)
    assert.NilError(t, err)

    result, err := doSomething()
    assert.NilError(t, err)  // gate: result is meaningless if err != nil

    // Check everything else — collects all failures in one run
    assert.Check(t, cmp.Equal(result.Status, "ok"))
    assert.Check(t, cmp.Len(result.Items, 3))
    assert.Check(t, result.Ready)
}
```

### Semantic Matchers

Use the most specific assertion for the situation. Prefer named functions over
raw boolean expressions when a named function exists. Fall back to
`assert.Check(t, <bool>)` for comparisons that have no dedicated function — the
expression source code appears verbatim in the failure message, which is good
enough.

### Core functions

| Situation                                  | Preferred form (non-fatal)                         | Gate form (fatal)                       |
| ------------------------------------------ | -------------------------------------------------- | --------------------------------------- |
| `err` must be nil                          | —                                                  | `assert.NilError(t, err)`               |
| Two scalar values must be equal (`==`)     | `assert.Check(t, cmp.Equal(actual, expected))`     | `assert.Equal(t, actual, expected)`     |
| Complex values must be equal (go-cmp diff) | `assert.Check(t, cmp.DeepEqual(actual, expected))` | `assert.DeepEqual(t, actual, expected)` |
| Error must match exact message             | `assert.Check(t, cmp.Error(err, "msg"))`           | `assert.Error(t, err, "msg")`           |
| Error must contain substring               | `assert.Check(t, cmp.ErrorContains(err, "sub"))`   | `assert.ErrorContains(t, err, "sub")`   |
| Error must match sentinel / wrapped error  | `assert.Check(t, cmp.ErrorIs(err, target))`        | `assert.ErrorIs(t, err, target)`        |
| Anything else                              | `assert.Check(t, <bool or cmp>)`                   | `assert.Assert(t, <bool or cmp>)`       |

### Nil and emptiness

There are no dedicated `NotNil` or `NotEmpty` helpers. Use
boolean expressions — the source is included in the failure message:

```go
// Use Assert as a gate when nil would cause a panic below
assert.Assert(t, result != nil)

// Use Check for non-fatal emptiness assertions
assert.Check(t, cmp.Nil(result))
assert.Check(t, len(items) != 0)
```

### Numeric comparisons

Express comparisons directly as boolean expressions:

```go
assert.Check(t, x > 0)
assert.Check(t, a >= b)
assert.Check(t, count < limit)
```

### String contains

To check if a string contains a substring:

```go
result := "this is the haystack"
assert.Check(t, cmp.Contains(result, "needle"))
```

### Length and containment

Use `cmp` comparisons for richer failure messages:

```go
// Length — prints expected vs actual length on failure
assert.Check(t, cmp.Len(items, 3))

// Containment — works for slices, maps, and strings
assert.Check(t, cmp.Contains(slice, item))
assert.Check(t, cmp.Contains(mapping, "key"))
assert.Check(t, cmp.Contains(str, "substr"))
```

### Structured data

Use `assert.Check(t, cmp.DeepEqual(...))` for structs, slices, and maps. It uses
`go-cmp` and produces a clear diff on failure:

```go
assert.Check(t, cmp.DeepEqual(result, myStruct{Name: "title"}))
// assertion failed: ... (diff of the two values)
```

For unordered slice comparison, pass `cmpopts.SortSlices` from
`github.com/google/go-cmp/cmp/cmpopts`:

```go
assert.Check(t, cmp.DeepEqual(actual, expected,
    cmpopts.SortSlices(func(a, b string) bool { return a < b }),
))
```

For JSON, unmarshal first and use `DeepEqual`:

```go
var actual, expected MyType
err := json.Unmarshal(data, &actual)
assert.NilError(t, err)
assert.Check(t, cmp.DeepEqual(actual, expected))
```

### Pattern matchers

```go
assert.Check(t, cmp.Regexp(`^\d{4}-\d{2}-\d{2}$`, dateStr))
```

### Custom comparisons

`assert.Check` (and `assert.Assert`) accept any `cmp.Comparison` — a function
that returns a `cmp.Result`. Use this for domain-specific checks:

```go
withinTolerance := func(got, want, delta float64) cmp.Comparison {
    return func() cmp.Result {
        if math.Abs(got-want) <= delta {
            return cmp.ResultSuccess
        }
        return cmp.ResultFailure(fmt.Sprintf("%v not within %v of %v", got, delta, want))
    }
}

assert.Check(t, withinTolerance(actual, expected, 0.01))
```
