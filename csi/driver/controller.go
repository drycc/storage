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
	"fmt"
	"path"

	"github.com/drycc/storage/csi/client"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

const (
	bucketKey            = "bucket"
	maxAvailableCapacity = 1 * 1024 * 1024 * 1024 * 1024 * 1024
)

var (
	controllerCaps = []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
	}
)

type ControllerServer struct {
	csi.UnimplementedControllerServer
	driver     *CSIDriver
	driverInfo *DriverInfo
}

func (cs *ControllerServer) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	glog.Infof("using CreateVolume: %#v, %#v", ctx, req)
	params := req.GetParameters()
	capacityBytes := int64(req.GetCapacityRange().GetRequiredBytes())
	volumeID := sanitizeVolumeID(req.GetName())
	bucketName := volumeID
	prefix := ""

	// check if bucket name is overridden
	if params[bucketKey] != "" {
		bucketName = params[bucketKey]
		prefix = volumeID
		volumeID = path.Join(bucketName, prefix)
	}

	if err := cs.driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.Infof("invalid create volume req: %v", req)
		return nil, err
	}

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "name missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume Capabilities missing in request")
	}

	glog.Infof("got a request to create volume %s", volumeID)
	client, err := client.NewClientFromSecret(req.GetSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}

	exists, err := client.BucketExists(bucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check if bucket %s exists: %v", volumeID, err)
	}

	if !exists {
		if err = client.CreateBucket(bucketName); err != nil {
			return nil, fmt.Errorf("failed to create bucket %s: %v", bucketName, err)
		}
	}

	if err = client.CreatePrefix(bucketName, prefix); err != nil {
		return nil, fmt.Errorf("failed to create prefix %s: %v", prefix, err)
	}

	glog.Infof("create volume %s", volumeID)
	// DeleteVolume lacks VolumeContext, but publish&unpublish requests have it,
	// so we don't need to store additional metadata anywhere
	context := make(map[string]string)
	for k, v := range params {
		context[k] = v
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      volumeID,
			CapacityBytes: capacityBytes,
			VolumeContext: context,
		},
	}, nil
}

func (cs *ControllerServer) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	glog.Infof("using DeleteVolume: %#v, %#v", ctx, req)
	volumeID := req.GetVolumeId()
	bucketName, prefix := volumeIDToBucketPrefix(volumeID)

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume ID missing in request")
	}

	if err := cs.driver.ValidateControllerServiceRequest(csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME); err != nil {
		glog.Infof("invalid delete volume req: %v", req)
		return nil, err
	}
	glog.Infof("deleting volume %s", volumeID)

	client, err := client.NewClientFromSecret(req.GetSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}

	var deleteErr error
	if prefix == "" {
		// prefix is empty, we delete the whole bucket
		if err := client.RemoveBucket(bucketName); err != nil && err.Error() != "The specified bucket does not exist" {
			deleteErr = err
		}
		glog.Infof("bucket %s removed", bucketName)
	} else {
		if err := client.RemovePrefix(bucketName, prefix); err != nil {
			deleteErr = fmt.Errorf("unable to remove prefix: %w", err)
		}
		glog.Infof("prefix %s removed", prefix)
	}

	if deleteErr != nil {
		return nil, deleteErr
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (cs *ControllerServer) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	glog.Infof("using ValidateVolumeCapabilities: %#v, %#v", ctx, req)
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if req.GetVolumeCapabilities() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capabilities missing in request")
	}
	bucketName, _ := volumeIDToBucketPrefix(req.GetVolumeId())

	client, err := client.NewClientFromSecret(req.GetSecrets())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize S3 client: %s", err)
	}
	exists, err := client.BucketExists(bucketName)
	if err != nil {
		return nil, err
	}

	if !exists {
		// return an error if the bucket of the requested volume does not exist
		return nil, status.Error(codes.NotFound, fmt.Sprintf("bucket of volume with id %s does not exist", req.GetVolumeId()))
	}

	supportedAccessMode := &csi.VolumeCapability_AccessMode{
		Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}

	for _, capability := range req.VolumeCapabilities {
		if capability.GetAccessMode().GetMode() != supportedAccessMode.GetMode() {
			return &csi.ValidateVolumeCapabilitiesResponse{Message: "Only single node writer is supported"}, nil

		}
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessMode: supportedAccessMode,
				},
			},
		},
	}, nil
}

func (cs *ControllerServer) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	glog.Infof("ControllerGetCapabilities: %#v, %#v", ctx, req)
	var caps []*csi.ControllerServiceCapability
	for _, cap := range controllerCaps {
		c := &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		}
		caps = append(caps, c)
	}
	return &csi.ControllerGetCapabilitiesResponse{Capabilities: caps}, nil
}
