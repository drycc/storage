package k8s

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

type K8sClient struct {
	kubernetes.Interface
	config *rest.Config
}

func NewClient() (*K8sClient, error) {
	//creates the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return &K8sClient{Interface: clientset, config: config}, nil
}

func (k *K8sClient) CreatePod(ctx context.Context, pod *corev1.Pod) (*corev1.Pod, error) {
	if pod == nil {
		glog.Info("Create pod error: pod is nil")
		return nil, nil
	}
	glog.Infof("Create pod %s", pod.Name)
	mntPod, err := k.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		glog.Infof("Can't create pod %s, error: %v", pod.Name, err)
		return nil, err
	}
	return mntPod, nil
}

func (k *K8sClient) GetPod(ctx context.Context, podName, namespace string) (*corev1.Pod, error) {
	glog.Infof("Get pod %s", podName)
	mntPod, err := k.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		glog.Infof("Can't get namespace %s pod %s, error: %v", namespace, podName, err)
		return nil, err
	}
	return mntPod, nil
}

func (k *K8sClient) DeletePod(ctx context.Context, podName, namespace string) error {
	glog.Infof("Delete pod %s", podName)
	return k.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
}

func (k *K8sClient) ExecInPod(ctx context.Context, podName, namespace, container, command string) (string, string, error) {
	cmd := []string{
		"sh",
		"-c",
		command,
	}
	const tty = false
	request := k.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).SubResource("exec").Param("container", container)
	request.VersionedParams(&corev1.PodExecOptions{Command: cmd, Stdin: false, Stdout: true, Stderr: true, TTY: tty}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	exec, err := remotecommand.NewSPDYExecutor(k.config, "POST", request.URL())
	if err != nil {
		return "", "", err
	}
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func (k *K8sClient) GetVolumeSize(volumeId string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if volume, err := k.CoreV1().PersistentVolumes().Get(ctx, volumeId, metav1.GetOptions{}); err != nil {
		return 0, err
	} else {
		storage := volume.Spec.Capacity.Storage()
		capacity, _ := storage.AsInt64()
		return capacity, nil
	}
}
