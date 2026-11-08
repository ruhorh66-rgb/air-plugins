//go:build !windows

package main

import "os/exec"

type routerOwnedTree struct{ cmd *exec.Cmd }

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
