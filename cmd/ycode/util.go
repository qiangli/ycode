package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
)

func openBrowser(url string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		fmt.Printf("Open %s in your browser\n", url)
		return nil
	}
	return exec.CommandContext(context.Background(), cmd, args...).Start()
}

func splitLines(s string) []string {
	var lines []string
	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] != '\n' {
			i++
		}
		line := s[:i]
		if line != "" {
			lines = append(lines, line)
		}
		if i < len(s) {
			i++
		}
		s = s[i:]
	}
	return lines
}
