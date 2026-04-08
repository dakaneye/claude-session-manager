# Manual Testing Guide: PTY Attach/Detach

This guide covers ONLY things that need a real TTY and a human eye on a terminal.
Everything deterministic — state transitions, key handling, rendering fixtures,
command construction, session discovery, dedup, health heuristics — lives in the
test suite under `internal/**/..._test.go`.

If a check here can be expressed as "given X state, pressing Y produces Z state",
it does not belong in this doc. Move it to a unit test instead.

## Setup

```bash
make install   # builds + installs to ~/go/bin/cs (already on PATH)
cs
```

## Detach chord

Default is `Ctrl+]`. Override via `CS_DETACH_BYTE` if your terminal eats it
(Warp intercepts `Ctrl+]` for its own bindings, for example):

```bash
CS_DETACH_BYTE=ctrl-q cs    # or ^Q, 0x11, 17
```

The inline hint printed at attach time reflects whatever chord is active, and
the check below verifies it.

## Status legend
- `[x]` Verified passing on this branch
- `[ ]` Not yet verified
- `[!]` Known issue / partially working
- `[~]` Out of scope / deferred

---

## 1. TUI visual baseline

- [x] TUI renders with split panes that fill the terminal without artifacts
- [x] Health dots are visually distinguishable (color: green / yellow / red)
- [x] `?` help screen renders fully within the visible terminal area (no clipping)
- [x] `q` exits and leaves the terminal in a clean state (prompt visible, cursor at bottom)

## 2. Spawn and attach (real claude + TTY)

From TUI, press `n`, pick type, enter directory, press `enter`.

- [x] `claude` launches full-screen in raw terminal mode
- [x] Typing a prompt gets a real response from claude
- [x] Configured detach chord returns to TUI with **no screen artifacts**
      (fixed — was an alt-screen layering bug)
- [ ] Detach hint: a line "[cs] Attached — press Ctrl+] to detach" prints briefly
      before claude's alt-screen takes over, and the terminal window title
      shows "cs attached — Ctrl+] to detach" while attached
- [ ] Sandbox picker: picking `s` launches `claude-sandbox spec` interactively
- [!] `Ctrl+C` is passed through to claude (kills it) rather than detaching —
      by design, but easy to confuse with detach
- [!] Warp terminal: multi-session flows may show rendering artifacts due to
      Warp's alt-screen handling; other terminals (iTerm2, Terminal.app) do not
      exhibit this

## 3. Reattach to running session

Select a running managed session, press `a`.

- [ ] Claude resumes full-screen, conversation context intact, interaction works
- [ ] Resize the terminal while attached — content reflows correctly (SIGWINCH)

## 4. Stop vs remove semantics

- [ ] `s` on a **running** managed session → confirmation "Stop X?" →
      process dies but session remains in the list as `stopped`
- [ ] `s` on a **stopped** managed session → confirmation "Remove X?" →
      metadata deleted, session disappears

## 5. Resume a stopped/dead session

- [ ] Confirmation → `y` → re-attach → claude picks up where it left off via
      `claude --resume` (requires real `claude` CLI)

## 6. CLI commands that spawn interactive claude

- [ ] `cs resume <session>` launches claude full-screen with `--resume`,
      in the session's original directory
- [ ] `cs new --sandbox` launches `claude-sandbox spec` interactively

## 7. Multi-session terminal behavior

Spawn 2-3 sessions via `n`.

- [ ] Can attach, detach, and attach a different session without terminal corruption
- [ ] Rapid attach/detach (press `a`, immediately `Ctrl+]`, repeat) leaves
      no stray escape sequences on the main screen

## 8. Process lifecycle across cs restarts

- [ ] Kill `cs` from another terminal (`pkill cs`) while attached — claude
      process continues running (verify with `ps`)
- [ ] Relaunch `cs` — the orphan appears in the list as a managed session
      that this new instance doesn't own
- [ ] `claude --resume <id>` from a shell still works independently

## 9. Sandbox stage transitions (requires claude-sandbox)

Skip if `claude-sandbox` is not installed.

- [ ] Sandbox session with `ready` status: attach flow spawns
      `claude-sandbox execute` on PTY, terminal UI is usable, output streams correctly
- [ ] After execute completes, attach flow spawns `claude-sandbox ship`

---

## Known limitations / deferred work

| Area | Status | Notes |
|---|---|---|
| `Ctrl+C` behavior | `[~]` | Passed through to claude, not bound to detach |
| Warp terminal artifacts | `[!]` | Suspected Warp-specific alt-screen handling; not reproduced in iTerm2 |
| Warp intercepts `Ctrl+]` | `[~]` | Warp grabs `Ctrl+]` before the proxy sees it. Use `CS_DETACH_BYTE=ctrl-q` (or any chord Warp doesn't claim) as a workaround |
