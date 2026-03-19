# lazyproc

A terminal UI process orchestrator for multi-process projects with git worktree support.

## Quick Start

Create `lazyproc.yaml`:

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

Run it:

```bash
lazyproc
```

**Or** pass commands directly with custom names:

```bash
lazyproc --labels "Frontend,Logs" "npm run dev" "tail -f log.txt"
```

## How It Works

Processes start in dependency order. Each waits for its `depends_on` targets to reach ready state before starting.

Process status in the sidebar:

- Yellow: starting
- Blue: running
- Green: ready
- Red: crashed
- Orange: restarting

Press `w` to switch git worktrees. All running processes stop, working directories update, and previously-running processes restart in order.

## Keybindings

| Key                  | Action                                 |
| -------------------- | -------------------------------------- |
| `j` or `Down`        | Down                                   |
| `k` or `Up`          | Up                                     |
| `Tab`                | Toggle sidebar/output focus            |
| `s`                  | Start process                          |
| `x`                  | Stop process                           |
| `r`                  | Restart process                        |
| `c`                  | Clear output                           |
| `g` or `G`           | Jump to top or bottom                  |
| `Ctrl+u` or `Ctrl+d` | Page up or down                        |
| `v`                  | Enter visual mode (disable autoscroll) |
| `y`                  | Copy selected lines (visual mode only) |
| `Esc`                | Exit visual mode                       |
| `w`                  | Switch worktree                        |
| `?`                  | Show help                              |
| `q` or `Ctrl+c`      | Quit                                   |

## Configuration

```yaml
settings:
  log_limit: 10000
  shell: "/bin/sh"

processes:
  <name>:
    cmd: "<shell command>"
    cwd: "./subdir"
    depends_on:
      - <process>
    ready_when:
      stdout: "<regex>"
    highlight:
      - pattern: "<regex>"
        color: "red"
```

### Settings

`log_limit`: Maximum lines buffered per process. Default: 10000.

`shell`: Shell used to run commands. Default: `/bin/sh`.

### Process Options

`cmd`: The command to run.

`cwd`: Working directory, relative to worktree root or absolute. Optional.

`depends_on`: List of processes that must reach ready state first.

`ready_when`: Mark process as ready when this regex matches stdout.

`highlight`: List of regex patterns to colorize. Applies first match per line. Colors: red, green, yellow, blue, cyan, magenta, orange, gray, or hex (#FF0000).

## Similar Projects

- [mprocs](https://github.com/pvolok/mprocs)
