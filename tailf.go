// Package tailf provides a modern, zero-dependency file tailer for Go.
//
// It replicates the behavior of "tail -f" with proper handling of
// file truncation (copytruncate), file rotation (rename/create),
// and partial line buffering. It uses native context.Context for
// lifecycle management with no external dependencies.
package tailf

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Line represents a single line read from the tailed file.
type Line struct {
	// Text is the line content with trailing newline characters stripped.
	Text string

	// Time is when the line was read by the tailer.
	Time time.Time
}

// Tailer follows a file and emits lines as they are appended.
// Create one with [Follow] and receive lines from [Tailer.Lines].
type Tailer struct {
	lines chan Line
	err   error
	mu    sync.Mutex
	done  chan struct{}
}

// Lines returns a read-only channel that receives lines as they appear
// in the tailed file. The channel is closed when the context passed to
// [Follow] is cancelled or a fatal error occurs.
func (t *Tailer) Lines() <-chan Line {
	return t.lines
}

// Err returns the error that caused the tailer to stop, or nil if it
// was stopped by context cancellation. Only meaningful after the
// [Tailer.Lines] channel has been closed.
func (t *Tailer) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Done returns a channel that is closed when the tailer has fully stopped
// and all resources have been released.
func (t *Tailer) Done() <-chan struct{} {
	return t.done
}

func (t *Tailer) setErr(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.err = err
}

// Follow starts tailing the given file and returns a Tailer immediately.
// Lines are delivered through the [Tailer.Lines] channel. Tailing stops
// when ctx is cancelled.
//
// By default, tailing starts from the end of the file (new lines only).
// Use [WithFromStart] to read existing content first.
func Follow(ctx context.Context, path string, opts ...Option) (*Tailer, error) {
	o := defaults()
	for _, opt := range opts {
		opt(&o)
	}

	file, reader, fileID, err := openFile(path, o)
	if err != nil {
		return nil, fmt.Errorf("tailf: %w", err)
	}

	t := &Tailer{
		lines: make(chan Line, 64),
		done:  make(chan struct{}),
	}

	go func() {
		defer close(t.done)
		defer close(t.lines)
		defer file.Close()
		if err := tailLoop(ctx, t, file, reader, fileID, path, o); err != nil {
			t.setErr(err)
		}
	}()

	return t, nil
}

// FollowFunc tails the given file and calls fn for each line.
// It blocks until ctx is cancelled or a fatal error occurs.
//
// This is a convenience wrapper for cases where a channel is not needed.
func FollowFunc(ctx context.Context, path string, fn func(Line), opts ...Option) error {
	t, err := Follow(ctx, path, opts...)
	if err != nil {
		return err
	}
	for line := range t.Lines() {
		fn(line)
	}
	return t.Err()
}

func tailLoop(ctx context.Context, t *Tailer, file *os.File, reader *bufio.Reader, fileID fileIdentity, path string, o options) error {
	var partialLine string

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("read error: %w", err)
			}

			// EOF: buffer any partial data and check for truncation/rotation.
			partialLine += line

			var reopened bool
			file, reader, fileID, reopened, err = checkFileState(file, reader, fileID, path)
			if err != nil {
				return err
			}
			if reopened {
				partialLine = ""
			}

			// Reset reader to drop cached EOF so new data is visible.
			if !reopened {
				reader.Reset(file)
			}

			waitForData(ctx, o)
			continue
		}

		// Complete line received.
		if partialLine != "" {
			line = partialLine + line
			partialLine = ""
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		l := Line{
			Text: line,
			Time: time.Now(),
		}

		select {
		case t.lines <- l:
		case <-ctx.Done():
			return nil
		}
	}
}

// checkFileState detects file truncation and rotation, adjusting the
// file handle and reader as needed. Returns true for reopened if the
// file was rotated to a new inode.
func checkFileState(file *os.File, reader *bufio.Reader, fileID fileIdentity, path string) (*os.File, *bufio.Reader, fileIdentity, bool, error) {
	// Check truncation: current position beyond file size.
	currentPos, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return file, reader, fileID, false, fmt.Errorf("seek error: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		return file, reader, fileID, false, fmt.Errorf("stat error: %w", err)
	}

	if stat.Size() < currentPos {
		// File was truncated (e.g. logrotate copytruncate). Seek to start.
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return file, reader, fileID, false, fmt.Errorf("seek after truncation: %w", err)
		}
		reader.Reset(file)
		return file, reader, fileID, false, nil
	}

	// Check rotation: file at path has a different inode.
	pathInfo, err := os.Stat(path)
	if err != nil {
		// File may have been removed temporarily during rotation.
		// Not fatal — we'll retry on next poll.
		return file, reader, fileID, false, nil
	}

	newID := getFileIdentity(pathInfo)
	if newID != fileID && newID != (fileIdentity{}) {
		// File was rotated. Open the new file.
		newFile, err := os.Open(path)
		if err != nil {
			return file, reader, fileID, false, nil
		}
		file.Close()
		newReader := bufio.NewReader(newFile)

		newInfo, err := newFile.Stat()
		if err != nil {
			newFile.Close()
			return file, reader, fileID, false, fmt.Errorf("stat new file: %w", err)
		}

		return newFile, newReader, getFileIdentity(newInfo), true, nil
	}

	return file, reader, fileID, false, nil
}

// waitForData blocks until either the notify channel fires, the poll
// interval elapses, or the context is cancelled.
func waitForData(ctx context.Context, o options) {
	if o.notify != nil {
		// Wait for notification with poll interval as fallback timeout.
		timer := time.NewTimer(o.pollInterval)
		defer timer.Stop()
		select {
		case <-o.notify:
		case <-timer.C:
		case <-ctx.Done():
		}
		return
	}

	// Pure polling fallback.
	timer := time.NewTimer(o.pollInterval)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func openFile(path string, o options) (*os.File, *bufio.Reader, fileIdentity, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fileIdentity{}, err
	}

	if !o.fromStart {
		if _, err := file.Seek(0, io.SeekEnd); err != nil {
			file.Close()
			return nil, nil, fileIdentity{}, err
		}
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, fileIdentity{}, err
	}

	reader := bufio.NewReaderSize(file, o.bufSize)
	return file, reader, getFileIdentity(info), nil
}
