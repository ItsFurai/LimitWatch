package main

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// listProcs snapshots running processes with the Toolhelp API, and reads
// the full path only for the few whose names we care about.
func listProcs() []proc {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var out []proc
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		name := windows.UTF16ToString(e.ExeFile[:])
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "claude") && !strings.HasPrefix(lower, "codex") && !strings.HasPrefix(lower, "chatgpt") {
			continue
		}
		out = append(out, proc{PID: int(e.ProcessID), Name: name, Path: procPath(e.ProcessID)})
	}
	return out
}

func procPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
