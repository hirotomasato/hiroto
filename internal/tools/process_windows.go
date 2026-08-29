//go:build windows

package tools

import "os/exec"

func setProcGroup(cmd *exec.Cmd) {}

func killProcGroup(p *proc) {}
