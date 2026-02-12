//go:build windows

package tailf

import "os"

type fileIdentity struct {
	dev uint64
	ino uint64
}

// getFileIdentity on Windows returns an empty identity.
// Rotation detection based on inode is not available on Windows,
// so the tailer degrades gracefully to truncation detection only.
func getFileIdentity(_ os.FileInfo) fileIdentity {
	return fileIdentity{}
}
