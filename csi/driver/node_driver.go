package driver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/drycc/storage/csi/k8s"
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
	pod, err := n.newMountPodTemplate(bucket, target, options)
	if err != nil {
		return err
	}

	pod, err = n.K8sClient.CreatePod(ctx, pod)
	if err != nil {
		return err
	}
	running, err := n.K8sClient.WaitPodRunning(ctx, pod.Name, pod.Namespace)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("wait pod running error: podName=%s, namespace=%s", pod.Name, pod.Namespace)
	}

	return n.Quota(bucket)
}

func (n *NodeDriver) Quota(bucket *mounter.Bucket) error {
	ctx, cancel := context.WithTimeout(context.Background(), *n.mounterInfo.Timeout)
	defer cancel()
	options, err := n.newCmdMounterOptions(bucket, "", nil)
	if err != nil {
		return err
	}

	_, stderr, err := n.K8sClient.ExecInPod(
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
	_, stderr, err := n.K8sClient.ExecInPod(
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
	volumeId := sanitizeVolumeID(n.volumeId)
	return fmt.Sprintf("mounter-%s-%s", *n.mounterInfo.Name, volumeId)
}

func (n *NodeDriver) newMountPodContainers(bucket *mounter.Bucket, target string, options []string) ([]corev1.Container, error) {
	mounterOptions, err := n.newCmdMounterOptions(bucket, target, options)
	if err != nil {
		return nil, err
	}
	privileged := true
	mountPropagation := corev1.MountPropagationBidirectional
	return []corev1.Container{{
		Name:            podContainerName,
		Args:            []string{*n.mounterInfo.Exefile, "mount", "--options", mounterOptions},
		Image:           *n.mounterInfo.Image,
		ImagePullPolicy: corev1.PullPolicy(*n.mounterInfo.ImagePullPolicy),
		SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
		VolumeMounts: []corev1.VolumeMount{{
			Name: podVolumeName, MountPath: target, MountPropagation: &mountPropagation,
		}},
	}}, nil
}

func (n *NodeDriver) newMountPodTemplate(bucket *mounter.Bucket, target string, options []string) (*corev1.Pod, error) {
	hostPathType := corev1.HostPathDirectoryOrCreate
	preemptPriority := corev1.PreemptLowerPriority
	containers, err := n.newMountPodContainers(bucket, target, options)
	if err != nil {
		return nil, err
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        n.getPodName(),
			Namespace:   *n.mounterInfo.Namespace,
			Labels:      map[string]string{"component": "drycc-storage", "app": "drycc-storage-csi-node"},
			Annotations: make(map[string]string),
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			NodeName:           *n.mounterInfo.NodeId,
			RestartPolicy:      corev1.RestartPolicyOnFailure,
			ServiceAccountName: *n.mounterInfo.ServiceAccount,
			PreemptionPolicy:   &preemptPriority,
			Volumes: []corev1.Volume{{
				Name:         podVolumeName,
				VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: target, Type: &hostPathType}},
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
