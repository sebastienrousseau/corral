// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build darwin

package engine

import (
	"errors"

	"golang.org/x/sys/unix"
	"howett.net/plist"
)

const finderTagsAttribute = "com.apple.metadata:_kMDItemUserTags"

var (
	getFinderXattr = unix.Getxattr
	setFinderXattr = unix.Setxattr
	marshalPlist   = plist.Marshal
	unmarshalPlist = plist.Unmarshal
)

func platformReadFinderTags(path string) ([]string, error) {
	size, err := getFinderXattr(path, finderTagsAttribute, nil)
	if errors.Is(err, unix.ENOATTR) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	data := make([]byte, size)
	if _, err := getFinderXattr(path, finderTagsAttribute, data); err != nil {
		return nil, err
	}
	var tags []string
	if _, err := unmarshalPlist(data, &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

func platformWriteFinderTags(path string, tags []string) error {
	data, err := marshalPlist(tags, plist.BinaryFormat)
	if err != nil {
		return err
	}
	return setFinderXattr(path, finderTagsAttribute, data, 0)
}
