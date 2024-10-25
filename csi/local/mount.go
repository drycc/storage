package local

import (
	"errors"
	"os"
	"time"

	"k8s.io/mount-utils"
)

var mountutils = mount.New("")
var Mount = mountutils.Mount
var Unmount = mountutils.Unmount
var PathExists = mount.PathExists

func WaitMount(path string, timeout time.Duration) error {
	var elapsed time.Duration
	var interval = 10 * time.Millisecond
	for {
		mounted, err := mountutils.IsMountPoint(path)
		if err != nil {
			return err
		}
		if mounted {
			return nil
		}
		time.Sleep(interval)
		elapsed = elapsed + interval
		if elapsed >= timeout {
			return errors.New("timeout waiting for mount")
		}
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
