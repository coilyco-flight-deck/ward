//go:build unix

package main

import (
	"fmt"
	"os"
)

func secureBrokerSocket(path string, gid int) error {
	if err := os.Chown(path, -1, gid); err != nil {
		return fmt.Errorf("ward container broker: chgrp socket to gid %d: %w", gid, err)
	}
	if err := os.Chmod(path, brokerSocketMode); err != nil {
		return fmt.Errorf("ward container broker: chmod socket %#o: %w", brokerSocketMode, err)
	}
	return nil
}
