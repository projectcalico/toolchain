// Copyright (c) 2026 Tigera, Inc. All rights reserved.

package util

import "os"

// EnvOr returns key's value, or def when key is unset or empty. Treating empty as
// absent is deliberate, so a workflow declaring a var it has no value for gets the
// default. Where the difference matters, use os.LookupEnv directly.
func EnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
