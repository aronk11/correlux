# Architecture Decision Records

Why kubeui is built the way it is. See [ADR 1](0001-record-architecture-decisions.md)
for the process; the format is Michael Nygard's: context, decision, consequences.

| # | Decision | Status |
|---|----------|--------|
| [1](0001-record-architecture-decisions.md) | Record architecture decisions | accepted |
| [2](0002-go-and-bubble-tea.md) | Go and Bubble Tea v2 for a single static binary | accepted |
| [3](0003-application-first-navigation.md) | Applications, not resource types, are the primary navigation model | accepted |
| [4](0004-layered-architecture.md) | The domain layer must not know that a terminal exists | accepted |
| [5](0005-explicit-async-state.md) | Remote data carries an explicit lifecycle and a generation counter | accepted |
| [6](0006-lazy-scoped-loading.md) | No global cache at startup: load lazily, within the active scope | accepted |
| [7](0007-session-local-context-switching.md) | Switching context inside kubeui never writes to the kubeconfig | accepted |
| [8](0008-production-safety.md) | Production contexts are classified heuristically and guarded by default | accepted |
| [9](0009-accessibility-and-terminal-capabilities.md) | Never encode meaning in colour alone; degrade to ASCII | accepted |
| [10](0010-deterministic-diagnosis-before-ai.md) | The diagnosis engine is deterministic; AI is an optional explanation layer | accepted |
| [11](0011-development-workflow.md) | Trunk-based development with feature branches and Conventional Commits | accepted |
| [12](0012-task-instead-of-make.md) | Task instead of Make | accepted |
| [13](0013-server-side-tables.md) | Resources are rendered from the API server's own tables | accepted |
| [14](0014-load-testing-with-kind.md) | Load is tested on a real API server, with pods that never run | accepted |
| [15](0015-signed-commits.md) | Every commit is signed | accepted |
| [16](0016-application-inference.md) | Applications are inferred from the cluster, not declared to kubeui | accepted |
| [17](0017-timed-refresh-not-watches.md) | The screen reloads on a timer the user turns on, not on watches | accepted |

## Adding one

Copy the structure of an existing record, take the next number, and open it with
your pull request. Never edit an accepted ADR to change its decision: write a new
one that supersedes it, and mark the old one `superseded by ADR N`.
