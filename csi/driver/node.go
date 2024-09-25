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
	"time"

	"github.com/drycc/storage/csi/k8s"
	"github.com/drycc/storage/csi/provider"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"k8s.io/klog/v2"
	"k8s.io/mount-utils"

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
	}
)

type NodeServer struct {
	csi.UnimplementedNodeServer
	provider provider.Provider
	driver   *CSIDriver
}

func (ns *NodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	glog.V(5).Infof("using NodePublishVolume: %#v, %#v", ctx, req)
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()
	bucket, prefix := volumeIDToBucketPrefix(volumeID)

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	notMnt, err := ns.provider.NodeCheckMountVolume(targetPath)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
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
	capacity, err := k8s.GetVolumeCapacity(volumeID)
	if err != nil {
		glog.Infof("orchestration system is not compatible with the k8s api, error is: %s", err)
	}
	mountPoint := &provider.MountPoint{Path: targetPath, Options: options, Readonly: req.GetReadonly()}
	mountBucket := &provider.MountBucket{Name: bucket, Prefix: prefix, Capacity: uint64(capacity), Secrets: req.GetSecrets()}
	if err := ns.provider.NodeMountVolume(mountPoint, mountBucket); err != nil {
		return nil, err
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *NodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	glog.V(5).Infof("using NodeUnpublishVolume: %#v, %#v", ctx, req)
	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	if err := ns.provider.NodeUmountVolume(targetPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	glog.V(4).Infof("Volume %s has been unmounted.", volumeID)
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (ns *NodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	// currently there is a single NodeServer capability according to the spec
	glog.V(5).Infof("using NodeGetCapabilities: %#v, %#v", ctx, req)
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
	glog.V(5).Infof("using NodeGetInfo: %#v, %#v", ctx, req)

	return &csi.NodeGetInfoResponse{
		NodeId: ns.driver.nodeID,
	}, nil
}

// NodeExpandVolume unimplemented
func (ns *NodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	glog.V(5).Infof("using NodeExpandVolume: %#v, %#v", ctx, req)
	volSizeBytes := req.GetCapacityRange().GetRequiredBytes()
	volumeID := req.GetVolumeId()
	bucket, prefix := volumeIDToBucketPrefix(volumeID)
	mountBucket := &provider.MountBucket{Name: bucket, Prefix: prefix, Capacity: uint64(volSizeBytes), Secrets: req.GetSecrets()}
	if err := ns.provider.NodeExpandVolume(mountBucket); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &csi.NodeExpandVolumeResponse{}, nil
}

func (d *NodeServer) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	log := klog.NewKlogr().WithName("NodeGetVolumeStats")
	log.V(1).Info("called with args", "args", req)

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
		exists, err = mount.PathExists(volumePath)
		return
	})
	if err == nil {
		if !exists {
			log.Info("Volume path not exists", "volumePath", volumePath)
			return nil, status.Error(codes.NotFound, "Volume path not exists")
		}
		if err := d.provider.NodeWaitMountVolume(volumePath, defaultCheckTimeout); err != nil {
			log.Info("Check volume path is mountpoint failed", "volumePath", volumePath, "error", err)
			return nil, status.Errorf(codes.Internal, "Check volume path is mountpoint failed: %s", err)
		}
	} else {
		log.Info("Check volume path %s, err: %s", "volumePath", volumePath, "error", err)
		return nil, status.Errorf(codes.Internal, "Check volume path, err: %s", err)
	}

	totalSize, freeSize, totalInodes, freeInodes := getDiskUsage(volumePath)
	usedSize := int64(totalSize) - int64(freeSize)
	usedInodes := int64(totalInodes) - int64(freeInodes)

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: int64(freeSize),
				Total:     int64(totalSize),
				Used:      usedSize,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: int64(freeInodes),
				Total:     int64(totalInodes),
				Used:      usedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}
