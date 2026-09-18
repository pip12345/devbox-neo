package migration

import (
	"context"
	"fmt"
	"io"
	"maps"
	"sort"
	"time"
)

// Discovery records copy candidates without walking or reading their payloads.
// These requests are invocation-local: resume reconstructs them from metadata
// and compares the resulting selected snapshot with the durable journal.
type scanRequest struct {
	item, source, relative string
	tree                   bool
	skip                   map[string]bool
}

func snapshotSelected(ctx context.Context, discovery *Inventory, excluded map[string]string, progress *preparationProgress) (*Inventory, error) {
	v := *discovery
	v.Items = append([]Item(nil), discovery.Items...)
	v.Files = nil
	v.SourceHashes = maps.Clone(discovery.SourceHashes)
	v.Directories = maps.Clone(discovery.Directories)
	v.progress = progress
	for i := range v.Items {
		v.Items[i].Bytes = 0
		v.Items[i].Scanned = excluded[v.Items[i].Key] == ""
	}
	for _, file := range discovery.Files {
		if excluded[file.Item] == "" {
			v.Files = append(v.Files, file)
			v.item(file.Item).Bytes += file.Size
		}
	}
	progress.begin("Snapshotting selected data")
	for _, request := range discovery.scans {
		if excluded[request.item] != "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress.item(request.item)
		var err error
		if request.tree {
			err = v.tree(ctx, request.item, request.source, request.relative, request.skip)
		} else {
			err = v.file(ctx, request.item, request.source, request.relative)
		}
		if err != nil {
			return nil, fmt.Errorf("cannot snapshot %s: %w", request.item, err)
		}
	}
	sort.Slice(v.Files, func(i, j int) bool { return v.Files[i].Relative < v.Files[j].Relative })
	progress.finish()
	return &v, nil
}

// Progress is throttled across files and within large reads. It contains only
// public paths/counters and never enters the inventory or journal fingerprint.
type preparationProgress struct {
	out            io.Writer
	phase, current string
	files, bytes   int64
	last           time.Time
}

func newPreparationProgress(out io.Writer) *preparationProgress {
	if out == nil {
		return nil
	}
	return &preparationProgress{out: out}
}
func (p *preparationProgress) begin(phase string) {
	if p == nil {
		return
	}
	p.phase, p.current, p.files, p.bytes, p.last = phase, "", 0, 0, time.Now()
	fmt.Fprintln(p.out, phase+"...")
}
func (p *preparationProgress) item(key string) {
	if p == nil {
		return
	}
	fmt.Fprintf(p.out, "  Scanning %s\n", display(key))
}
func (p *preparationProgress) file(path string) {
	if p == nil {
		return
	}
	p.files++
	p.current = path
	p.emit(false)
}
func (p *preparationProgress) emit(force bool) {
	if p == nil || (!force && time.Since(p.last) < time.Second) {
		return
	}
	fmt.Fprintf(p.out, "  %s: %d files, %.1f MiB read", p.phase, p.files, float64(p.bytes)/(1<<20))
	if p.current != "" {
		fmt.Fprintf(p.out, " — %s", display(p.current))
	}
	fmt.Fprintln(p.out)
	p.last = time.Now()
}
func (p *preparationProgress) finish() { p.emit(true) }
func (p *preparationProgress) reader(ctx context.Context, r io.Reader) io.Reader {
	reader := contextReader{ctx, r}
	if p == nil {
		return reader
	}
	return progressReader{reader, p}
}

type progressReader struct {
	r io.Reader
	p *preparationProgress
}

func (r progressReader) Read(b []byte) (int, error) {
	n, err := r.r.Read(b)
	r.p.bytes += int64(n)
	r.p.emit(false)
	return n, err
}
