package driver

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
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

func logGRPC(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	glog.Infof("grpc call: %s", info.FullMethod)
	glog.Infof("grpc request: %s", protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		glog.Errorf("grpc call %s requests %s error: %v", info.FullMethod, protosanitizer.StripSecrets(req), err)
	} else {
		glog.Infof("grpc response: %s", protosanitizer.StripSecrets(resp))
	}
	return resp, err
}

func ParseEndpoint(ep string) (string, string, error) {
	if strings.HasPrefix(strings.ToLower(ep), "unix://") || strings.HasPrefix(strings.ToLower(ep), "tcp://") {
		s := strings.SplitN(ep, "://", 2)
		if s[1] != "" {
			if s[0] == "unix" {
				if !strings.HasPrefix(s[1], "/") {
					s[1] = "/" + s[1]
				}
			}
			return s[0], s[1], nil
		}
	}
	return "", "", fmt.Errorf("invalid endpoint: %v", ep)
}

func NewGrpcServer(endpoint string) (net.Listener, *grpc.Server) {
	proto, addr, err := ParseEndpoint(endpoint)
	if err != nil {
		glog.Fatal(err.Error())
	}

	if proto == "unix" {
		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			glog.Fatalf("failed to remove %s, error: %s", addr, err.Error())
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		glog.Fatalf("failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logGRPC),
	}
	return listener, grpc.NewServer(opts...)
}
