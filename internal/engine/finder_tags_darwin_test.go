// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package engine

import (
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPlatformFinderTagsRoundTrip(t *testing.T) {
	path := t.TempDir()
	want := []string{"Active\n2", "Ecosystem: Go\n0"}
	if err := platformWriteFinderTags(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := platformReadFinderTags(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
}

func TestPlatformReadFinderTagsErrors(t *testing.T) {
	oldGet, oldUnmarshal := getFinderXattr, unmarshalPlist
	t.Cleanup(func() { getFinderXattr, unmarshalPlist = oldGet, oldUnmarshal })

	getFinderXattr = func(string, string, []byte) (int, error) { return 0, unix.ENOATTR }
	if tags, err := platformReadFinderTags("repo"); err != nil || tags != nil {
		t.Fatalf("missing attribute = %v, %v", tags, err)
	}

	wantErr := errors.New("get")
	getFinderXattr = func(string, string, []byte) (int, error) { return 0, wantErr }
	if _, err := platformReadFinderTags("repo"); !errors.Is(err, wantErr) {
		t.Fatalf("first get error = %v", err)
	}

	calls := 0
	getFinderXattr = func(_ string, _ string, data []byte) (int, error) {
		calls++
		if data == nil {
			return 1, nil
		}
		return 0, wantErr
	}
	if _, err := platformReadFinderTags("repo"); !errors.Is(err, wantErr) {
		t.Fatalf("second get error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("get calls = %d", calls)
	}

	getFinderXattr = func(_ string, _ string, data []byte) (int, error) {
		if data == nil {
			return 1, nil
		}
		data[0] = 0
		return 1, nil
	}
	unmarshalPlist = func([]byte, interface{}) (int, error) { return 0, wantErr }
	if _, err := platformReadFinderTags("repo"); !errors.Is(err, wantErr) {
		t.Fatalf("unmarshal error = %v", err)
	}
}

func TestPlatformWriteFinderTagsErrors(t *testing.T) {
	oldMarshal, oldSet := marshalPlist, setFinderXattr
	t.Cleanup(func() { marshalPlist, setFinderXattr = oldMarshal, oldSet })
	wantErr := errors.New("write")
	marshalPlist = func(interface{}, int) ([]byte, error) { return nil, wantErr }
	if err := platformWriteFinderTags("repo", nil); !errors.Is(err, wantErr) {
		t.Fatalf("marshal error = %v", err)
	}
	marshalPlist = oldMarshal
	setFinderXattr = func(string, string, []byte, int) error { return wantErr }
	if err := platformWriteFinderTags("repo", nil); !errors.Is(err, wantErr) {
		t.Fatalf("setxattr error = %v", err)
	}
}
