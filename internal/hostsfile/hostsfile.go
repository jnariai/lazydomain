package hostsfile

import (
	"errors"
	"strings"
)

const (
	BeginMarker = "# BEGIN lazydomain (managed, do not edit)"
	EndMarker   = "# END lazydomain"

	beginPrefix = "# BEGIN lazydomain"
)

var ErrUnterminated = errors.New("hosts file has a lazydomain BEGIN marker without END marker")

type Entry struct {
	IP    string
	Hosts []string
}

func Managed(content string) (hosts []string, found bool, err error) {
	inBlock := false
	for _, line := range splitLines(content) {
		t := strings.TrimSpace(line)
		switch {
		case !inBlock && strings.HasPrefix(t, beginPrefix):
			inBlock, found = true, true
		case inBlock && t == EndMarker:
			inBlock = false
		case inBlock && t != "" && !strings.HasPrefix(t, "#"):
			if f := strings.Fields(t); len(f) > 1 {
				hosts = append(hosts, f[1:]...)
			}
		}
	}
	if inBlock {
		return nil, true, ErrUnterminated
	}
	return hosts, found, nil
}

func Replace(content string, entries []Entry) (string, error) {
	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}

	var out []string
	inBlock := false
	insertAt := -1
	for _, line := range splitLines(content) {
		t := strings.TrimSpace(line)
		switch {
		case !inBlock && strings.HasPrefix(t, beginPrefix):
			inBlock = true
			if insertAt < 0 {
				insertAt = len(out)
			}
		case inBlock && t == EndMarker:
			inBlock = false
		case inBlock:
		default:
			out = append(out, line)
		}
	}
	if inBlock {
		return "", ErrUnterminated
	}

	if block := renderBlock(entries); len(block) > 0 {
		if insertAt < 0 {
			for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
				out = out[:len(out)-1]
			}
			if len(out) > 0 {
				out = append(out, "")
			}
			insertAt = len(out)
		}
		out = append(out[:insertAt], append(block, out[insertAt:]...)...)
	}

	if len(out) == 0 {
		return "", nil
	}
	return strings.Join(out, eol) + eol, nil
}

func renderBlock(entries []Entry) []string {
	var lines []string
	for _, e := range entries {
		if e.IP == "" || len(e.Hosts) == 0 {
			continue
		}
		lines = append(lines, e.IP+" "+strings.Join(e.Hosts, " "))
	}
	if len(lines) == 0 {
		return nil
	}
	return append(append([]string{BeginMarker}, lines...), EndMarker)
}

func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
