package driver

import (
	"context"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/drycc/storage/csi/k8s"
	"github.com/drycc/storage/csi/local"
	"github.com/drycc/storage/csi/mounter"
	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const podVolumeName = "target-dir"
const podContainerName = "mounter"

type NodeDriver struct {
	volumeId    string
	K8sClient   *k8s.K8sClient
	mounterInfo *MounterInfo
}

type MounterOptions struct {
	Mounter string          `json:"mounter"`
	Bucket  *mounter.Bucket `json:"bucket,omitempty"`
	Target  string          `json:"target,omitempty"`
	Options []string        `json:"options,omitempty"`
}

func NewNodeDriver(volumeId string, mounterInfo *MounterInfo) (*NodeDriver, error) {
	client, err := k8s.NewClient()
	if err != nil {
		return nil, err
	}
	return &NodeDriver{
		volumeId:    volumeId,
		K8sClient:   client,
		mounterInfo: mounterInfo,
	}, nil
}

func (n *NodeDriver) Mount(bucket *mounter.Bucket, target string, options []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), *n.mounterInfo.Timeout)
	defer cancel()
	newPod, err := n.newMountPodTemplate(bucket, target, options)
	if err != nil {
		return err
	}

	oldPod, _ := n.K8sClient.GetPod(ctx, newPod.Name, newPod.Namespace)
	if oldPod != nil {
		glog.Infof("pod %s already exists, preparing to delete")
		err = n.K8sClient.DeletePod(ctx, oldPod.Name, oldPod.Namespace)
		if err != nil {
			return err
		}
	}

	if _, err := n.K8sClient.CreatePod(ctx, newPod); err != nil {
		return err
	}

	return local.WaitMount(ctx, target)
}

func (n *NodeDriver) Quota(bucket *mounter.Bucket) error {
	ctx, cancel := context.WithTimeout(context.Background(), *n.mounterInfo.Timeout)
	defer cancel()
	options, err := n.newCmdMounterOptions(bucket, "", nil)
	if err != nil {
		return err
	}
	glog.Infof("call quota options=%v", options)
	stdout, stderr, err := n.K8sClient.ExecInPod(
		ctx,
		n.getPodName(),
		*n.mounterInfo.Namespace,
		podContainerName,
		strings.Join([]string{
			*n.mounterInfo.Exefile,
			"quota",
			"--options",
			options,
		}, " "),
	)
	glog.Infof("call quota result: stdout=%s, stderr=%s, error=%v", stdout, stderr, err)
	if err != nil {
		return err
	}
	if stderr != "" {
		return errors.New(stderr)
	}
	return nil
}

func (n *NodeDriver) Unmount(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), *n.mounterInfo.Timeout)
	defer cancel()
	options, err := n.newCmdMounterOptions(nil, target, nil)
	if err != nil {
		return err
	}
	glog.Infof("call unmount target=%s", target)
	stdout, stderr, err := n.K8sClient.ExecInPod(
		ctx,
		n.getPodName(),
		*n.mounterInfo.Namespace,
		podContainerName,
		strings.Join([]string{
			*n.mounterInfo.Exefile,
			"unmount",
			"--options",
			options,
		}, " "),
	)
	glog.Infof("call unmount result: stdout=%s, stderr=%s, error=%v", stdout, stderr, err)
	if err != nil {
		return err
	}
	if stderr != "" {
		return errors.New(stderr)
	}
	glog.Infof("unmount target path %s success", target)
	return n.K8sClient.DeletePod(ctx, n.getPodName(), *n.mounterInfo.Namespace)
}

func (n *NodeDriver) getPodName() string {
	h := sha1.New()
	io.WriteString(h, fmt.Sprintf("%s-%s-%s", *n.mounterInfo.Name, *n.mounterInfo.NodeId, n.volumeId))
	return fmt.Sprintf("drycc-storage-mounter-%s", strings.ToLower(base32.StdEncoding.EncodeToString(h.Sum(nil))))
}

func (n *NodeDriver) newMountPodContainers(bucket *mounter.Bucket, target string, options []string) ([]corev1.Container, error) {
	mountPath := path.Join(target, "../../")
	mounterOptions, err := n.newCmdMounterOptions(bucket, target, options)
	if err != nil {
		return nil, err
	}
	privileged := true
	args := []string{*n.mounterInfo.Exefile, "mount", "--options", mounterOptions}
	mountPropagation := corev1.MountPropagationBidirectional
	glog.Infof("create new mount container, args: %v, target: %s", args, target)

	return []corev1.Container{{
		Name:            podContainerName,
		Args:            args,
		Image:           *n.mounterInfo.Image,
		ImagePullPolicy: corev1.PullPolicy(*n.mounterInfo.ImagePullPolicy),
		SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
		VolumeMounts: []corev1.VolumeMount{{
			Name: podVolumeName, MountPath: mountPath, MountPropagation: &mountPropagation,
		}},
	}}, nil
}

func (n *NodeDriver) newMountPodTemplate(bucket *mounter.Bucket, target string, options []string) (*corev1.Pod, error) {
	var zero int64 = 0
	hostPath := path.Join(target, "../../")
	hostPathType := corev1.HostPathDirectoryOrCreate
	preemptPriority := corev1.PreemptLowerPriority
	containers, err := n.newMountPodContainers(bucket, target, options)
	if err != nil {
		return nil, err
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      n.getPodName(),
			Namespace: *n.mounterInfo.Namespace,
			Labels: map[string]string{
				"component": "drycc-storage",
				"app":       "drycc-storage-csi-mounter",
				"volume":    n.volumeId,
				"nodeid":    *n.mounterInfo.NodeId,
			},
			Annotations: make(map[string]string),
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			NodeName:           *n.mounterInfo.NodeId,
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: *n.mounterInfo.ServiceAccount,
			PreemptionPolicy:   &preemptPriority,
			SecurityContext:    &corev1.PodSecurityContext{RunAsUser: &zero, FSGroup: &zero, RunAsGroup: &zero},
			Volumes: []corev1.Volume{{
				Name:         podVolumeName,
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: hostPath, Type: &hostPathType}},
			}},
		},
	}, nil
}

func (n *NodeDriver) newCmdMounterOptions(bucket *mounter.Bucket, target string, options []string) (string, error) {
	mounterOptions := MounterOptions{
		Mounter: *n.mounterInfo.Name,
		Bucket:  bucket,
		Target:  target,
		Options: options,
	}
	data, err := json.Marshal(mounterOptions)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
