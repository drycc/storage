package mounter

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"

	"github.com/drycc/storage/csi/local"
	"github.com/golang/glog"
	"github.com/seaweedfs/seaweedfs/weed/pb/mount_pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	name := "seaweedfs"
	globalProviders[name] = &seaweedfsMounter{
		BaseMounter: BaseMounter{
			Name:        name,
			Description: "SeaweedFS is a simple and highly scalable distributed file system.",
		},
	}
}

type seaweedfsMounter struct {
	BaseMounter
}

func (mounter *seaweedfsMounter) Mount(bucket *Bucket, target string, options []string) error {
	command := "weed"
	sock := mounter.getSeaweedfsLocalSocket(bucket.Name, bucket.Prefix)
	kwargs := map[string]string{
		"dirAutoCreate":   "true",
		"umask":           "000",
		"filer":           bucket.Endpoint,
		"filer.path":      fmt.Sprintf("/buckets/%s/%s", bucket.Name, bucket.Prefix),
		"collection":      bucket.Name,
		"dir":             target,
		"localSocket":     sock,
		"cacheDir":        os.TempDir(),
		"cacheCapacityMB": "100",
	}
	args := []string{"-logtostderr=true", "mount"}
	for k, v := range kwargs {
		if v != "" {
			args = append(args, fmt.Sprintf("-%s=%s", k, v))
		}
	}
	args = append(args, options...)
	cmd := exec.Command(command, args...)
	glog.Infof("Mounting fuse with command: %s and args: %s", command, args)
	cmd.Run()
	// log fuse process messages - we need an easy way to investigate crashes in case it happens
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Run()
	if err != nil {
		glog.Errorf("weed mount exit, pid: %d, path: %v, error: %v", cmd.Process.Pid, target, err)
	} else {
		glog.Infof("weed mount exit, pid: %d, path: %v", cmd.Process.Pid, target)
	}
	if local.Unmount(target); err != nil {
		glog.Errorf("unmount error: %v", err)
	}
	return err
}

func (mounter *seaweedfsMounter) Quota(mountBucket *Bucket) error {
	glog.Infof("node expand volume %+v", mountBucket)
	sock := mounter.getSeaweedfsLocalSocket(mountBucket.Name, mountBucket.Prefix)
	return mounter.seaweedfsQuota(sock, int64(mountBucket.Capacity))
}

func (mounter *seaweedfsMounter) seaweedfsQuota(localSocket string, sizeByte int64) error {
	target := fmt.Sprintf("passthrough:///unix://%s", localSocket)
	dialOption := grpc.WithTransportCredentials(insecure.NewCredentials())

	clientConn, err := grpc.NewClient(target, dialOption)
	if err != nil {
		return err
	}
	defer clientConn.Close()

	// We can't create PV of zero size, so we're using quota of 1 byte to define no quota.
	if sizeByte == 1 {
		sizeByte = 0
	}

	client := mount_pb.NewSeaweedMountClient(clientConn)
	_, err = client.Configure(context.Background(), &mount_pb.ConfigureRequest{
		CollectionCapacity: sizeByte,
	})
	return err
}

func (mounter *seaweedfsMounter) getSeaweedfsLocalSocket(bucket, prefix string) string {
	h := md5.New()
	h.Write([]byte(fmt.Sprintf("%s-%s", bucket, prefix)))
	b := h.Sum(nil)
	socket := fmt.Sprintf("/tmp/seaweedfs-mount-%d.sock", binary.BigEndian.Uint64(b))
	return socket
}
