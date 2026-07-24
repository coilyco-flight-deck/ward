package main

import (
	"fmt"
	"io/fs"
	"path"
	"sort"

	kdl "github.com/calico32/kdl-go"
)

type bundleKDLFile struct {
	path string
	src  []byte
	doc  *kdl.Document
}

func loadBundleKDLFiles(src configSource) ([]bundleKDLFile, error) {
	root := "."

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
