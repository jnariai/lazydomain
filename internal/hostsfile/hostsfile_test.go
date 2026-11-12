package hostsfile

import (
	"reflect"
	"strings"
	"testing"
)

const windowsHosts = "# Copyright (c) Microsoft\r\n#\r\n127.0.0.1 localhost\r\n"

func TestReplaceAppendsBlockAndKeepsCRLF(t *testing.T) {
	got, err := Replace(windowsHosts, []Entry{{IP: "127.0.0.1", Hosts: []string{"a.test", "vite.a.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := windowsHosts + "\r\n" + BeginMarker + "\r\n127.0.0.1 a.test vite.a.test\r\n" + EndMarker + "\r\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	if strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Error("mixed line endings")
	}
}

func TestReplaceUpdatesInPlace(t *testing.T) {
	in := "127.0.0.1 localhost\n" + BeginMarker + "\n127.0.0.1 old.test\n" + EndMarker + "\n10.0.0.1 after\n"
	got, err := Replace(in, []Entry{{IP: "127.0.0.1", Hosts: []string{"new.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1 localhost\n" + BeginMarker + "\n127.0.0.1 new.test\n" + EndMarker + "\n10.0.0.1 after\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReplaceRemovesBlock(t *testing.T) {
	in := "127.0.0.1 localhost\n" + BeginMarker + "\n127.0.0.1 old.test\n" + EndMarker + "\n"
	got, err := Replace(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1 localhost\n" {
		t.Errorf("got %q", got)
	}
}

func TestReplaceIsIdempotent(t *testing.T) {
	entries := []Entry{{IP: "127.0.0.1", Hosts: []string{"a.test"}}}
	once, _ := Replace(windowsHosts, entries)
	twice, _ := Replace(once, entries)
	if once != twice {
		t.Errorf("not idempotent:\n%q\n%q", once, twice)
	}
}

func TestUnterminatedBlockIsRejected(t *testing.T) {
	in := "127.0.0.1 localhost\n" + BeginMarker + "\n127.0.0.1 a.test\n"
	if _, err := Replace(in, nil); err != ErrUnterminated {
		t.Errorf("Replace err = %v, want ErrUnterminated", err)
	}
	if _, _, err := Managed(in); err != ErrUnterminated {
		t.Errorf("Managed err = %v, want ErrUnterminated", err)
	}
}

func TestManaged(t *testing.T) {
	in := "127.0.0.1 localhost\r\n" + BeginMarker + "\r\n127.0.0.1 a.test vite.a.test\r\n# note\r\n127.0.0.1 b.test\r\n" + EndMarker + "\r\n"
	hosts, found, err := Managed(in)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	want := []string{"a.test", "vite.a.test", "b.test"}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("hosts = %v, want %v", hosts, want)
	}

	if _, found, _ := Managed(windowsHosts); found {
		t.Error("found block in file without one")
	}
}
