package source

import (
	"bufio"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type KubeOptions struct {
	Namespace  string
	Labels     string
	PodPrefix  string
	Container  string
	Kubeconfig string
}

type KubeSource struct {
	client    kubernetes.Interface
	namespace string
	selector  labels.Selector
	podPrefix string
	container string
	entries   chan LogEntry
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.Mutex
	tailing   map[string]bool
}

func NewKubeSource(opts KubeOptions) (*KubeSource, error) {
	config, err := loadKubeConfig(opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("k8s client: %w", err)
	}

	selector := labels.Everything()
	if opts.Labels != "" {
		selector, err = labels.Parse(opts.Labels)
		if err != nil {
			return nil, fmt.Errorf("invalid label selector %q: %w", opts.Labels, err)
		}
	}

	ns := opts.Namespace
	if ns == "" {
		ns = "default"
	}

	return &KubeSource{
		client:    clientset,
		namespace: ns,
		selector:  selector,
		podPrefix: opts.PodPrefix,
		container: opts.Container,
		entries:   make(chan LogEntry, 512),
		tailing:   map[string]bool{},
	}, nil
}

func (k *KubeSource) Name() string { return "kubernetes" }

func (k *KubeSource) Stream(ctx context.Context) (<-chan LogEntry, error) {
	ctx, k.cancel = context.WithCancel(ctx)

	pods, err := k.resolvePods(ctx)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running pods found (namespace: %s, selector: %s)",
			k.namespace, k.selector)
	}

	for _, pod := range pods {
		k.startTailingPod(ctx, pod)
	}

	go k.watchNewPods(ctx)

	go func() {
		k.wg.Wait()
		close(k.entries)
	}()

	return k.entries, nil
}

func (k *KubeSource) Close() error {
	if k.cancel != nil {
		k.cancel()
	}
	k.wg.Wait()
	return nil
}

func (k *KubeSource) resolvePods(ctx context.Context) ([]corev1.Pod, error) {
	ns := k.namespace
	if ns == "all" {
		ns = ""
	}

	list, err := k.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{
		LabelSelector: k.selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing pods: %w", err)
	}

	matching := make([]corev1.Pod, 0, len(list.Items))
	for _, pod := range list.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}
		if k.podPrefix != "" && !strings.HasPrefix(pod.Name, k.podPrefix) {
			continue
		}
		matching = append(matching, pod)
	}
	return matching, nil
}

func (k *KubeSource) startTailingPod(ctx context.Context, pod corev1.Pod) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.tailing[pod.Name] {
		return
	}
	k.tailing[pod.Name] = true

	for _, containerName := range containersToTail(pod, k.container) {
		k.wg.Add(1)
		go k.tailContainer(ctx, pod, containerName)
	}
}

func (k *KubeSource) tailContainer(ctx context.Context, pod corev1.Pod, container string) {
	defer k.wg.Done()

	service := podServiceName(pod)
	if container != "" && len(pod.Spec.Containers) > 1 {
		service = service + "/" + container
	}

	tailLines := int64(50)
	req := k.client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container:  container,
		Follow:     true,
		TailLines:  &tailLines,
		Timestamps: true,
	})

	stream, err := req.Stream(ctx)
	if err != nil {
		k.sendEntry(ctx, errorEntry(service, fmt.Sprintf("failed to stream logs: %v", err)))
		return
	}
	defer stream.Close()

	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		ts, content := parseTimestampPrefix(line)
		entry := LogEntry{
			Timestamp: ts,
			Service:   service,
			Fields:    map[string]any{},
			Raw:       line,
		}
		entry = enrichEntry(entry, content)
		k.sendEntry(ctx, entry)
	}

	k.mu.Lock()
	delete(k.tailing, pod.Name)
	k.mu.Unlock()
}

func (k *KubeSource) watchNewPods(ctx context.Context) {
	ns := k.namespace
	if ns == "all" {
		ns = ""
	}

	watcher, err := k.client.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{
		LabelSelector: k.selector.String(),
	})
	if err != nil {
		return
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}
			pod, ok := event.Object.(*corev1.Pod)
			if !ok {
				continue
			}
			if pod.Status.Phase == corev1.PodRunning {
				if k.podPrefix == "" || strings.HasPrefix(pod.Name, k.podPrefix) {
					time.Sleep(500 * time.Millisecond)
					k.startTailingPod(ctx, *pod)
				}
			}
		}
	}
}

func (k *KubeSource) sendEntry(ctx context.Context, e LogEntry) {
	select {
	case k.entries <- e:
	case <-ctx.Done():
	}
}

func containersToTail(pod corev1.Pod, specific string) []string {
	if specific != "" {
		return []string{specific}
	}
	var names []string
	for _, c := range pod.Spec.Containers {
		if !isSidecar(c.Name) {
			names = append(names, c.Name)
		}
	}
	return names
}

func isSidecar(name string) bool {
	for _, s := range []string{"istio-proxy", "linkerd-proxy", "envoy", "fluentd", "datadog-agent"} {
		if name == s {
			return true
		}
	}
	return false
}

func podServiceName(pod corev1.Pod) string {
	for _, label := range []string{"app", "app.kubernetes.io/name", "name"} {
		if v, ok := pod.Labels[label]; ok {
			return v
		}
	}
	parts := strings.Split(pod.Name, "-")
	if len(parts) > 2 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return pod.Name
}

func loadKubeConfig(explicit string) (*rest.Config, error) {
	if explicit != "" {
		return clientcmd.BuildConfigFromFlags("", explicit)
	}
	if config, err := rest.InClusterConfig(); err == nil {
		return config, nil
	}
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
