//go:build windows

package termpanel

import (
	"context"
	"fmt"
)

func startSession(context.Context, string, int, int) (session, error) {
	return nil, fmt.Errorf("integrated terminal is not available on Windows yet")
}
