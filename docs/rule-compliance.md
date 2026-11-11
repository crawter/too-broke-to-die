# Rules, traceability, and evidence baseline

## Available authority

The current task supplies the complete text of the Engineering Architecture
Guide, Go Coding Style Guide, and Go Wiring & Lifecycle Guide as project rules
to this run. They apply to every implementation step. The earlier conversation
also states that the Go Coding Style Guide was supplied; its complete text is
accessible here in the supplied task rules, although no standalone guide file
exists in the inspected repository. The plan's earlier assumption that the full
guide was inaccessible does not describe this run's supplied rules.

The selected text for every non-generated Go file's copyright header is exactly:

```text
Copyright (c) TOO BROKE TO DIE contributors
```

Apply it as a Go comment, respecting build constraints and generated-code
notices. Generated files follow their generator's required notice and record
their source. Header selection is not evidence that any source has been
checked.

The conversation refers to project pull-request rules, but no separate PR rule
text or repository rule file was accessible in the inspected tree or supplied
task rules. The available workflow uses a base branch and pull requests; do not
invent additional branch protection, approval counts, or review requirements.
Apply any actual PR rules when made available. Record implementing and
reviewing owners separately for PDD phase evidence.

The PDD and current task govern game-specific requirements and authorized
scope. The PDD's description of its own document-only authorization is
historical: this task authorizes implementation, not commissioning,
purchasing, signing, deployment, publication, or release.

## Applicable engineering rules

### Architecture

- Keep domain behavior independent of entrypoints, raylib, Windows, and
  persistence implementations. Use one desktop composition root; the
  entrypoint handles process concerns, not campaign rules.
- Use responsibility-based packages and explicit exported contracts. Do not
  create catch-all packages or in-process interfaces solely for tests.
  Storage, filesystem, and OS boundaries justify narrow contracts.
- Input and presentation translate calls and results; session coordinates
  state, campaign, simulation, and persistence. Validate business rules before
  mutation. Publish immutable frame views and only committed durable progress.
- Keep content data declarative, versioned, validated, and cross-referenced.
  Russian text and asset filenames do not define runtime credit rules.
- Inject external dependencies; avoid hidden clients, configuration reads,
  service locators, and unmanaged goroutines. Use identity-based error mapping,
  safe player messages, and meaningful boundary logging.

### Go style

- Use the module's declared Go version, `gofmt`, grouped imports, clear names,
  intentional exports, and the selected header. Preserve fixed wire and save
  identities; check bounds, precision, and overflow. Money uses integer cents.
- Pass contexts and defer monkit tasks for nontrivial operations as required by
  the guide, including its fixed-signature, test, and hot-loop exceptions.
  Do not pass a captured context's address to instrumentation in a closure.
- Preserve error identities and causes; use the supplied error conventions and
  combine consequential cleanup errors. Do not branch on error text.
- Give goroutines owners, termination conditions, and completion policies.
  Synchronize shared state consistently; do not use sleeps as synchronization.
- Add relevant success, failure, and boundary tests. Use real protocol and
  persistence boundaries where the guide requires them, and race checks for
  concurrent code where supported.

### Wiring and lifecycle

- Constructors validate configuration, receive required dependencies, register
  cleanup immediately, and do not start long-running work. Construction failure
  closes acquired resources and combines errors.
- The initial OS thread owns all application raylib calls and native cleanup.
  Each resource has one owner; borrowed resources are not closed by consumers.
- `Run` supervises work and reports unexpected exits, including early nil exits.
  `Close` is safe after partial initialization and repeated requests. Stop
  accepting work, drain accepted writes, join workers, then close their
  dependencies and flush diagnostics.
- Shutdown uses a fresh bounded context and the PDD's independently enforced
  ten-second budget. Test cancellation, active writes, forced stop, error
  propagation, and exactly-once cleanup.

These are applicable rules, not a finding that future source complies. The
PDD's FGA-E01–E10 and the supplied guides remain the detailed review authority.

## Requirement traceability

The table is an index into the PDD, not a replacement for its individual
requirements or scenario parameter sets. Row numbers refer to the ordered changes
in `implementation-plan.md`. All inclusive requirement ranges below cover the
complete PDD §13 list, 1–96.

| PDD §13 requirements | Implementation coverage | Evidence authority |
|---|---|---|
| 1–3: offline target, authorization, Russian | 1–4, 32–38, 65, 77–80 | PDD §§1, 10, 12; FGA-B01–B08 and Russian review |
| 4–15: story, institutions, tone, identities | 43–64 | PDD §§2–6, 8–9; CAM-T01, FGA-N01–N07, PRE-T01–T02 |
| 16–21: controls, movement, combat, limbs, feces, interactions | 18–24, 32, 40–42 | SYS-T01–T05; FGA-G01–G07, G10 |
| 22–29: certification, consequences, states, recovery, ending eligibility | 7–16, 27–31, 39, 60–64, 70 | SYS-T06–T14; FGA-C03–C08, FGA-R01–R11 |
| 30–40: four acts, bosses, 100 sources, recoverability, ending gates | 25–28, 44–64, 72 | PDD §8; CAM-T01–T17; FGA-C01–C08, G08–G09, N04–N06 |
| 41–48: visual identity, animation, HUD, menus, recovery, audio | 33–42, 66–68, 73 | PDD §9; PRE-T01–T13, FGA-V01–V04, V06 |
| 49–57: Russian, ending staging, accessibility, assets and handoff | 49, 53, 57, 61, 63–69, 73, 80 | PRE-T11–T18; FGA-N05–N07, V05–V08; specialist reviews |
| 58–62: runtime, toolchain, reproducibility, package, provenance | 2–4, 66–69, 77–80 | FGA-B01–B08, V01, V08; locked-input and rights records |
| 63–70: architecture, contracts, content, ticks, collision, encounters | 4–10, 18–30, 71 | FGA-E01–E05; SYS-T01–T05; FGA-G01–G09 |
| 71–76: schema, profile, atomicity, idempotency, recovery | 7–16, 30–31, 60, 62, 70 | SYS-T06–T14; FGA-R01–R11 |
| 77–82: native ownership, shutdown, failures, diagnostics, honest status | 11, 14, 17, 33–37, 74, 82 | FGA-E06–E10, FGA-B06–B07; documented unrun evidence |
| 83–88: complete phases and story, mechanics, credit, persistence, presentation qualification | 1–2, 44–75 | PDD §§11–12; FGA-N, G, C, R, V matrices and phase records |
| 89–93: distribution, performance, architecture, evidence, three campaigns | 74–81 | FGA-B01–B08, P01–P05, E01–E10; candidate records and three fresh-profile runs |
| 94–96: risks, truthful readiness, separate release | 1–2, 69, 80–82 | PDD §§11.4, 12.1, 12.11; risk dispositions and readiness record |

For integrated mapping of each SYS-T, CAM-T, and PRE-T scenario to FGA cases
and execution/review owners, use PDD §12.2. The qualification index must retain
each individual requirement and scenario, not merely the ranges above.

## Evidence and acceptance

A result is recorded per scenario **and parameter set** with requirement
references, source revision, executable hash, content digest, environment
including Windows, hardware and drivers, setup, devices, settings, fixture
identity where applicable, procedure, expected and observed results, evidence
files, status, defect references, executor, reviewer, and execution date.
Changes invalidate affected results; do not carry a pass to a different
candidate or parameter set without applicable new evidence.

Full acceptance requires all mandatory SYS-T01–T14, CAM-T01–T17,
PRE-T01–T18, and FGA-N01–N07, G01–G10, C01–C08, R01–R11, V01–V08,
B01–B08, P01–P05, and E01–E10 parameter sets, plus every §13 requirement and
specialist review. A receipt-set audit must include the complete sorted set,
not only a screenshot of `100/100`.

Three real packaged-game campaigns must start from fresh profiles and reach
ST-END without fixture shortcuts: one with keyboard and mouse, one with a
controller, and one including failures, catastrophes, interruptions, revisits,
and high debt. Automated simulation or an isolated fixture is not one of
these runs. The final candidate also needs all final media and rights records,
reference-machine performance, compatibility, lifecycle, and clean-package
evidence on the same candidate, with no unresolved blocker or major defect.
Remaining minor defects must not violate an acceptance condition.

At this baseline:

| Evidence | Status and reason |
|---|---|
| Source implementation, source checks, and automated scenarios | **Not run**; no implementation or project check commands exist in the inspected tree. |
| Dependency intake and generated locks | **Not run**; proposed pins and archive identities are unverified. |
| Representative and complete final media, permissions, and originality review | **Blocked**; exports and rights evidence are not supplied. |
| Native Windows, hardware, controller, audio, clean-machine, and power-loss checks | **Blocked**; implementation and qualified environments are unconfirmed. |
| Fresh-profile campaigns, native Russian editorial review, and final visual/audio reviews | **Blocked**; candidate, final content, and necessary people or environments are unconfirmed. |
| Candidate readiness and release | **Blocked**; mandatory evidence is missing. Release is separately authorized work. |

A blocked prerequisite may later be resolved; the corresponding scenario must
then be executed and reviewed. A definition, planned command, or future
mitigation is not a result.

## Documentation and lane checks

Before accepting either baseline document, the lane must check:

1. PH-00 through PH-09 each retain their PDD dependencies, owners, exit
   criteria, risks, and evidence; PH-02 is explicitly intermediate and retains
   its representative final-media and integrated-mechanics requirements.
2. The 82 change names appear once, in the agreed order, each with an outcome,
   phase, immediate predecessor, and acceptance evidence. Verify the chain has
   no branch join or missing predecessor.
3. Every PDD §13 requirement maps to implementation and evidence; cross-check
   the individual SYS, CAM, PRE, FGA, and audit identifiers against PDD §12.
   Check the 96 tickets, four closures, `D-INS-END` as credit 100, and
   canonical ending against PDD §8.
4. Every status distinguishes actual observation from planned work. Missing
   media and environments remain blocked; unexecuted checks remain not run.
   Verify candidate identity, parameter coverage, and invalidation rules.
5. Check the selected header and all accessible guide rules; identify any
   inaccessible rule source without implying it was reviewed.

The repository currently supplies no runnable project checks. As facilities
are introduced, the lane must run available documentation, content,
architecture, dependency, formatting, compilation, and focused test checks.
PDD §12.10 names `gofmt -l`, `go vet ./...`, `go test ./...`, and
`go test -race ./...` as proposed checks; their applicability depends on the
created module and supported toolchain. Run native Windows cgo integration,
build, import-audit, reproducibility, package, and physical qualification
checks on their required environments. Record exact commands, inputs, outcomes,
and limitations. An unsupported race or target check is not a pass, and
portable checks do not replace native or product qualification.
