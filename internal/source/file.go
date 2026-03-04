package source

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type FileSource struct {
	paths     []string
	fromStart bool // true → read from BOF (query mode); false → tail from EOF (watch mode)
	entries   chan LogEntry
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// NewFileSource creates a FileSource that tails from EOF — for live watch/tail commands.
func NewFileSource(patterns []string) (*FileSource, error) {
	return newFileSource(patterns, false)
}

// NewFileSourceFromStart creates a FileSource that reads from the beginning of each file —
// for query commands that need to scan historical log data.
func NewFileSourceFromStart(patterns []string) (*FileSource, error) {
	return newFileSource(patterns, true)
}

func newFileSource(patterns []string, fromStart bool) (*FileSource, error) {
	var paths []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("no files matched pattern %q", pattern)
		}
		paths = append(paths, matches...)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no files found")
	}
	return &FileSource{
		paths:     paths,
		fromStart: fromStart,
		entries:   make(chan LogEntry, 512),
	}, nil
}

func (f *FileSource) Name() string { return "file" }

func (f *FileSource) Stream(ctx context.Context) (<-chan LogEntry, error) {
	ctx, f.cancel = context.WithCancel(ctx)

	for _, path := range f.paths {
		f.wg.Add(1)
		go f.tailFile(ctx, path)
	}

	go func() {
		f.wg.Wait()
		close(f.entries)
	}()

	return f.entries, nil
}

func (f *FileSource) Close() error {
	if f.cancel != nil {
		f.cancel()
	}
	f.wg.Wait()
	return nil
}

func (f *FileSource) tailFile(ctx context.Context, path string) {
	defer f.wg.Done()

	service := fileServiceName(path)

	if f.fromStart {
		f.readFileFromStart(ctx, path, service)
		return
	}

	file, offset, inode, err := openAtEnd(path)
	if err != nil {
		f.sendEntry(ctx, errorEntry(service, fmt.Sprintf("open %s: %v", path, err)))
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	pollTicker := time.NewTicker(200 * time.Millisecond)
	rotateTicker := time.NewTicker(2 * time.Second)
	defer pollTicker.Stop()
	defer rotateTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-pollTicker.C:
			for {
				line, err := reader.ReadString('\n')
				if len(line) > 0 {
					trimmed := strings.TrimRight(line, "\n\r")
					entry := LogEntry{
						Timestamp: time.Now(),
						Service:   service,
						Fields:    map[string]any{},
						Raw:       trimmed,
					}
					entry = enrichEntry(entry, trimmed)
					f.sendEntry(ctx, entry)
					offset += int64(len(line))
				}
				if err == io.EOF {
					break
				}
				if err != nil {
					f.sendEntry(ctx, errorEntry(service, fmt.Sprintf("read error: %v", err)))
					return
				}
			}

		case <-rotateTicker.C:
			newInode, newSize, err := statFile(path)
			if err != nil {
				continue
			}
			if newInode != inode || newSize < offset {
				file.Close()
				file, offset, inode, err = openAtEnd(path)
				if err != nil {
					f.sendEntry(ctx, errorEntry(service, fmt.Sprintf("reopen after rotate: %v", err)))
					return
				}
				reader.Reset(file)
				f.sendEntry(ctx, LogEntry{
					Timestamp: time.Now(),
					Service:   service,
					Level:     LevelInfo,
					Message:   fmt.Sprintf("log rotated, following: %s", path),
					Fields:    map[string]any{"event": "rotation"},
				})
			}
		}
	}
}

// readFileFromStart reads a file from the beginning to EOF once, emitting every line.
// Used in query mode — it does not poll or watch for rotation; it simply drains the file.
func (f *FileSource) readFileFromStart(ctx context.Context, path, service string) {
	file, err := os.Open(path)
	if err != nil {
		f.sendEntry(ctx, errorEntry(service, fmt.Sprintf("open %s: %v", path, err)))
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		entry := LogEntry{
			Timestamp: time.Now(),
			Service:   service,
			Fields:    map[string]any{},
			Raw:       line,
		}
		entry = enrichEntry(entry, line)
		f.sendEntry(ctx, entry)
	}
}

func (f *FileSource) sendEntry(ctx context.Context, e LogEntry) {
	select {
	case f.entries <- e:
	case <-ctx.Done():
	}
}

func openAtEnd(path string) (*os.File, int64, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	offset, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		file.Close()
		return nil, 0, 0, err
	}
	inode, _, err := statFile(path)
	if err != nil {
		file.Close()
		return nil, 0, 0, err
	}
	return file, offset, inode, nil
}

func fileServiceName(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}
