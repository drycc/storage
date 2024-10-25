/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driver

import (
	"regexp"
	"syscall"
	"time"

	"github.com/drycc/storage/csi/local"
	"github.com/drycc/storage/csi/mounter"
	"github.com/golang/glog"
	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	optionsKey          = "options"
	defaultCheckTimeout = 2 * time.Second
)

var (
	nodeCaps = []csi.NodeServiceCapability_RPC_Type{
		csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
		csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
		csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
	}
)

type NodeServer struct {
	csi.UnimplementedNodeServer
	driver     *CSIDriver
	driverInfo *DriverInfo
}

// NodeStageVolume is called by the CO prior to the volume being consumed by any workloads on the node by `NodePublishVolume`
func (ns *NodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	glog.Infof("using NodeStageVolume: %#v, %#v", ctx, req)
	volumeId := req.GetVolumeId()
	stagingTargetPath := req.GetStagingTargetPath()
	bucket, prefix := volumeIDToBucketPrefix(volumeId)

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(volumeId) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging target path missing in request")
	}
	glog.Infof("check stage target path: %s", stagingTargetPath)
	mounted, err := local.CheckMount(stagingTargetPath)
	if err != nil {
		glog.Errorf("check stage target path error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	if mounted {
		glog.Infof("ignore, stage target path %s already mounted.", stagingTargetPath)
		return &csi.NodeStageVolumeResponse{}, nil
	}

	options := make([]string, 0)
	if req.VolumeContext[optionsKey] != "" {
		re, _ := regexp.Compile(`([^\s"]+|"([^"\\]+|\\")*")+`)
		re2, _ := regexp.Compile(`"([^"\\]+|\\")*"`)
		re3, _ := regexp.Compile(`\\(.)`)
		for _, opt := range re.FindAll([]byte(req.VolumeContext[optionsKey]), -1) {
			// Unquote options
			opt = re2.ReplaceAllFunc(opt, func(q []byte) []byte {
				return re3.ReplaceAll(q[1:len(q)-1], []byte("$1"))
			})
			options = append(options, string(opt))
		}
	}

	nodeDriver, err := NewNodeDriver(volumeId, ns.driverInfo.MounterInfo)
	if err != nil {
		glog.Errorf("new node driver error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	capacity, err := nodeDriver.K8sClient.GetVolumeSize(volumeId)
	if err != nil {
		glog.Infof("get volume size error: %v", err)
	}
	mounterBucket := &mounter.Bucket{
		Name:     bucket,
		Endpoint: *ns.driverInfo.MounterInfo.Endpoint,
		Prefix:   prefix,
		Capacity: uint64(capacity),
		Secrets:  req.GetSecrets(),
	}

	if err := nodeDriver.Mount(mounterBucket, stagingTargetPath, options); err != nil {
		glog.Errorf("mount stage target path error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume is a reverse operation of `NodeStageVolume`
func (ns *NodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeId := req.GetVolumeId()
	stagingTargetPath := req.GetStagingTargetPath()

	glog.Infof("node unstage volume %s from %s", volumeId, stagingTargetPath)

	// Check arguments
	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	nodeDriver, err := NewNodeDriver(volumeId, ns.driverInfo.MounterInfo)
	if err != nil {
		glog.Errorf("create node driver error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := nodeDriver.Unmount(stagingTargetPath); err != nil {
		glog.Errorf("unmount stage target path error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}

	glog.Infof("volume %s successfully unstaged from %s", volumeId, stagingTargetPath)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (ns *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	stagingTargetPath := req.GetStagingTargetPath()

	glog.Infof("node publish volume %s to %s", volumeID, targetPath)

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "Staging target path missing in request")
	}
	//TODO
	// check whether it can be mounted
	if mounted, err := local.CheckMount(targetPath); err != nil {
		return nil, err
	} else if mounted {
		// maybe already mounted?
		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Use bind mount to create an alias of the real mount point.
	mountOptions := []string{"bind"}
	if req.GetReadonly() {
		mountOptions = append(mountOptions, "ro")
	}

	if err := local.Mount(stagingTargetPath, targetPath, "", mountOptions); err != nil {
		glog.Errorf("mount target path error: %v", err)
		return nil, err
	}

	glog.Infof("volume %s successfully published to %s", volumeID, targetPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	glog.Infof("using NodeUnpublishVolume: %#v, %#v", ctx, req)
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	if err := local.Unmount(targetPath); err != nil {
		glog.Errorf("unmount target path error: %v", err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.Infof("Volume %s has been unmounted.", volumeID)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (ns *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	// currently there is a single NodeServer capability according to the spec
	glog.Infof("using NodeGetCapabilities: %#v, %#v", ctx, req)
	var caps []*csi.NodeServiceCapability
	for _, cap := range nodeCaps {
		c := &csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}

func (ns *NodeServer) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	glog.Infof("using NodeGetInfo: %#v, %#v", ctx, req)

	return &csi.NodeGetInfoResponse{
		NodeId: ns.driver.nodeID,
	}, nil
}

// NodeExpandVolume unimplemented
func (ns *NodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	glog.Infof("using NodeExpandVolume: %#v, %#v", ctx, req)
	volSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	volumeId := req.GetVolumeId()
	bucket, prefix := volumeIDToBucketPrefix(volumeId)
	mounterBucket := &mounter.Bucket{
		Name:     bucket,
		Endpoint: *ns.driverInfo.MounterInfo.Endpoint,
		Prefix:   prefix,
		Capacity: uint64(volSizeBytes),
		Secrets:  req.GetSecrets(),
	}

	nodeDriver, err := NewNodeDriver(volumeId, ns.driverInfo.MounterInfo)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if err := nodeDriver.Quota(mounterBucket); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodeExpandVolumeResponse{}, nil
}

func (d *NodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	glog.Info("called with args", "args", req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	volumePath := req.GetVolumePath()
	if len(volumePath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume path not provided")
	}

	var exists bool

	err := doWithTimeout(ctx, defaultCheckTimeout, func() (err error) {
		exists, err = local.PathExists(volumePath)
		return
	})
	if err == nil {
		if !exists {
			glog.Infof("Volume path %s not exists", volumePath)
			return nil, status.Error(codes.NotFound, "Volume path not exists")
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultCheckTimeout)
		defer cancel()
		if err := local.WaitMount(ctx, volumePath); err != nil {
			glog.Infof("Check volume path is mountpoint failed, path=%s, error=%s", volumePath, err)
			return nil, status.Errorf(codes.Internal, "Check volume path is mountpoint failed: %s", err)
		}
	} else {
		glog.Infof("Check volume path %s, err: %s", volumePath, err)
		return nil, status.Errorf(codes.Internal, "Check volume path, err: %s", err)
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(volumePath, &stat); err != nil {
		glog.Errorf("Check volume path %s, err: %s", volumePath, err)
	}
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: stat.Bsize * int64(stat.Bfree),
				Total:     stat.Bsize * int64(stat.Blocks),
				Used:      stat.Bsize*int64(stat.Blocks) - stat.Bsize*int64(stat.Bfree),
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: int64(stat.Ffree),
				Total:     int64(stat.Files),
				Used:      int64(stat.Files - stat.Ffree),
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}
