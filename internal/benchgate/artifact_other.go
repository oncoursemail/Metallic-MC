//go:build !linux

package benchgate

import (
	"errors"
	"os"
)

type artifactRoot interface {
	Open(string) (*os.File, error)
	Close() error
}

func openArtifactRoot(string) (artifactRoot, error) {
	return nil, errors.New("v1 scoped artifact verification requires Linux; no screening pass is available on this platform")
}
