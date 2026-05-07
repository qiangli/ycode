package netscan

import (
	"sort"
	"time"
)

// Merge folds new observations into a working set keyed by IP. Existing
// hosts get sources appended, names filled in from whichever observer
// has them, and LastSeen advanced. New hosts are inserted.
//
// Returns a freshly sorted slice (most recently seen first; ties broken
// by IP for determinism).
func Merge(existing []Host, observed []Host) []Host {
	byIP := make(map[string]*Host, len(existing)+len(observed))
	for i := range existing {
		h := existing[i]
		byIP[h.IP] = &h
	}
	for _, o := range observed {
		if o.IP == "" {
			continue
		}
		cur, ok := byIP[o.IP]
		if !ok {
			cp := o
			byIP[o.IP] = &cp
			continue
		}
		if cur.Name == "" && o.Name != "" {
			cur.Name = o.Name
		}
		if cur.Port == 0 && o.Port != 0 {
			cur.Port = o.Port
		}
		if cur.Service == "" && o.Service != "" {
			cur.Service = o.Service
		}
		for _, s := range o.Sources {
			cur.AddSource(s)
		}
		if o.LastSeen.After(cur.LastSeen) {
			cur.LastSeen = o.LastSeen
		}
		for k, v := range o.Attrs {
			if cur.Attrs == nil {
				cur.Attrs = make(map[string]string, 1)
			}
			if _, exists := cur.Attrs[k]; !exists {
				cur.Attrs[k] = v
			}
		}
	}

	out := make([]Host, 0, len(byIP))
	for _, h := range byIP {
		out = append(out, *h)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		return out[i].IP < out[j].IP
	})
	return out
}

// stamp returns ts if non-zero, otherwise time.Now().UTC().
// Helper used by source-specific probers so callers can pass a fixed
// timestamp in tests.
func stamp(ts time.Time) time.Time {
	if ts.IsZero() {
		return time.Now().UTC()
	}
	return ts.UTC()
}
