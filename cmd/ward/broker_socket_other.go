//go:build !unix

package main

func secureBrokerSocket(_ string, _ int) error {
	return nil
}
