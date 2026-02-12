package tailf_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Splat/go-tailf"
)

func ExampleFollow() {
	// Create a temp file for the example.
	dir, _ := os.MkdirTemp("", "tailf-example")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("hello world\n"), 0644)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t, err := tailf.Follow(ctx, path, tailf.WithFromStart(true))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	line := <-t.Lines()
	fmt.Println(line.Text)
	cancel()

	// Output: hello world
}

func ExampleFollowFunc() {
	// Create a temp file for the example.
	dir, _ := os.MkdirTemp("", "tailf-example")
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "app.log")
	os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0644)

	ctx, cancel := context.WithCancel(context.Background())

	count := 0
	tailf.FollowFunc(ctx, path, func(line tailf.Line) {
		fmt.Println(line.Text)
		count++
		if count == 3 {
			cancel()
		}
	}, tailf.WithFromStart(true))

	// Output:
	// one
	// two
	// three
}
