package app

import (
	"fmt"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"teak/internal/overlay"
	"teak/internal/plugin"
)

const maxPluginConfirmOptions = 8

const maxPluginSelectOptions = 128

const maxPluginInputBytes = 4096

const (
	maxPluginInputPromptBytes    = 4096
	maxPluginConfirmMessageBytes = 16 << 10
	maxPluginConfirmOptionBytes  = 256
	maxPluginSelectOptionBytes   = 256
)

const (
	maxPluginFloatContentBytes = 64 << 10
	maxPluginFloatWidth        = 120
	maxPluginFloatHeight       = 40
)

var nextPluginFloatID atomic.Uint64

// pluginUIConfirmResultMsg is emitted by the modal overlay and starts a new
// worker dispatch. It intentionally carries no Lua values across Update.
type pluginUIConfirmResultMsg struct {
	CallbackID uint64
	Option     string
	Index      int
	Accepted   bool
}

// pluginUIInputResultMsg is emitted by the text prompt and starts a new
// worker dispatch. It intentionally carries no Lua values across Update.
type pluginUIInputResultMsg struct {
	CallbackID uint64
	Value      string
	Accepted   bool
}

type pluginUISelectResultMsg struct {
	CallbackID uint64
	Option     string
	Index      int
	Accepted   bool
}

type pluginUISelectItem struct {
	CallbackID uint64
	Option     string
	Index      int
}

func validatePluginConfirm(request plugin.UIConfirmRequest) error {
	if request.CallbackID == 0 {
		return fmt.Errorf("confirmation callback is required")
	}
	if len(request.Message) > maxPluginConfirmMessageBytes {
		return fmt.Errorf("confirmation message exceeds %d bytes", maxPluginConfirmMessageBytes)
	}
	if len(request.Options) == 0 || len(request.Options) > maxPluginConfirmOptions {
		return fmt.Errorf("confirmation requires 1-%d options", maxPluginConfirmOptions)
	}
	for i, option := range request.Options {
		if option == "" {
			return fmt.Errorf("confirmation option %d is empty", i+1)
		}
		if len(option) > maxPluginConfirmOptionBytes {
			return fmt.Errorf("confirmation option %d exceeds %d bytes", i+1, maxPluginConfirmOptionBytes)
		}
	}
	return nil
}

func (m *Model) showPluginConfirm(request plugin.UIConfirmRequest) error {
	if err := validatePluginConfirm(request); err != nil {
		return err
	}
	buttons := make([]overlay.Button, len(request.Options))
	for i, option := range request.Options {
		buttons[i] = overlay.Button{
			Label: option,
			Action: pluginUIConfirmResultMsg{
				CallbackID: request.CallbackID,
				Option:     option,
				Index:      i + 1,
				Accepted:   true,
			},
		}
	}
	confirm := overlay.NewConfirm(
		"Plugin confirmation",
		request.Message,
		nil,
		buttons,
		m.theme,
	)
	confirm.SetDismissAction(pluginUIConfirmResultMsg{CallbackID: request.CallbackID})
	if m.width > 0 {
		confirm.SetWidth(min(50, max(1, m.width-4)))
	}
	m.overlayStack.Push(confirm)
	return nil
}

func (m Model) handlePluginUIConfirmResult(msg pluginUIConfirmResultMsg) (tea.Model, tea.Cmd) {
	if m.pluginMgr == nil || msg.CallbackID == 0 {
		return m, nil
	}
	runtime := newPluginAsyncRuntime(m)
	manager := m.pluginMgr
	return m, func() tea.Msg {
		err := manager.DispatchUIConfirm(runtime, msg.CallbackID, plugin.UIConfirmResult{
			Option:   msg.Option,
			Index:    msg.Index,
			Accepted: msg.Accepted,
		})
		return pluginDispatchResultMsg{Runtime: runtime, Err: err}
	}
}

func validatePluginInput(request plugin.UIInputRequest) error {
	if request.CallbackID == 0 {
		return fmt.Errorf("input callback is required")
	}
	if len(request.Prompt) > maxPluginInputPromptBytes {
		return fmt.Errorf("input prompt exceeds %d bytes", maxPluginInputPromptBytes)
	}
	if len(request.InitialValue) > maxPluginInputBytes {
		return fmt.Errorf("input initial value exceeds %d bytes", maxPluginInputBytes)
	}
	return nil
}

func validatePluginSelect(request plugin.UISelectRequest) error {
	if request.CallbackID == 0 {
		return fmt.Errorf("selector callback is required")
	}
	if len(request.Prompt) > maxPluginInputPromptBytes {
		return fmt.Errorf("selector prompt exceeds %d bytes", maxPluginInputPromptBytes)
	}
	if len(request.Options) == 0 || len(request.Options) > maxPluginSelectOptions {
		return fmt.Errorf("selector requires 1-%d options", maxPluginSelectOptions)
	}
	for i, option := range request.Options {
		if option == "" {
			return fmt.Errorf("selector option %d is empty", i+1)
		}
		if len(option) > maxPluginSelectOptionBytes {
			return fmt.Errorf("selector option %d exceeds %d bytes", i+1, maxPluginSelectOptionBytes)
		}
	}
	return nil
}

func validatePluginFloat(options plugin.UIFloatOptions) error {
	if options.Title == "" {
		return fmt.Errorf("float title is required")
	}
	if len(options.Content) > maxPluginFloatContentBytes {
		return fmt.Errorf("float content exceeds %d bytes", maxPluginFloatContentBytes)
	}
	if options.Width < 1 || options.Width > maxPluginFloatWidth {
		return fmt.Errorf("float width must be between 1 and %d", maxPluginFloatWidth)
	}
	if options.Height < 1 || options.Height > maxPluginFloatHeight {
		return fmt.Errorf("float height must be between 1 and %d", maxPluginFloatHeight)
	}
	return nil
}

func allocatePluginFloatID() (int, error) {
	id := nextPluginFloatID.Add(1)
	if id > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("float ID space exhausted")
	}
	return int(id), nil
}

func (m *Model) showPluginFloatWithID(id int, options plugin.UIFloatOptions) error {
	if err := validatePluginFloat(options); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("float ID must be positive")
	}
	if m.pluginFloats == nil {
		m.pluginFloats = make(map[int]*overlay.Float)
	}
	if _, exists := m.pluginFloats[id]; exists {
		return fmt.Errorf("float %d is already open", id)
	}
	float := overlay.NewFloat(id, options.Title, options.Content, options.Width, options.Height)
	m.pluginFloats[id] = float
	m.overlayStack.Push(float)
	return nil
}

func (m *Model) closePluginFloat(id int) error {
	float, ok := m.pluginFloats[id]
	if !ok {
		return fmt.Errorf("float %d is not open", id)
	}
	if !m.overlayStack.Remove(float) {
		delete(m.pluginFloats, id)
		return fmt.Errorf("float %d is no longer visible", id)
	}
	delete(m.pluginFloats, id)
	return nil
}

func (m *Model) showPluginInput(request plugin.UIInputRequest) error {
	if err := validatePluginInput(request); err != nil {
		return err
	}
	input := overlay.NewInput(request.Prompt, request.InitialValue, m.theme)
	input.SetResultAction(func(result overlay.InputResult) tea.Msg {
		return pluginUIInputResultMsg{
			CallbackID: request.CallbackID,
			Value:      result.Value,
			Accepted:   result.Accepted,
		}
	})
	if m.width > 0 {
		input.SetWidth(min(70, max(1, m.width-4)))
	}
	m.overlayStack.Push(input)
	return nil
}

func (m *Model) showPluginSelect(request plugin.UISelectRequest) error {
	if err := validatePluginSelect(request); err != nil {
		return err
	}
	items := make([]overlay.PickerItem, len(request.Options))
	for i, option := range request.Options {
		items[i] = overlay.PickerItem{
			Label: option,
			Value: pluginUISelectItem{CallbackID: request.CallbackID, Option: option, Index: i + 1},
		}
	}
	picker := overlay.NewPicker(request.Prompt, items, m.theme, fmt.Sprintf("plugin-select-%d", request.CallbackID))
	picker.SetDismissAction(pluginUISelectResultMsg{CallbackID: request.CallbackID})
	if m.width > 0 {
		picker.SetSize(min(80, max(1, m.width-4)), min(24, max(1, m.height-4)))
	}
	m.overlayStack.Push(picker)
	return nil
}

// clearOverlayStack releases plugin callbacks attached to overlays that are
// removed by a higher-level action before the user can answer them.
func (m *Model) clearOverlayStack() {
	for _, layer := range m.overlayStack.Clear() {
		switch layer := layer.(type) {
		case *overlay.Confirm:
			if result, ok := layer.DismissAction().(pluginUIConfirmResultMsg); ok {
				plugin.CancelUICallback(result.CallbackID)
			}
		case *overlay.Input:
			if result, ok := layer.CancelAction().(pluginUIInputResultMsg); ok {
				plugin.CancelUICallback(result.CallbackID)
			}
		case *overlay.Picker:
			if result, ok := layer.DismissAction().(pluginUISelectResultMsg); ok {
				plugin.CancelUICallback(result.CallbackID)
			}
		case *overlay.Float:
			delete(m.pluginFloats, layer.ID)
		case *healthDashboardOverlay:
			if layer == m.healthDashboard {
				m.cancelHealthDashboard()
				m.healthDashboard = nil
			}
		}
	}
}

func (m *Model) handlePluginFloatClosed(msg overlay.FloatCloseMsg) {
	delete(m.pluginFloats, msg.ID)
}

func (m Model) handlePluginUIInputResult(msg pluginUIInputResultMsg) (tea.Model, tea.Cmd) {
	if m.pluginMgr == nil || msg.CallbackID == 0 {
		return m, nil
	}
	runtime := newPluginAsyncRuntime(m)
	manager := m.pluginMgr
	return m, func() tea.Msg {
		err := manager.DispatchUIInput(runtime, msg.CallbackID, plugin.UIInputResult{
			Value:    msg.Value,
			Accepted: msg.Accepted,
		})
		return pluginDispatchResultMsg{Runtime: runtime, Err: err}
	}
}

func (m Model) handlePluginUISelectResult(msg pluginUISelectResultMsg) (tea.Model, tea.Cmd) {
	if m.pluginMgr == nil || msg.CallbackID == 0 {
		return m, nil
	}
	runtime := newPluginAsyncRuntime(m)
	manager := m.pluginMgr
	return m, func() tea.Msg {
		err := manager.DispatchUISelect(runtime, msg.CallbackID, plugin.UISelectResult{
			Option:   msg.Option,
			Index:    msg.Index,
			Accepted: msg.Accepted,
		})
		return pluginDispatchResultMsg{Runtime: runtime, Err: err}
	}
}
