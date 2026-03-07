# lazyproc

A terminal UI process orchestrator for multi-process projects, with git worktree support.

---

## Keybindings

| Key                 | Action                                              |
| ------------------- | --------------------------------------------------- |
| `j` / `↓`           | Down (sidebar: next process, viewport: scroll down) |
| `k` / `↑`           | Up (sidebar: previous process, viewport: scroll up) |
| `tab`               | Toggle focus between sidebar and output pane        |
| `s`                 | Start focused process                               |
| `x`                 | Stop focused process                                |
| `r`                 | Restart focused process                             |
| `c`                 | Clear output buffer                                 |
| `g` / `G`           | Jump to top / bottom of output                      |
| `ctrl+u` / `ctrl+d` | Page up / page down                                 |
| `w`                 | Open worktree switcher (git repos only)             |
| `?`                 | Toggle full help                                    |
| `q` / `ctrl+c`      | Quit                                                |

---

## Use Cases

### Running multiple services together

Define all processes in `lazyproc.yaml`. They start in dependency order — each process waits for its `depends_on` targets to become ready before starting.

```yaml
processes:
  db:
    cmd: "docker compose up postgres"
    ready_when:
      stdout: "database system is ready to accept connections"

  api:
    cmd: "go run ./cmd/api"
    depends_on: [db]
    ready_when:
      stdout: "listening on :8080"

  frontend:
    cmd: "npm run dev"
    depends_on: [api]
```

### Process status at a glance

The sidebar shows a coloured dot per process:

| Dot colour | State      |
| ---------- | ---------- |
| yellow     | starting   |
| blue       | running    |
| green      | ready      |
| red        | crashed    |
| orange     | restarting |

### Switching git worktrees

Press `w` to open the worktree picker. On confirmation, all running processes are killed, each process's working directory is updated to the new worktree root, and only the previously-running processes are restarted in dependency order. The current branch is shown in the header. Outside a git repo, `w` does nothing.

---

## Config Reference

```yaml
settings:
  log_limit: 10000 # max lines buffered per process (default: 10000)
  shell: "/bin/sh" # shell used to run commands (default: /bin/sh)

processes:
  <name>:
    cmd: "<shell command>"
    cwd: "./subdir" # optional; relative to worktree root or absolute
    depends_on:
      - <other-process>
    ready_when:
      stdout: "<regex>" # transitions state to READY when matched on stdout
    highlight:
      - pattern: "<regex>" # first match wins; applied per output line
        color:
          "red" # named colour (red, green, yellow, blue, cyan,
          # magenta, orange, gray) or hex (e.g. "#FF0000")
```

Run with a custom config path:

```
lazyproc --config path/to/lazyproc.yaml
```

Debug logging (written to `debug.log`):

```
DEBUG=1 lazyproc
```

---

## Similar Projects

- [`mprocs`](https://github.com/pvolok/mprocs)
