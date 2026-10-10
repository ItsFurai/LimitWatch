//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// listProcs asks ps for every process's executable. On macOS that's the
// full path; on Linux it's the name, and /proc has the path.
func listProcs() []proc {
	out, err := exec.Command("ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil
	}
	var ps []proc
	for _, line := range strings.Split(string(out), "\n") {
		pidS, comm, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		pid, _ := strconv.Atoi(pidS)
		comm = strings.TrimSpace(comm)
		p := proc{PID: pid, Name: comm[strings.LastIndex(comm, "/")+1:], Path: comm}
		if exe, err := os.Readlink("/proc/" + pidS + "/exe"); err == nil {
			p.Path = exe
		}
		ps = append(ps, p)
	}
	return ps
}
