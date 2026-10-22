# Quickstart: Validating the lifecycle Module

This guide proves the feature works end to end. It only says how to check it. The expected
behavior is defined in:

- [contracts/public-api.md](contracts/public-api.md);
- [contracts/lifecycle-semantics.md](contracts/lifecycle-semantics.md);
- [contracts/health-http.md](contracts/health-http.md).

## Prerequisites

- Go 1.26 or newer, and a C toolchain for `-race`.
- golangci-lint at the version pinned in `versions.env`.
- From the repository root, run `make work` once so that `go.work` lists `./lifecycle`. This is
  only for editor navigation; no gate uses it.
- For the manual check (section 5): a Unix shell with `kill`, and `curl`.

## 1. Local gates (constitution)

```bash
make check MODULE=lifecycle   # golangci-lint with zero findings, then go test -race -shuffle=on with coverage ≥ 80%
```

**Expected**: a `PASS` line for `lifecycle` from both `lint` and `test`, with no
`.module-exceptions` entry.

## 2. Structure, tidy and cross-build (the same checks CI runs)

```bash
scripts/structure-check.sh lifecycle   # README, CHANGELOG, no replace, go directive ≤ 1.26
(cd lifecycle && GOWORK=off go mod tidy -diff)
for os in linux darwin windows; do for arch in amd64 arm64; do
  (cd lifecycle && GOWORK=off GOOS=$os GOARCH=$arch go vet ./...) || echo "FAIL $os/$arch"
done; done
(cd lifecycle && GOWORK=off go list -deps ./health | grep -c 'paccolamano/go/lifecycle$')
```

**Expected**:

- the structure check reports `PASS` for `lifecycle`;
- tidy prints nothing, `lifecycle/go.mod` has no `require` list (SC-007), and there are no `FAIL`
  lines;
- the last command prints `0`: `health` does not depend on `lifecycle` (FR-002).

## 3. Scenario checks per user story

Run these from `lifecycle/` with `GOWORK=off`. Test names follow the `Test<Feature>_<Scenario>`
convention fixed in `tasks.md`. Every test of time-dependent behavior runs inside `synctest`,
with no socket and no real waiting.

| Story | Command | What must hold |
|-------|---------|----------------|
| US1: ordered stop | `go test -race -run 'Register\|Order\|Context\|Signal' .` | Components stop in exact reverse order, each after the previous one, for 1 to 50 components (SC-001). Cancelling `Run`'s context does not cancel the components' contexts by itself, and their values are visible. A configured signal triggers the stop. Without `WithSignals`, nothing is intercepted. A second `Run` returns `ErrAlreadyStarted`. |
| US2: stop contract | `go test -race -run 'Stopper\|InFlight\|HTTPServer\|Closer\|StartStop\|Listener' .` | A component with `Stop` finishes its item with a valid context (SC-004). Without `Stop`, the context is cancelled at its turn. At the deadline the context is cancelled and `ErrStopTimeout` is reported. 100 requests in progress complete during the server's turn (SC-002), the shutdown hook runs, and the server's listener is closed. `Closer` calls each shape once. `StartStop` calls start at launch and stop at its turn. |
| US3: failures | `go test -race -run 'Failure\|ExitedEarly\|Panic\|ListenError' .` | An error, a nil return or a panic of one run stops the others in reverse order and is reported with its name, and the panic with its stack. An invalid port fails the server at once. Two near-simultaneous failures are both reported, with one stop sequence. |
| US4: drain | `go test -race -run 'Drain\|Ready' .` | Readiness fails at the trigger, and components keep serving for the drain delay, including requests sent during it (SC-002). With the default, the stop starts at once. `Ready` names `starting`, `stopping` and `stopped`. |
| US5: bounded | `go test -race -run 'Deadline\|Forced\|Expired\|Restore' .` | `Run` returns at `drain + stop timeout` with components that ignore cancellation or whose `Stop` never returns (SC-003). Remaining components still get their stop and cancellation. A second signal returns at once with `ErrForced`. Signal handling is restored after `Run`. |
| US6: health | `go test -race -run '.' ./health` | Statuses, headers and bodies match [health-http.md](contracts/health-http.md). Error texts containing hosts and connection strings never appear in responses (SC-005). A blocking check fails at its timeout. A panic fails its check. HEAD has no body, and POST gets 405. 100 concurrent requests share one evaluation, and a later request starts a new one. Invalid checks are refused. |
| US7: documentation | `go test -run 'Example' ./...` | Every example passes (SC-008). Section 4 covers the README. |
| Real signals | `go test -race -run 'RealSignal' .` | On Unix, real `SIGUSR1` signals trigger the stop, then force it. |
| Leaks | (all of the above) | `synctest.Test` fails if a goroutine outlives a test, so a full pass proves SC-006. |
| Coverage | `go test -cover ./...` | ≥ 80% (SC-009). |

## 4. Documentation review

Check the README against FR-045 to FR-053:

- [ ] a complete `main` with resources, workers, the HTTP server and the probes;
- [ ] the model, the ordering rule, the stop contract and the shutdown timeline;
- [ ] bounded shutdown, the second signal, and reading the App's error;
- [ ] signals, Windows, and testing through the context (the `synctest` warning);
- [ ] the compatibility table with every system of FR-048, and the RabbitMQ, gRPC and NATS
      components;
- [ ] the three cases of blocking functions;
- [ ] `sse` and `slogx`;
- [ ] health: readiness and liveness, reasons withheld, shared evaluations;
- [ ] Kubernetes: drain or `preStop`, the grace period, probe timeouts;
- [ ] third-party snippets marked as sketches with the client version, and no compiled example
      that imports them.

## 5. Manual check (optional)

Build a small program outside the repository with an App, `WithSignals(os.Interrupt,
syscall.SIGTERM)`, `WithDrainDelay(5 * time.Second)`, an HTTP server with `/readyz` and `/livez`
handlers, and a worker. Run it, then:

```bash
curl -i localhost:8080/readyz   # 200 ok
kill -TERM <pid>
curl -i localhost:8080/readyz   # 503 within the next 5 s; the server still answers
curl -i localhost:8080/livez    # 200 ok during the drain
kill -TERM <pid>                # a second signal: the program exits at once
```

**Expected**: the readiness answers 503 during the drain while the server keeps answering. The
second signal makes `Run` return with `ErrForced`.
