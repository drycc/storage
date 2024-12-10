package local

import (
	"context"
	"errors"
	"os"
	"time"

	"k8s.io/mount-utils"
)

var mounter = mount.New("")
var Mount = mounter.Mount
var PathExists = mount.PathExists
var UnmountTimeout = 30 * time.Second

func Unmount(target string) error {
	if unmounter, ok := mounter.(mount.MounterForceUnmounter); ok {
		return unmounter.UnmountWithForce(target, UnmountTimeout)
	}
	return mounter.Unmount(target)
}

func WaitMount(ctx context.Context, path string) error {
	interval := 10 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for mount")
		default:
			mounted, err := mounter.IsMountPoint(path)
			if err != nil {
				return err
			}
			if mounted {
				return nil
			}
		}
		time.Sleep(interval)
	}
}

func CheckMount(path string) (bool, error) {
	isMnt, err := mounter.IsMountPoint(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, 0750); err != nil {
				return false, err
			}
			isMnt = false
		} else if mount.IsCorruptedMnt(err) {
			if err := mounter.Unmount(path); err != nil {
				return false, err
			}
			isMnt, err = mounter.IsMountPoint(path)
		} else {
			return false, err
		}
	}
	return isMnt, err
}
