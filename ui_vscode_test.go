// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"reflect"
	"testing"

	"codeberg.org/tslocum/cbind"
	"github.com/PalWorks/whatscli/config"
)

// Every VS Code mode key must be one cbind can decode, or Set would fail at start-up.
func TestVscodeKeysDecode(t *testing.T) {
	v := reflect.ValueOf(config.VscodeKeymap()).Elem()
	for i := 0; i < v.NumField(); i++ {
		key := v.Field(i).String()
		if _, _, _, err := cbind.Decode(key); err != nil {
			t.Errorf("%s = %q: %v", v.Type().Field(i).Name, key, err)
		}
	}
}
