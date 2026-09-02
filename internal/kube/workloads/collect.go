package workloads

import (
	"context"
	"sort"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aronk11/kubeui/internal/domain/application"
)

// group runs several listings concurrently and collects their results.
//
// It exists because both passes over a scope — the dashboard's and the
// diagnosis evidence — have the same shape and the same two rules: one round
// trip's latency rather than one per kind, and a kind that cannot be read is a
// gap rather than a failure. Only a scope where *nothing* could be read is an
// error, because "no applications" must never be what an unreachable API
// server looks like.
type group struct {
	mu        sync.Mutex
	wg        sync.WaitGroup
	gaps      []application.Gap
	firstErr  error
	attempted int
	truncated bool
}

// run schedules one kind's listing.
func (g *group) run(kind string, list func() (bool, error)) {
	g.attempted++
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		truncated, err := list()

		g.mu.Lock()
		defer g.mu.Unlock()
		if err != nil {
			g.gaps = append(g.gaps, application.Gap{Kind: kind, Reason: gapReason(err)})
			if g.firstErr == nil {
				g.firstErr = err
			}
			return
		}
		g.truncated = g.truncated || truncated
	}()
}

// collect appends to the caller's slices under the group's lock, so the
// listings can write their results without a lock of their own.
func (g *group) collect(f func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f()
}

// wait blocks until every listing has answered.
func (g *group) wait() (gaps []application.Gap, truncated bool, err error) {
	g.wg.Wait()
	if len(g.gaps) == g.attempted && g.firstErr != nil {
		return nil, false, g.firstErr
	}
	sort.Slice(g.gaps, func(i, j int) bool { return g.gaps[i].Kind < g.gaps[j].Kind })
	return g.gaps, g.truncated, nil
}

// page walks a resource's pages until the server runs out of them or the budget
// does, and reports whether anything was left behind.
func page(
	ctx context.Context,
	opts Options,
	fetch func(context.Context, metav1.ListOptions) (string, error),
) (bool, error) {
	list := metav1.ListOptions{Limit: opts.pageSize()}
	for i := 0; i < opts.maxPages(); i++ {
		next, err := fetch(ctx, list)
		if err != nil {
			return false, err
		}
		if next == "" {
			return false, nil
		}
		list.Continue = next
	}
	return true, nil
}
