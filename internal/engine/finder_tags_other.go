// SPDX-FileCopyrightText: 2026 Sebastien Rousseau <sebastian.rousseau@gmail.com>
// SPDX-License-Identifier: GPL-3.0-only

//go:build !darwin

package engine

func platformReadFinderTags(string) ([]string, error) {
	return nil, nil
}

func platformWriteFinderTags(string, []string) error {
	return nil
}
