package fanin

import (
	"context"
	"sync"

	"github.com/OpenCortex-Labs/logr/internal/source"
)

// Merge streams all sources concurrently into a single LogEntry channel.
// The returned channel closes when all sources are exhausted or ctx is cancelled.
func Merge(ctx context.Context, sources []source.Source) <-chan source.LogEntry {
	merged := make(chan source.LogEntry, 512)
	var wg sync.WaitGroup

	for _, src := range sources {
		wg.Add(1)
		go func(s source.Source) {
			defer wg.Done()
			ch, err := s.Stream(ctx)
			if err != nil {
				return
			}
			for entry := range ch {
				select {
				case merged <- entry:
				case <-ctx.Done():
					return
				}
			}
		}(src)
	}

	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged
}
