# AI instructions

CameraCoordinator is a Go library that detects if a web cam is turned on or off on Linux.

## Documentation

Read documentations depending on the tasks

- **Architecture docs** (for when making architectural or bigger changes): `docs/development/architecture.md`

## Top-level rules

- Use notes/CURRENT_TASKS.md to track and update the current, short-term tasks.
- Use notes/PLAN.md for understanding the long-term plan and tasks.

## Coding policy

### General policy

- Use `slog` package to log.

### Test policy

- Add sufficient test coverage for code changes. Think of all the possible edge cases and comment inline in the tests on why these cases matter.
- Code coverage should be 100% (but some error paths might be near-impossible to test, so they can be skipped).
- Test failures should be accompanied with good error messages for debugging.
- Follow test commands exactly as stated above where possible.
