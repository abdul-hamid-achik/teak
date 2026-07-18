package treebuilder

import (
	"strings"

	"teak/internal/git"
)

func Build(entries []git.StatusEntry, staged bool) []*git.GitTreeNode {
	root := &git.GitTreeNode{IsDir: true, Expanded: true}

	for i := range entries {
		e := &entries[i]
		path := strings.TrimRight(e.Path, "/")
		if path == "" {
			continue
		}

		if e.IsDir {
			addDirectoryNode(root, path, e, staged)
			continue
		}

		addFileNode(root, path, e, staged)
	}

	return root.Children
}

func addDirectoryNode(root *git.GitTreeNode, path string, e *git.StatusEntry, staged bool) {
	parts := strings.Split(path, "/")
	node := root
	for i, part := range parts {
		dirPath := strings.Join(parts[:i+1], "/")
		found := false
		for _, c := range node.Children {
			if c.IsDir && c.Name == part {
				node = c
				found = true
				break
			}
		}
		if !found {
			dir := &git.GitTreeNode{
				Name:     part,
				Path:     dirPath,
				IsDir:    true,
				Depth:    node.Depth + 1,
				Expanded: true,
				Entry:    e,
				Staged:   staged,
			}
			if node == root {
				dir.Depth = 0
			}
			node.Children = append(node.Children, dir)
			node = dir
		}
	}
}

func addFileNode(root *git.GitTreeNode, path string, e *git.StatusEntry, staged bool) {
	parts := strings.Split(path, "/")
	node := root
	for j, part := range parts {
		if j == len(parts)-1 {
			node.Children = append(node.Children, &git.GitTreeNode{
				Name:   part,
				Path:   e.Path,
				IsDir:  false,
				Depth:  j,
				Entry:  e,
				Staged: staged,
			})
		} else {
			found := false
			for _, c := range node.Children {
				if c.IsDir && c.Name == part {
					node = c
					found = true
					break
				}
			}
			if !found {
				dirPath := strings.Join(parts[:j+1], "/")
				dir := &git.GitTreeNode{
					Name:     part,
					Path:     dirPath,
					IsDir:    true,
					Depth:    j,
					Expanded: true,
				}
				node.Children = append(node.Children, dir)
				node = dir
			}
		}
	}
}
