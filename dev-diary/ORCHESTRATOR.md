# Orchestrator

How the orchestrator runs the work in this repository. Workers read [`../AGENTS.md`](../AGENTS.md). The task rules are in [`README.md`](README.md). This file covers what only the orchestrator does.

**Process (user decision):** P0 stays with the orchestrator. It checks each result itself and lands it, or sends the same implementer back. From P1 on, a review agent reads the work, then a remediation agent fixes what the review found. Review and remediation repeat until a review comes back clean. The orchestrator lands only then.

The orchestrator is the one session the user talks to. It picks tasks, dispatches agents, validates and lands their work, and keeps the board true. It does not write product code.

---

## Two roles

| Role | Does | Never does |
|---|---|---|
| **Orchestrator** | Picks tasks. Dispatches agents. On P0, checks every result itself. From P1, runs review and remediation until a review is clean. Lands commits on `main`. Updates the status board and handoff log. Fixes documentation itself. Asks the user for decisions. | Implement a task. Review or remediate a task itself from P1 on. Land work that is not clean. Treat an agent's message as the user's approval. |
| **Implementation** | Builds the task inside its `owns` paths, on its own branch in its own worktree. Sets its task to `in-review`. Reports. | Mark its task `done`. Edit `dev-diary/README.md`. Touch files outside `owns`. Review its own task. |
| **Review** (from P1) | Reads the branch. Runs the gate. Reports each **Done when** clause and every finding. | Edit, commit, push, or set a task `done`. |
| **Remediation** (from P1) | Fixes the review's findings on the same branch, inside `owns`. Sets the task back to `in-review`. Reports. | Mark its task `done`. Expand the task past the findings. |

On P0, a problem goes back to the same implementer with the evidence. From P1, it goes to a remediation agent, and then to a new review. The orchestrator fixes only documentation itself: markdown under `dev-diary/` or `docs/`, `README.md`, `AGENTS.md`, or a comment that does not describe behaviour. Anything that touches behaviour, a contract, a test, a fixture or the gate goes to the implementer on P0, and to the remediation agent from P1.

---

## The loop

### 1. Pick

A task is **ready** when its status is `not-started`, every task in `requires` is `done`, and it is not `blocked`.

- Prefer ★ tasks on the critical path.
- Run ready tasks in parallel when their `owns` paths do not overlap.
- If a task names a decision as the user's, ask the user before dispatching, with options and a recommendation.
- Never schedule. Order work by `requires` only: no dates, windows or target days, in any document or message.

### 2. Prepare

- `main` is clean, and `./tools/check.sh` passes in the main checkout.
- Set the task to `status: claimed:<agent>` on `main` and commit. That claim is how a later session sees the task is taken.
- The user assigns the agent and the model for each task. The task's class (README, Task Pattern) says what kind of work it is; it does not pick the model.

### 3. Dispatch

Always in the background, always in its own worktree (`isolation: worktree`). A new worktree is cut from `origin/main`, not the local `main`. If local `main` is ahead, either push first (with the user's approval) or tell the worker in the brief to move its new branch to the brief's commit before starting, and to stop if it can't. Use the brief template below. A worker starts cold, so the brief carries everything it needs: what to read, what to build, what it may touch, how to prove it, and what to report.

### 4. Check

The report is a claim. What has to be true:

- every **Done when** clause is met, with evidence from the check, not just the report's word;
- the diff holds only the task's `owns` paths and its own status lines, and does what the task says;
- `./tools/check.sh` passes;
- no key value is in any file (see Secrets);
- for a task that records fixtures or prompts, an offline replay from the branch gives what the report says;
- for a `b` task, the live transcript is in the report and matches the committed test.

**P0.** The orchestrator does this check itself and lands, or sends the same implementer back with what is missing. There is no review agent and no remediation agent.

**From P1.** The orchestrator does not check the work itself. It dispatches a review agent, in its own worktree, detached at the branch tip, told not to edit. The review reports clean, or it lists findings. A clean review means every clause above holds and nothing must be fixed.

If the review is not clean, the orchestrator dispatches a remediation agent on the task branch with those findings. The remediator edits only inside `owns`, commits, and returns the task to `in-review`. Then a new review agent runs. Review and remediation repeat until a review comes back clean. The orchestrator lands only then. Two agents never edit one checkout. The reviewer never edits.

### 5. Land

In the task's worktree:
1. `git rebase main`. Conflicts in `dev-diary/` status lines are the orchestrator's to resolve, keeping both sides' truth. A conflict in code goes back to the implementer.
2. `./tools/check.sh` passes.

Then in the main checkout:

3. `git merge --ff-only <branch>`.
4. `./tools/check.sh` passes, with the gitignored files (`.env`, `dresscode.toml`, `data/`) present.
5. For a task that wrote `testdata/`, grep for every key value and confirm it matches nothing.

If step 4 fails, `main` is broken. Record a finding against the task at once, and fix it before anything else lands.

### 6. Close

- Set the task to `status: done` in its phase file, and tick its exit criteria only where they are true.
- Update the README status board and add a handoff-log entry at the top: what exists, what surprised, and what the next agent must not have to work out again. Only the orchestrator edits `dev-diary/README.md`, so parallel branches never conflict on it.
- Remove the worktree and delete its branch **in the same hand as the close**: `git worktree remove <path>` then `git branch -d <branch>`, after `git cherry main <branch>` shows no `+` lines and the worktree is clean. A landed task with a branch still on it is a finished close, not a paused one — the next session reads an unlanded-looking branch as in-flight work. Never remove a worktree with unmerged or uncommitted work; report it instead.
- Record the landing in Lambo with `lambo_record_action`, and every decision with `lambo_derive`: what was decided and why.
- Tell the user what landed, what was found, and what is ready next.

---

## Cleanup

Every task is dispatched in a worktree under `~/work/dresscode-wt/`, and a UI task starts a dev server inside it. Both outlive the agent that made them, and the protocol only cleans the task being landed, so the residue accumulates silently. It is the orchestrator's to clear.

- **Landing removes both halves.** Worktree and branch go in the close hand, not "later". `git branch -d` (never `-D`) is the guard: it refuses anything `git cherry` says is unlanded, so a `-d` that fails is a finding, not an obstacle.
- **A dev server is a leak.** `git worktree remove` fails with "Directory not empty" when a server is still rewriting `web/.vite/deps`, and a dead agent never kills its own server. So the brief says so (see the template), and the sweep checks for it. A stopped agent's worktree is removed the same hand the task is re-dispatched or retired.
- **Review worktrees go when their round reports.** A detached review checkout at an old tip cannot test a newer claim; keep it only while that round's findings are still live.

### The sweep

Run at the end of each phase, and whenever the worktree list and the directory disagree:

1. `git worktree prune`, then compare `git worktree list` with `ls ~/work/dresscode-wt`. A directory in no registry is a remnant.
2. A remnant may hold work. Check every non-cache file in it against `main` (`git hash-object <file>` vs `git rev-parse main:<path>`). Identical or absent, it is cache and goes. Anything else, stop and read it before deleting.
3. Branches: for each `task-*` branch not in the worktree registry, `git cherry main <branch>` must show no `+` lines before `git branch -d`.
4. Servers: `ps -eo pid,args | grep vite` — any server whose path is not a registered worktree is orphaned. Stop it, then re-check that nothing still holds a removed path.

Report what the sweep removed and what it refused to remove.

---

## Brief template

```
You are doing task <id> ("<title>") for Dresscode. You are in your own worktree of
~/work/dresscode, branched from main at <hash>. Commit on your branch. Do not push,
merge, or touch the main checkout.

Read first: AGENTS.md; product.md; docs/architecture.md (<sections>);
dev-diary/README.md (<sections>); dev-diary/PHASE-<n>-*.md (<id>, and the tasks it
requires); <fixtures and earlier code to read>.

What to build: <the task, restated with any decisions the user has made since it was written>.
You own: <owns paths>. Touch nothing else.

Hard rules: keys only from the environment or the keyring, never printed or written;
<budget and the guard that stops at it, for a live task>; new code only, never copied
from Reprise, Before Yes or any earlier project; plain commit messages with no Co-Authored-By
or Claude/Anthropic attribution; no dates or schedules in the dev-diary.

Prove it: <tests, the gate, the pins this task needs>.

Dev-diary: set <id> to `in-review` in its phase file, and add your notes under the task.
Do not edit dev-diary/README.md; put your handoff-log text in your report.

If you start a dev server, kill it before you report. Nothing else can remove this
worktree while it holds web/.vite/deps open.

Never tell a UI worker that `web/tests/a11y/` is Chromium-backed: it mounts in jsdom,
which performs no layout, so it cannot see a `position: fixed` bar and a width
assertion there proves nothing. A mobile geometry claim needs a browser-backed rect
assertion; a text pin on a CSS file guards against a careless edit and nothing else.

Report: branch, `git log --oneline main..HEAD`, each Done-when clause with its evidence,
anything unmet, and anything surprising.
```

---

## Secrets and spend

- **Keys come from the keyring or the environment.** YouCam: `secret-tool lookup service youcam account <account> kind api-key`. Workers AI: `… service cloudflare account <account> kind workers-ai`. Resend and OpenRouter: settled in T0.2. A worktree has no `.env`. A worker that needs a key writes a wrapper script outside the repository, under the session scratchpad, that loads the key and `exec`s the command. The wrapper is never committed.
- **YouCam units are the scarcest thing in the project.** Every live task's brief names a unit budget, and the orchestrator keeps the running unit count in the handoff log. Judging needs its own share; never let development spend it.
- **No key is ever printed.** Check for leaks with `grep -rlF -- "$VALUE"`, which prints only file names, with the value loaded and never echoed.
- **Every live (`b`) task carries a budget and a guard in its brief.** Run one case first, check its cost, and stop and report if it goes over. The orchestrator keeps the running spend in the handoff log.
- **Anything outward is the user's call:** filing an issue, pushing, making the repository public, publishing the video, submitting, or a spend beyond the brief's budget. Ask first, every time.

---

## Asking the user

Ask, with options and a recommendation, when:
- a task names a decision as the user's;
- evidence contradicts product.md or an earlier decision;
- a budget would be exceeded;
- a task is blocked and the way forward is a product choice;
- anything outward-facing is next.

Report outcomes plainly: what landed, what failed, and what the orchestrator itself got wrong.
