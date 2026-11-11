//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

type routerOwnedTree struct{ cmd *exec.Cmd }

func codingTreeQuiescent(*routerOwnedTree) error {
	return errors.New("coding process tree quiescence is not verified on this platform")
}

func startRouterOwnedTree(cmd *exec.Cmd) (*routerOwnedTree, error) {
	prepareLearningProcessTree(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &routerOwnedTree{cmd: cmd}, nil
}

func stopRouterOwnedTree(tree *routerOwnedTree) error {
	if tree == nil {
		return nil
	}
	return terminateLearningProcessTree(tree.cmd)
}

// After Wait, releasing the tree cannot safely terminate remaining descendants.
func releaseRouterOwnedTree(tree *routerOwnedTree) error {
	return nil
}
