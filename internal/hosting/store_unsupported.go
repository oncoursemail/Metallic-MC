//go:build !linux

package hosting

import "fmt"

func openDurableStore(path string) (durableStore, error) {
	return nil, fmt.Errorf("%w: persistent hosting requires a private local Linux filesystem", ErrStore)
}
