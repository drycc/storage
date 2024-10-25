package driver

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
)

type MounterInfo struct {
	Name     *string
	Exefile  *string
	Timeout  *time.Duration
	Endpoint *string
	// pod used
	NodeId          *string
	Namespace       *string
	Image           *string
	ImagePullPolicy *string
	ServiceAccount  *string
}

type DriverInfo struct {
	NodeId      *string
	DriverName  *string
	Endpoint    *string
	Components  *string
	Version     *string
	HealthPort  *int
	MounterInfo *MounterInfo
}

type DriveService struct {
	driver     *CSIDriver
	driverInfo *DriverInfo

	ids *IdentityServer
	ns  *NodeServer
	cs  *ControllerServer
}

// New initializes the driver
func New(driverInfo *DriverInfo) (*DriveService, error) {
	d := NewCSIDriver(*driverInfo.DriverName, *driverInfo.Version, *driverInfo.NodeId)
	if d == nil {
		glog.Fatalln("failed to initialize CSI Driver.")
	}
	service := &DriveService{driver: d, driverInfo: driverInfo}
	if err := service.initComponents(); err != nil {
		return nil, err
	}
	return service, nil
}

func (service *DriveService) startHealthz(port int) {
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("ok\r\n"))
		}))
		server := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
		server.ListenAndServe()
	}()
}

func (service *DriveService) initComponents() error {
	service.ids = &IdentityServer{driver: service.driver}
	for _, component := range strings.Split(*service.driverInfo.Components, ",") {
		switch component {
		case "node":
			service.ns = &NodeServer{driver: service.driver, driverInfo: service.driverInfo}
		case "controller":
			service.cs = &ControllerServer{driver: service.driver, driverInfo: service.driverInfo}
		default:
			return fmt.Errorf("unknown component: %s", component)
		}
	}
	return nil
}

func (service *DriveService) Serve() {
	glog.Infof("driver: %v ", service.driver.name)
	glog.Infof("version: %v ", service.driver.version)
	glog.Infof("components: %v ", *service.driverInfo.Components)

	// Initialize default library driver
	service.driver.AddControllerServiceCapabilities([]csi.ControllerServiceCapability_RPC_Type{csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME})
	service.driver.AddVolumeCapabilityAccessModes([]csi.VolumeCapability_AccessMode_Mode{csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER})

	// Create GRPC servers
	s := NewNonBlockingGRPCServer()
	s.Start(*service.driverInfo.Endpoint, service.ids, service.cs, service.ns)
	service.startHealthz(*service.driverInfo.HealthPort)
	s.Wait()
}
