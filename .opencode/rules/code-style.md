# Go code style

- **No blank lines inside functions.**
- **Struct and map literals:** one field per line.
- **Comments:** short, single-line where possible. Explain *why*, not
  *what*. If a comment needs a paragraph, the code probably needs a
  rename or a refactor instead.
- **Errors wrap with `fmt.Errorf("context: %w", err)`.** No bare
  `return err` at API boundaries.
- **No `panic` outside of `init()` or genuine programmer errors.**
  Programmer errors caught at init are preferable to runtime surprises;
  see `formats.Register` for an example.
- **`context.Context` is the first parameter of any function that does
  I/O or spawns goroutines.**
- **Exported symbols have godoc.** Enforced by `revive`'s `exported`
  rule in CI; missing godoc fails the build.

These are not negotiable defaults; if a change deliberately bends one,
the commit message explains why.
