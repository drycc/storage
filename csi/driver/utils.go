package driver

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/context"
	"k8s.io/klog/v2"
)

func sanitizeVolumeID(volumeID string) string {
	volumeID = strings.ToLower(volumeID)
	if len(volumeID) > 63 {
		h := sha1.New()
		io.WriteString(h, volumeID)
		volumeID = hex.EncodeToString(h.Sum(nil))
	}
	return volumeID
}

// volumeIDToBucketPrefix returns the bucket name and prefix based on the volumeID.
// Prefix is empty if volumeID does not have a slash in the name.
func volumeIDToBucketPrefix(volumeID string) (string, string) {
	// if the volumeID has a slash in it, this volume is
	// stored under a certain prefix within the bucket.
	splitVolumeID := strings.SplitN(volumeID, "/", 2)
	if len(splitVolumeID) > 1 {
		return splitVolumeID[0], splitVolumeID[1]
	}

	return volumeID, ""
}

func getDiskUsage(path string) (uint64, uint64, uint64, uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err == nil {
		// in bytes
		blockSize := uint64(stat.Bsize)
		totalSize := blockSize * stat.Blocks
		freeSize := blockSize * stat.Bfree
		totalFiles := stat.Files
		freeFiles := stat.Ffree
		return totalSize, freeSize, totalFiles, freeFiles
	} else {
		klog.Errorf("Check volume path %s, err: %s", path, err)
		return 1, 1, 1, 1
	}
}

func doWithTimeout(parent context.Context, timeout time.Duration, f func() error) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	doneCh := make(chan error)
	go func() {
		doneCh <- f()
	}()

	select {
	case <-parent.Done():
		return parent.Err()
	case <-timer.C:
		return errors.New("function timeout")
	case err := <-doneCh:
		return err
	}
}
