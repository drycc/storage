package mounter

import (
	"fmt"

	"github.com/drycc/storage/csi/local"
)

var globalProviders = make(map[string]Mounter)

type Bucket struct {
	Name     string            `json:"name"`
	Prefix   string            `json:"prefix"`
	Endpoint string            `json:"endpoint"`
	Capacity uint64            `json:"capacity"`
	Secrets  map[string]string `json:"secrets"`
}

type Mounter interface {
	Mount(bucket *Bucket, target string, options []string) error
	Quota(bucket *Bucket) error
	Unmount(target string) error
}

func GetMounter(mounterString string) (Mounter, error) {
	mounter := globalProviders[mounterString]
	if mounter == nil {
		return nil, fmt.Errorf("provider %v unimplemented", mounterString)
	}
	return mounter, nil
}

type BaseMounter struct {
	Name        string
	Description string
}

func (mounter *BaseMounter) Unmount(target string) error {
	if err := local.Unmount(target); err != nil {
		return err
	}

	return nil
}
