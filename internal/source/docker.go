package source

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// DockerSource streams logs from one or more Docker containers.
type DockerSource struct {
	client   *client.Client
	services []string
	entries  chan LogEntry
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	tailing  map[string]bool
}

func NewDockerSource(services []string) (*DockerSource, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &DockerSource{
		client:   cli,
		services: services,
		entries:  make(chan LogEntry, 512),
		tailing:  map[string]bool{},
	}, nil
}

func (d *DockerSource) Name() string { return "docker" }

func (d *DockerSource) Stream(ctx context.Context) (<-chan LogEntry, error) {
	ctx, d.cancel = context.WithCancel(ctx)

	containers, err := d.resolveContainers(ctx)
	if err != nil {
		return nil, err
	}
	if len(containers) == 0 {
		return nil, fmt.Errorf("no running containers found (services: %v)", d.services)
	}

	for _, c := range containers {
		d.wg.Add(1)
		go d.tailContainer(ctx, c)
	}

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.WatchNewContainers(ctx)
	}()

	go func() {
		d.wg.Wait()
		close(d.entries)
	}()

	return d.entries, nil
}

func (d *DockerSource) Close() error {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	return d.client.Close()
}

func (d *DockerSource) resolveContainers(ctx context.Context) ([]types.Container, error) {
	f := filters.NewArgs()
	f.Add("status", "running")
	for _, svc := range d.services {
		f.Add("label", "com.docker.compose.service="+svc)
	}
	return d.client.ContainerList(ctx, container.ListOptions{Filters: f})
}

func (d *DockerSource) tailContainer(ctx context.Context, c types.Container) {
	defer d.wg.Done()
	serviceName := containerServiceName(c)

	opts := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "50",
		Timestamps: true,
	}

	rc, err := d.client.ContainerLogs(ctx, c.ID, opts)
	if err != nil {
		return
	}
	defer rc.Close()

	scanner := bufio.NewScanner(newDockerLogReader(rc))
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
			Service:   serviceName,
			Fields:    map[string]any{},
			Raw:       line,
		}
		entry = enrichEntry(entry, content)
		select {
		case d.entries <- entry:
		case <-ctx.Done():
			return
		}
	}
}

func (d *DockerSource) WatchNewContainers(ctx context.Context) {
	f := filters.NewArgs()
	f.Add("type", "container")
	f.Add("event", "start")

	eventsCh, errCh := d.client.Events(ctx, events.ListOptions{Filters: f})
	for {
		select {
		case <-ctx.Done():
			return
		case <-errCh:
			return
		case event := <-eventsCh:
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				d.tailContainerByID(ctx, event)
			}()
		}
	}
}

func (d *DockerSource) tailContainerByID(ctx context.Context, event events.Message) {
	time.Sleep(500 * time.Millisecond)
	if ctx.Err() != nil {
		return
	}
	containers, err := d.client.ContainerList(ctx, container.ListOptions{Filters: filters.NewArgs()})
	if err != nil {
		return
	}
	for _, c := range containers {
		if c.ID == event.Actor.ID && d.matchesFilter(c) {
			if ctx.Err() != nil {
				return
			}
			d.mu.Lock()
			if !d.tailing[c.ID] {
				d.tailing[c.ID] = true
				d.wg.Add(1)
				d.mu.Unlock()
				go d.tailContainer(ctx, c)
			} else {
				d.mu.Unlock()
			}
			return
		}
	}
}

func (d *DockerSource) matchesFilter(c types.Container) bool {
	if len(d.services) == 0 {
		return true
	}
	svc := containerServiceName(c)
	for _, s := range d.services {
		if s == svc {
			return true
		}
	}
	return false
}

func containerServiceName(c types.Container) string {
	if svc, ok := c.Labels["com.docker.compose.service"]; ok {
		return svc
	}
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:12]
}

func newDockerLogReader(rc io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		_, err := stdcopy.StdCopy(pw, pw, rc)
		pw.CloseWithError(err)
	}()
	return pr
}
