package local

import (
	"context"
	"errors"
	"os"
	"time"

	"k8s.io/mount-utils"
)

var mountutils = mount.New("")
var Mount = mountutils.Mount
var Unmount = mountutils.Unmount
var PathExists = mount.PathExists

func WaitMount(ctx context.Context, path string) error {
	interval := 10 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return errors.New("timeout waiting for mount")
		default:
			mounted, err := mountutils.IsMountPoint(path)
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
	isMnt, err := mountutils.IsMountPoint(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(path, 0750); err != nil {
				return false, err
			}
			isMnt = false
		} else if mount.IsCorruptedMnt(err) {
			if err := mountutils.Unmount(path); err != nil {
				return false, err
			}
			isMnt, err = mountutils.IsMountPoint(path)
		} else {
			return false, err
		}
	}
	return isMnt, err
}
