package tailf

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFollowFromStart(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	content := "line one\nline two\nline three\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	var lines []string
	for i := 0; i < 3; i++ {
		select {
		case line := <-tailer.Lines():
			lines = append(lines, line.Text)
		case <-ctx.Done():
			t.Fatalf("timed out after receiving %d lines", len(lines))
		}
	}

	cancel()
	<-tailer.Done()

	expected := []string{"line one", "line two", "line three"}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want)
		}
	}
}

func TestFollowFromEnd(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	// Write initial content that should NOT be seen.
	if err := os.WriteFile(path, []byte("old line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	// Append new content after tailer is started.
	time.Sleep(150 * time.Millisecond)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("new line\n")
	f.Close()

	select {
	case line := <-tailer.Lines():
		if line.Text != "new line" {
			t.Errorf("got %q, want %q", line.Text, "new line")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for new line")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowAppendsAfterEOF(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte("first\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	// Read the first line.
	select {
	case line := <-tailer.Lines():
		if line.Text != "first" {
			t.Errorf("got %q, want %q", line.Text, "first")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for first line")
	}

	// Wait for tailer to hit EOF, then append.
	time.Sleep(200 * time.Millisecond)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("second\n")
	f.Close()

	select {
	case line := <-tailer.Lines():
		if line.Text != "second" {
			t.Errorf("got %q, want %q", line.Text, "second")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for appended line after EOF")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowTruncation(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte("before truncation\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	// Read initial line.
	select {
	case line := <-tailer.Lines():
		if line.Text != "before truncation" {
			t.Errorf("got %q, want %q", line.Text, "before truncation")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for initial line")
	}

	// Truncate file (simulates logrotate copytruncate).
	time.Sleep(200 * time.Millisecond)
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}

	// Wait for tailer to detect truncation.
	time.Sleep(200 * time.Millisecond)

	// Write new content.
	f, err := os.OpenFile(path, os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("after truncation\n")
	f.Close()

	select {
	case line := <-tailer.Lines():
		if line.Text != "after truncation" {
			t.Errorf("got %q, want %q", line.Text, "after truncation")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for line after truncation")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowRotation(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte("before rotation\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	// Read initial line.
	select {
	case line := <-tailer.Lines():
		if line.Text != "before rotation" {
			t.Errorf("got %q, want %q", line.Text, "before rotation")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for initial line")
	}

	// Simulate rotation: rename old file and create new one.
	time.Sleep(200 * time.Millisecond)
	rotated := filepath.Join(tmp, "test.log.1")
	if err := os.Rename(path, rotated); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("after rotation\n"), 0644); err != nil {
		t.Fatal(err)
	}

	select {
	case line := <-tailer.Lines():
		if line.Text != "after rotation" {
			t.Errorf("got %q, want %q", line.Text, "after rotation")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for line after rotation")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowPartialLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	// Write partial line (no newline).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("partial")
	f.Close()

	// Wait to ensure partial data is buffered but not emitted.
	time.Sleep(300 * time.Millisecond)

	select {
	case line := <-tailer.Lines():
		t.Errorf("should not have received line yet, got %q", line.Text)
	default:
		// Expected: no line yet.
	}

	// Complete the line.
	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(" complete\n")
	f.Close()

	select {
	case line := <-tailer.Lines():
		if line.Text != "partial complete" {
			t.Errorf("got %q, want %q", line.Text, "partial complete")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for completed partial line")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowContextCancel(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	// Lines channel should close promptly.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case <-tailer.Done():
		// Expected.
	case <-timer.C:
		t.Fatal("tailer did not stop after context cancel")
	}

	if err := tailer.Err(); err != nil {
		t.Errorf("expected nil error after cancel, got %v", err)
	}
}

func TestFollowFunc(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	content := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var lines []string
	done := make(chan error, 1)

	go func() {
		done <- FollowFunc(ctx, path, func(line Line) {
			lines = append(lines, line.Text)
			if len(lines) == 3 {
				cancel()
			}
		}, WithFromStart(true))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FollowFunc returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FollowFunc did not return")
	}

	expected := []string{"alpha", "beta", "gamma"}
	if len(lines) != len(expected) {
		t.Fatalf("got %d lines, want %d", len(lines), len(expected))
	}
	for i, want := range expected {
		if lines[i] != want {
			t.Errorf("line %d: got %q, want %q", i, lines[i], want)
		}
	}
}

func TestFollowNotify(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	notify := make(chan struct{}, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use a long poll interval so only the notify channel triggers reads.
	tailer, err := Follow(ctx, path,
		WithFromStart(true),
		WithPollInterval(10*time.Second),
		WithNotify(notify),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Write a line and send notification.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("notified\n")
	f.Close()

	notify <- struct{}{}

	select {
	case line := <-tailer.Lines():
		if line.Text != "notified" {
			t.Errorf("got %q, want %q", line.Text, "notified")
		}
	case <-ctx.Done():
		t.Fatal("timed out — notify channel did not trigger read")
	}

	cancel()
	<-tailer.Done()
}

func TestFollowNonExistent(t *testing.T) {
	ctx := context.Background()
	_, err := Follow(ctx, "/nonexistent/path/file.log")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "tailf:") {
		t.Errorf("error should be prefixed with 'tailf:', got: %v", err)
	}
}

func TestFollowLineTime(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.log")

	if err := os.WriteFile(path, []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	before := time.Now()
	tailer, err := Follow(ctx, path, WithFromStart(true))
	if err != nil {
		t.Fatal(err)
	}

	select {
	case line := <-tailer.Lines():
		if line.Time.Before(before) {
			t.Error("line.Time should be after test start")
		}
		if line.Time.After(time.Now()) {
			t.Error("line.Time should not be in the future")
		}
	case <-ctx.Done():
		t.Fatal("timed out")
	}

	cancel()
	<-tailer.Done()
}
