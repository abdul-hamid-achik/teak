//go:build windows

package termpanel

import "fmt"

func (m *Model) Start() error {
	return fmt.Errorf("integrated terminal is not available on Windows yet")
}
