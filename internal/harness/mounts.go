package harness

import (
	"path"
	"sort"
	"strings"
)

// MountParent identifies the filesystem that will own a directory after Docker
// installs the mounts. An empty Mount means image storage; otherwise it names
// the enclosing mount target, whose host source must contain this directory.
type MountParent struct {
	Target string
	Mount  string
}

// MountParents accepts a validated definition. A nested auth mount can require
// parents inside a store (or another auth directory); creating those in the
// image would have no effect once Docker covers them with the enclosing mount.
func (d Definition) MountParents() []MountParent {
	targets := make([]string, 0, len(d.Stores)+len(d.Auth))
	for _, s := range d.Stores {
		targets = append(targets, s.Target)
	}
	for _, a := range d.Auth {
		targets = append(targets, a.Target)
	}
	parents := map[string]bool{}
	for _, target := range targets {
		for parent := path.Dir(target); parent == "/home/devuser" || strings.HasPrefix(parent, "/home/devuser/"); parent = path.Dir(parent) {
			parents[parent] = true
		}
	}
	names := make([]string, 0, len(parents))
	for parent := range parents {
		names = append(names, parent)
	}
	sort.Strings(names)
	result := make([]MountParent, 0, len(names))
	for _, parent := range names {
		owner := ""
		for _, target := range targets {
			if (parent == target || strings.HasPrefix(parent, target+"/")) && len(target) > len(owner) {
				owner = target
			}
		}
		result = append(result, MountParent{Target: parent, Mount: owner})
	}
	return result
}
