# Practice service

Go/PostgreSQL service for HENU Kit question banks, practice sessions, server-side answer evaluation, favorites, rankings, personal statistics, and correction feedback.

## Browser boundary

HENU Kit Portal and Portal Gateway own authenticated browser journeys. This service does not expose an independent OAuth login or callback and does not issue new product sessions. Guest practice uses only the service-issued anonymous HttpOnly cookie; authenticated Portal commands bind the signed Platform user ID on the server.

The versioned bank-administration API remains frozen for trusted migration callers only. It has no browser page, navigation entry, admin-token form, or independent login. Removing that API requires a separate breaking-contract release.

## Runtime

Required inputs are documented in `.env.example`. PostgreSQL is the only runtime source of truth; startup never scans local JSON or turns fixtures into production success. Portal read and command clients use separate HMAC credentials, nonce replay protection, bounded bodies, and default-off write gates.

Apply migrations:

```bash
go run ./cmd/migrate -database "$DATABASE_URL"
```

Run locally:

```bash
go run ./cmd/server
```

## Verification

```bash
go test ./...
bash scripts/generate-contract.sh
git diff --exit-code -- internal/contract ../web-app/src/generated/quizcraft-api
```

The browser cutover verifier exercises real guest practice, answer submission, correction feedback, ranking, retired-route convergence, and visible-copy checks at desktop and 390px. It requires HTTPS and does not inject an independent product login Session.

## Operational claims

Keep these states separate: candidate build, CI result, merge SHA, deployed SHA, and production user journey. `/healthz`, `/readyz`, HTTP 200, or a single redirect are not acceptance evidence for practice or authentication.

## Learning entitlement caller (dark)

QuizCraft's optional `QUIZCRAFT_LEARNING_ENTITLEMENT_URL`, `_CLIENT_ID`,
`_KEY_ID` and `_SECRET` must be set together. The URL is the private Account
Portfolio origin; its dedicated credential must match the service's
`ACCOUNT_PORTFOLIO_QUIZCRAFT_*` settings, not a Portal/Console or QuizCraft
command credential. The compose example wires the shared values into both
services with empty defaults. Partial, placeholder or reused QuizCraft
credentials prevent startup. This only prepares a signed, uncached internal
caller: it does **not** expose report routes, start a model worker, or enable
learning feedback. Until those gates are implemented and reviewed, leave the
settings empty.

### Manual generation abuse guard

`QUIZCRAFT_LEARNING_MANUAL_LIMIT` bounds how many course-feedback generations
one member may start for one course inside a fixed one-hour window (default
`10`; `1..1000` accepted; `0` disables the guard explicitly). The guard counts
stored jobs, runs inside the member's preference lock only when a new job would
be written, so replays and concurrent retries of the same request still reuse
their job for free, and scheduled (`automatic`) generation is never limited. It
is abuse protection, not a usage quota: no credits are charged and no daily
allowance exists. Exceeding it is `429 rate_limited` from Core, which the Portal
Gateway must forward as `429 practice_command_rate_limited` instead of reporting
a dependency failure.
