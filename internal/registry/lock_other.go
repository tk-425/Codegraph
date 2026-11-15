//go:build !darwin && !linux

package registry

import (
	"context"
	"fmt"
	"os"
)

func lockRegistry(context.Context, string) (*os.File, error) {
	return nil, fmt.Errorf("registry updates require native file locking on darwin or linux")
}
