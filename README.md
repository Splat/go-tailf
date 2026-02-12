# go-tailf

A modern, zero-dependency Go library for tailing files. Built with native `context.Context` and the Go standard library only.

```
go get github.com/Splat/go-tailf
```

## Why go-tailf?

The Go ecosystem's existing tail libraries (`nxadm/tail`, `hpcloud/tail`) depend on the unmaintained `gopkg.in/tomb.v1` for goroutine lifecycle management, which can cause goroutine leaks and deadlocks. Several forks (`grafana/tail`, `go-faster/tail`) have been archived. This isn't as battle tested as the prior solutions but does offer a clean implementation leveraging `context.Context`.

**go-tailf** is a clean-room implementation that uses only the Go standard library:

|                        | go-tailf             | nxadm/tail                |
|------------------------|----------------------|---------------------------|
| Goroutine lifecycle    | `context.Context`    | `gopkg.in/tomb.v1`        |
| External dependencies  | **0**                | 3 (tomb, fsnotify, gopkg) |
| File truncation        | Yes                  | Yes                       |
| File rotation          | Yes (inode tracking) | Yes                       |
| Partial line buffering | Yes                  | Yes                       |
| Channel API            | Yes                  | Yes                       |
| Callback API           | Yes                  | No                        |
| Event-driven mode      | Yes (pluggable)      | fsnotify built-in         |
| Clean shutdown         | `cancel()`           | `t.Stop()` (can deadlock) |

## Quick Start

### Channel API

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

t, err := tailf.Follow(ctx, "/var/log/app.log")
if err != nil {
    log.Fatal(err)
}

for line := range t.Lines() {
    fmt.Println(line.Text)
}

if err := t.Err(); err != nil {
    log.Fatal(err)
}
```

### Callback API

```go
err := tailf.FollowFunc(ctx, "/var/log/app.log", func(line tailf.Line) {
    fmt.Printf("[%s] %s\n", line.Time.Format(time.RFC3339), line.Text)
})
```

### Read From Beginning
Since this is a tail-f library the default is to read from the end of the file. To read from the beginning instead, pass in the appropriate option:
```go
t, err := tailf.Follow(ctx, path, tailf.WithFromStart(true))
```

## Options
There are a few options available to tail files:

| Option                | Default | Description                                |
|-----------------------|---------|--------------------------------------------|
| `WithFromStart(true)` | `false` | Read from beginning of file instead of end |
| `WithPollInterval(d)` | `100ms` | How often to check for new data at EOF     |
| `WithNotify(ch)`      | `nil`   | External notification channel (see below)  |
| `WithBufSize(n)`      | `4096`  | Read buffer size in bytes                  |

## Event-Driven Mode with fsnotify

The core library has zero dependencies by design. To use filesystem notifications instead of polling, plug in any watcher via the `WithNotify` channel:

```go
import (
    "github.com/fsnotify/fsnotify"
    "github.com/Splat/go-tailf"
)

// Set up fsnotify.
watcher, _ := fsnotify.NewWatcher()
defer watcher.Close()
watcher.Add("/var/log/app.log")

// Bridge fsnotify events to a notification channel.
notify := make(chan struct{}, 1)
go func() {
    for range watcher.Events {
        select {
        case notify <- struct{}{}:
        default:
        }
    }
}()

// The tailer reads immediately on notification instead of polling.
// The poll interval is still used as a fallback timeout.
t, _ := tailf.Follow(ctx, "/var/log/app.log", tailf.WithNotify(notify))

for line := range t.Lines() {
    fmt.Println(line.Text)
}
```

## What It Handles

### File Truncation (copytruncate)
When a log rotation tool truncates a file in-place, go-tailf detects that the file size is smaller than the current read position and seeks back to the beginning.

### File Rotation (rename/create)
When a log rotation tool renames the current file and creates a new one, go-tailf detects the inode change and reopens the file at the same path. On Windows, inode-based rotation detection is not available and the tailer degrades to truncation detection only.

### Partial Lines
Data written without a trailing newline is buffered internally until the line is complete. This prevents emitting half-written log entries.

### Clean Shutdown
Cancel the context and the tailer stops. No deadlocks, no leaked goroutines. Use `t.Done()` to wait for full cleanup:

```go
cancel()
<-t.Done() // blocks until all resources are released
```

## Types

```go
// Line represents a single line read from the tailed file.
type Line struct {
    Text string    // line content (trailing newline stripped)
    Time time.Time // when the line was read
}
```

## License

MIT
