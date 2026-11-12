# too-broke-to-die

TOO BROKE TO DIE is planned as a complete, offline, single-player Go/raylib desktop game for Linux x86-64 and Windows x86-64, with Russian player-facing text. The attached `too-broke-to-die-pdd-linux.md` is the requirements authority; these records summarize it, not replace it. Implementation from `main` has been authorized, but no game implementation or qualification is established by this baseline. A vertical slice is not the finished game.

- [Requirements](docs/requirements.md): full-game scope and PDD traceability.
- [Phases](docs/phases.md): ordered dependencies, exits, and progress.
- [Production dependencies](docs/production-dependencies.md): final media, rights, and open risks.
- [Verification](docs/verification.md): evidence statuses, scenarios, and qualification boundaries.

This documentation-only bootstrap uses `git diff --check`; the dependent source-gates change will introduce the full display-free Linux `scripts/check.sh` gate. Manual desktop and device qualification does not gate implementation changes. Publication, release, signing, storefront submission, and spending on external providers are outside this task.

## Source headers

Each new Go file must begin with the repository-defined header:

```go
// Copyright (C) 2026 crawter
// All rights reserved.
```
