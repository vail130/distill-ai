# Vendor-frame catalogue

The collapse stage (`internal/event.Collapse`) treats third-party,
runtime, and framework stack frames as candidates for collapse when
the user has not set `--keep-vendor`. The patterns below are the
default catalogue (`event.DefaultPatterns`). They apply to every
format; format authors do not maintain their own vendor-detection
tables.

Adding a language? Append patterns to `event.DefaultPatterns` and ship
the addition together with a unit test in `collapse_test.go`
demonstrating a true-positive (a path that should match) and a true-
negative (a user-code path that should not match). The alignment rule
treats the catalogue as documentation: any new pattern updates this
page in the same commit.

## Python

| Pattern label             | Matches against | What it catches                                |
|---------------------------|-----------------|------------------------------------------------|
| Python site-packages      | `File`          | `site-packages/` and `dist-packages/` directories: pip-installed packages. |
| Python stdlib             | `File`          | `/usr/lib/python<N>/`, `/usr/local/lib/python<N>/`, `/opt/.../lib/python<N>/`: distribution-shipped Python stdlib. |
| Python frozen importlib   | `File`          | `<frozen importlib._bootstrap>` and similar frozen module markers in traceback output. |

## Node / JavaScript

| Pattern label  | Matches against | What it catches                                                |
|----------------|-----------------|----------------------------------------------------------------|
| Node modules   | `File`          | Anything under `node_modules/`, including nested `node_modules/` from `pnpm` and `yarn` workspaces. |

## Go

| Pattern label    | Matches against | What it catches                                                  |
|------------------|-----------------|------------------------------------------------------------------|
| Go runtime       | `File`          | `/src/runtime/` paths (`runtime/proc.go`, `runtime/panic.go`).   |
| Go vendor        | `File`          | `/vendor/` directories produced by `go mod vendor`.              |
| Go module cache  | `File`          | `pkg/mod/` paths from the module download cache.                 |

## JVM

The JVM patterns match on `StackFrame.Function` (the fully-qualified
symbol), not the source file, because JVM stack traces typically
identify frames by their package-qualified method name rather than
their `.java` path.

| Pattern label       | Matches against | What it catches                                              |
|---------------------|-----------------|--------------------------------------------------------------|
| JVM runtime         | `Function`      | Functions starting with `java.`, `javax.`, `sun.`, or `jdk.` |
| JVM test framework  | `Function`      | Functions starting with `org.junit.`, `org.gradle.`, or `org.testng.` |

## What is *not* a vendor pattern

These deliberately do not match:

- A user-code path with `vendor` as a segment name (e.g.
  `app/vendor_integration.go`). The pattern anchors to a path
  separator: `/vendor/`.
- A user package called `node_modules_helper` outside any
  `node_modules/` directory.
- A JVM application class named `javaland.MyApp` — the pattern
  requires the prefix to be followed by `.`.
- A Python file in a project directory called `site-packages/`. The
  pattern is path-anchored: `(?:^|/)site-packages/`.

## Why the collapse stage is the single source of truth

Format parsers (M9–M12) populate `StackFrame.File` and
`StackFrame.Function` but do **not** set `StackFrame.Vendor`. The
collapse stage's `event.ClassifyFrames` overwrites `Vendor` on every
event, so adding a new vendor pattern automatically benefits every
format — and so a stale pattern in one format never silently
diverges from the catalogue here.
