package main

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	kdl "github.com/calico32/kdl-go"
)

type bundleKDLFile struct {
	path string
	src  []byte
	doc  *kdl.Document
}

func loadBundleKDLFiles(src configSource) ([]bundleKDLFile, error) {
	root := src.execDir
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	files := make([]bundleKDLFile, 0, 8)
	err := fs.WalkDir(src.fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if path.Ext(d.Name()) != ".kdl" {
			return nil
		}
		srcBytes, err := fs.ReadFile(src.fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		doc, err := kdl.ParseString(string(srcBytes))
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		files = append(files, bundleKDLFile{path: p, src: srcBytes, doc: doc})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].path < files[j].path
	})
	return files, nil
}

func findUniqueBundleFileWithExt(src configSource, ext, label string) (string, []byte, error) {
	root := src.execDir
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	var hitPath string
	var hitSrc []byte
	err := fs.WalkDir(src.fsys, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if path.Ext(d.Name()) != ext {
			return nil
		}
		if hitPath != "" {
			return fmt.Errorf("bundle: duplicate %s in %s and %s (fail-closed)", label, hitPath, p)
		}
		srcBytes, err := fs.ReadFile(src.fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		hitPath = p
		hitSrc = srcBytes
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	if hitPath == "" {
		return "", nil, fmt.Errorf("bundle: missing %s (fail-closed)", label)
	}
	return hitPath, hitSrc, nil
}

func emitKDLDocument(nodes ...*kdl.Node) ([]byte, error) {
	doc := kdl.NewDocument(nodes...)
	out, err := kdl.EmitToString(doc)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func findUniqueBundleNode(files []bundleKDLFile, label string, match func(*kdl.Node) bool) (*bundleKDLFile, *kdl.Node, error) {
	var hit *bundleKDLFile
	var node *kdl.Node
	for i := range files {
		for _, n := range files[i].doc.Nodes {
			if !match(n) {
				continue
			}
			if hit != nil {
				return nil, nil, fmt.Errorf("bundle: duplicate %s in %s and %s (fail-closed)", label, hit.path, files[i].path)
			}
			hit = &files[i]
			node = n
		}
	}
	if hit == nil {
		return nil, nil, fmt.Errorf("bundle: missing %s (fail-closed)", label)
	}
	return hit, node, nil
}

func findUniqueNamedBundleNode(files []bundleKDLFile, label string, names ...string) (*bundleKDLFile, *kdl.Node, error) {
	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[name] = true
	}
	return findUniqueBundleNode(files, label, func(n *kdl.Node) bool {
		return nameSet[n.Name()]
	})
}

func findOptionalNamedBundleNode(files []bundleKDLFile, label string, names ...string) (*bundleKDLFile, *kdl.Node, bool, error) {
	nameSet := make(map[string]bool, len(names))
	for _, name := range names {
		nameSet[name] = true
	}
	var hit *bundleKDLFile
	var node *kdl.Node
	for i := range files {
		for _, n := range files[i].doc.Nodes {
			if !nameSet[n.Name()] {
				continue
			}
			if hit != nil {
				return nil, nil, false, fmt.Errorf("bundle: duplicate %s in %s and %s (fail-closed)", label, hit.path, files[i].path)
			}
			hit = &files[i]
			node = n
		}
	}
	if hit == nil {
		return nil, nil, false, nil
	}
	return hit, node, true, nil
}

func findMergedBundleNode(files []bundleKDLFile, label string, match func(*kdl.Node) bool) (*bundleKDLFile, *kdl.Node, error) {
	var hit *bundleKDLFile
	var merged *kdl.Node
	for i := range files {
		for _, n := range files[i].doc.Nodes {
			if !match(n) {
				continue
			}
			if hit == nil {
				hit = &files[i]
				merged = n.Clone()
				continue
			}
			if merged.Name() != n.Name() || !sameNodeArgs(merged, n) {
				return nil, nil, fmt.Errorf("bundle: conflicting %s in %s and %s (fail-closed)", label, hit.path, files[i].path)
			}
			if children := n.Children(); children != nil {
				merged.Children().Nodes = mergePlaceholderAwareChildren(merged.Children().Nodes, cloneNodes(children.Nodes)...)
			}
		}
	}
	if hit == nil {
		return nil, nil, fmt.Errorf("bundle: missing %s (fail-closed)", label)
	}
	return hit, merged, nil
}

func nodeArgEquals(n *kdl.Node, want ...string) bool {
	args := n.Arguments()
	if len(args) != len(want) {
		return false
	}
	for i, arg := range args {
		if arg.Kind() != kdl.String || arg.String() != want[i] {
			return false
		}
	}
	return true
}

func sameNodeArgs(a, b *kdl.Node) bool {
	aArgs := a.Arguments()
	bArgs := b.Arguments()
	if len(aArgs) != len(bArgs) {
		return false
	}
	for i := range aArgs {
		if a.Name() == "wrap" && b.Name() == "wrap" && i == 0 && sameWrapBinaryAlias(aArgs[i].String(), bArgs[i].String()) {
			continue
		}
		if aArgs[i].Kind() != bArgs[i].Kind() || aArgs[i].String() != bArgs[i].String() {
			return false
		}
	}
	return true
}

func sameWrapBinaryAlias(a, b string) bool {
	if a == b {
		return true
	}
	return normalizeWrapBinaryName(a) == normalizeWrapBinaryName(b)
}

func normalizeWrapBinaryName(name string) string {
	return strings.TrimSuffix(name, "-kdl")
}

func cloneNodes(nodes []*kdl.Node) []*kdl.Node {
	out := make([]*kdl.Node, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Clone())
	}
	return out
}

func wrapNodeMatchesPath(n *kdl.Node, want ...string) bool {
	if n.Name() != "wrap" {
		return false
	}
	args := n.Arguments()
	if len(args) == len(want) {
		return nodeArgEquals(n, want...)
	}
	if len(args) != len(want)+1 {
		return false
	}
	for i, w := range want {
		arg := args[i+1]
		if arg.Kind() != kdl.String || arg.String() != w {
			return false
		}
	}
	return true
}
