package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/drycc/storage/csi/driver"
)

var version = "v1.3.1"

func main() {
	driverInfo := driver.DriverInfo{Version: &version}
	driverInfo.NodeId = flag.String("node-id", "", "the k8s node id")
	driverInfo.Endpoint = flag.String("endpoint", "unix:///tmp/csi.sock", "CSI endpoint to accept gRPC calls")
	driverInfo.Components = flag.String("components", "controller,node", "components to run, by default both controller and node")
	driverInfo.DriverName = flag.String("driver-name", "storage.drycc.cc", "name is the prefix of all objects in data storage")
	driverInfo.HealthPort = flag.Int("health-port", 9808, "health check port")

	//mounter info
	mounterInfo := driver.MounterInfo{NodeId: driverInfo.NodeId}
	mounterInfo.Name = flag.String("mounter", "seaweedfs", "backend implementation of mounter")
	mounterInfo.Exefile = flag.String("mounter-exefile", "csi_mounter", "the executable binary file of csi mounter")
	mounterInfo.Timeout = flag.Duration("mounter-timeout", 120*time.Second, "timeout time for calling api")
	mounterInfo.Endpoint = flag.String("mounter-endpoint", "127.0.0.1:8888", "the endpoint of csi mounter")

	mounterInfo.Image = flag.String("mounter-image", "", "the container image of cis mounter")
	mounterInfo.ImagePullPolicy = flag.String("mounter-image-pull-policy", "", "the container image pull policy for csi mounter")
	mounterInfo.Namespace = flag.String("mounter-namespace", "", "the k8s namespace")
	mounterInfo.ServiceAccount = flag.String("mounter-service-account", "drycc-storage-csi", "the k8s service account")

	driverInfo.MounterInfo = &mounterInfo
	flag.Parse()

	if *driverInfo.NodeId == "" {
		log.Fatal("node-id is required")
	}

	driver, err := driver.New(&driverInfo)
	if err != nil {
		log.Fatal(err)
	}
	driver.Serve()
	os.Exit(0)
}
