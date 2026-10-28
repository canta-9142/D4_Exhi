//go:build !linux

package serial

import (
	"errors"
	"io"
)

var portPatterns []string

func OpenPort(name string) (io.ReadWriteCloser, error) {
	return nil, errors.New("USB CDC ports require Linux")
}
