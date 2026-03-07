package app

import (
	"teak/internal/config"
	"teak/internal/editor"
	"teak/internal/git"
	"teak/internal/overlay"
	"teak/internal/search"
	"teak/internal/settings"
	"teak/internal/ui"
)

// OverlayManager handles all modal overlays and transient UI states.
type OverlayManager struct {
	// Theme reference for creating confirmations
	theme ui.Theme

	// Help
	showHelp bool
	helpM    editor.HelpModel

	// Search
	showSearch        bool
	searchMode        search.Mode
	searchM           search.Model
	lastSearchResults []search.Result
	lastSearchIndex   int

	// Input modes
	goToLineMode  bool
	goToLineInput string
	renameMode    bool
	renameInput   string
	saveAsMode    bool
	saveAsInput   string
	newFileMode   bool
	newFolderMode bool
	newItemInput  string
	newItemDir    string

	// Pickers
	showBranchPicker bool
	branchPickerM    git.BranchPickerModel
	showSettings     bool
	settingsM        settings.Model

	// Stack for generic overlays
	overlayStack overlay.Stack

	// Confirmations
	unsavedConfirm *overlay.Confirm
	deleteConfirm  bool
	deleteTarget   string
}

// NewOverlayManager creates a new overlay manager.
func NewOverlayManager(theme ui.Theme, rootDir string) *OverlayManager {
	return &OverlayManager{
		theme:         theme,
		helpM:         editor.NewHelpModel(theme),
		searchM:       search.New(theme, rootDir, search.ModeText),
		branchPickerM: git.NewBranchPicker(theme),
		settingsM:     settings.New(theme, config.Config{}, ""), // Will be updated when config is available
		overlayStack:  overlay.Stack{},
	}
}

// ============== Help Overlay ==============

// ShowHelp shows the help overlay.
func (om *OverlayManager) ShowHelp() {
	om.showHelp = true
}

// HideHelp hides the help overlay.
func (om *OverlayManager) HideHelp() {
	om.showHelp = false
}

// IsHelpVisible returns whether help is visible.
func (om *OverlayManager) IsHelpVisible() bool {
	return om.showHelp
}

// GetHelpModel returns the help model.
func (om *OverlayManager) GetHelpModel() *editor.HelpModel {
	return &om.helpM
}

// ============== Search Overlay ==============

// ShowSearch shows the search overlay with the given mode.
func (om *OverlayManager) ShowSearch(mode search.Mode) {
	om.showSearch = true
	om.searchMode = mode
	// Note: search.Model doesn't have SetMode, so we track mode separately
	// The actual search will be created fresh when needed
}

// HideSearch hides the search overlay.
func (om *OverlayManager) HideSearch() {
	om.showSearch = false
}

// IsSearchVisible returns whether search is visible.
func (om *OverlayManager) IsSearchVisible() bool {
	return om.showSearch
}

// GetSearchModel returns the search model.
func (om *OverlayManager) GetSearchModel() *search.Model {
	return &om.searchM
}

// GetSearchMode returns the current search mode.
func (om *OverlayManager) GetSearchMode() search.Mode {
	return om.searchMode
}

// SetLastSearchResults stores the last search results.
func (om *OverlayManager) SetLastSearchResults(results []search.Result) {
	om.lastSearchResults = results
	om.lastSearchIndex = 0
}

// GetLastSearchResults returns the last search results.
func (om *OverlayManager) GetLastSearchResults() []search.Result {
	return om.lastSearchResults
}

// GetLastSearchIndex returns the current index in search results.
func (om *OverlayManager) GetLastSearchIndex() int {
	return om.lastSearchIndex
}

// SetLastSearchIndex sets the current index in search results.
func (om *OverlayManager) SetLastSearchIndex(idx int) {
	if idx >= 0 && idx < len(om.lastSearchResults) {
		om.lastSearchIndex = idx
	}
}

// NextSearchResult moves to the next search result.
func (om *OverlayManager) NextSearchResult() {
	if len(om.lastSearchResults) > 0 {
		om.lastSearchIndex = (om.lastSearchIndex + 1) % len(om.lastSearchResults)
	}
}

// PrevSearchResult moves to the previous search result.
func (om *OverlayManager) PrevSearchResult() {
	if len(om.lastSearchResults) > 0 {
		om.lastSearchIndex--
		if om.lastSearchIndex < 0 {
			om.lastSearchIndex = len(om.lastSearchResults) - 1
		}
	}
}

// ============== Input Modes ==============

// ShowGoToLine shows the go-to-line input.
func (om *OverlayManager) ShowGoToLine() {
	om.goToLineMode = true
	om.goToLineInput = ""
}

// HideGoToLine hides the go-to-line input.
func (om *OverlayManager) HideGoToLine() {
	om.goToLineMode = false
	om.goToLineInput = ""
}

// IsGoToLineVisible returns whether go-to-line is active.
func (om *OverlayManager) IsGoToLineVisible() bool {
	return om.goToLineMode
}

// GetGoToLineInput returns the go-to-line input.
func (om *OverlayManager) GetGoToLineInput() string {
	return om.goToLineInput
}

// SetGoToLineInput sets the go-to-line input.
func (om *OverlayManager) SetGoToLineInput(input string) {
	om.goToLineInput = input
}

// ShowRename shows the rename input with initial value.
func (om *OverlayManager) ShowRename(initial string) {
	om.renameMode = true
	om.renameInput = initial
}

// HideRename hides the rename input.
func (om *OverlayManager) HideRename() {
	om.renameMode = false
	om.renameInput = ""
}

// IsRenameVisible returns whether rename is active.
func (om *OverlayManager) IsRenameVisible() bool {
	return om.renameMode
}

// GetRenameInput returns the rename input.
func (om *OverlayManager) GetRenameInput() string {
	return om.renameInput
}

// SetRenameInput sets the rename input.
func (om *OverlayManager) SetRenameInput(input string) {
	om.renameInput = input
}

// ShowSaveAs shows the save-as input with initial value.
func (om *OverlayManager) ShowSaveAs(initial string) {
	om.saveAsMode = true
	om.saveAsInput = initial
}

// HideSaveAs hides the save-as input.
func (om *OverlayManager) HideSaveAs() {
	om.saveAsMode = false
	om.saveAsInput = ""
}

// IsSaveAsVisible returns whether save-as is active.
func (om *OverlayManager) IsSaveAsVisible() bool {
	return om.saveAsMode
}

// GetSaveAsInput returns the save-as input.
func (om *OverlayManager) GetSaveAsInput() string {
	return om.saveAsInput
}

// SetSaveAsInput sets the save-as input.
func (om *OverlayManager) SetSaveAsInput(input string) {
	om.saveAsInput = input
}

// ShowNewFile shows the new file input for the given directory.
func (om *OverlayManager) ShowNewFile(dir string) {
	om.newFileMode = true
	om.newItemDir = dir
	om.newItemInput = ""
}

// ShowNewFolder shows the new folder input for the given directory.
func (om *OverlayManager) ShowNewFolder(dir string) {
	om.newFolderMode = true
	om.newItemDir = dir
	om.newItemInput = ""
}

// HideNewItem hides the new file/folder input.
func (om *OverlayManager) HideNewItem() {
	om.newFileMode = false
	om.newFolderMode = false
	om.newItemInput = ""
	om.newItemDir = ""
}

// IsNewFileVisible returns whether new file input is active.
func (om *OverlayManager) IsNewFileVisible() bool {
	return om.newFileMode
}

// IsNewFolderVisible returns whether new folder input is active.
func (om *OverlayManager) IsNewFolderVisible() bool {
	return om.newFolderMode
}

// GetNewItemInput returns the new item input.
func (om *OverlayManager) GetNewItemInput() string {
	return om.newItemInput
}

// SetNewItemInput sets the new item input.
func (om *OverlayManager) SetNewItemInput(input string) {
	om.newItemInput = input
}

// GetNewItemDir returns the directory for new item.
func (om *OverlayManager) GetNewItemDir() string {
	return om.newItemDir
}

// IsAnyInputMode returns whether any input mode is active.
func (om *OverlayManager) IsAnyInputMode() bool {
	return om.goToLineMode || om.renameMode || om.saveAsMode || om.newFileMode || om.newFolderMode
}

// ClearAllInputs clears all input modes.
func (om *OverlayManager) ClearAllInputs() {
	om.HideGoToLine()
	om.HideRename()
	om.HideSaveAs()
	om.HideNewItem()
}

// ============== Pickers ==============

// ShowBranchPicker shows the branch picker.
func (om *OverlayManager) ShowBranchPicker() {
	om.showBranchPicker = true
}

// HideBranchPicker hides the branch picker.
func (om *OverlayManager) HideBranchPicker() {
	om.showBranchPicker = false
}

// IsBranchPickerVisible returns whether branch picker is visible.
func (om *OverlayManager) IsBranchPickerVisible() bool {
	return om.showBranchPicker
}

// GetBranchPicker returns the branch picker model.
func (om *OverlayManager) GetBranchPicker() *git.BranchPickerModel {
	return &om.branchPickerM
}

// ShowSettings shows the settings overlay.
func (om *OverlayManager) ShowSettings() {
	om.showSettings = true
}

// HideSettings hides the settings overlay.
func (om *OverlayManager) HideSettings() {
	om.showSettings = false
}

// IsSettingsVisible returns whether settings is visible.
func (om *OverlayManager) IsSettingsVisible() bool {
	return om.showSettings
}

// GetSettingsModel returns the settings model.
func (om *OverlayManager) GetSettingsModel() *settings.Model {
	return &om.settingsM
}

// ============== Overlay Stack ==============

// GetOverlayStack returns the overlay stack.
func (om *OverlayManager) GetOverlayStack() *overlay.Stack {
	return &om.overlayStack
}

// HasOverlay returns whether there's an active overlay in the stack.
func (om *OverlayManager) HasOverlay() bool {
	return om.overlayStack.Len() > 0
}

// ============== Confirmations ==============

// ShowUnsavedConfirm shows the unsaved changes confirmation.
func (om *OverlayManager) ShowUnsavedConfirm(tabIdx int) {
	buttons := []overlay.Button{
		{Label: "Close Without Saving", Action: overlay.ButtonAction{Label: "close"}},
		{Label: "Cancel", Action: overlay.ButtonAction{Label: "cancel"}},
	}
	om.unsavedConfirm = overlay.NewConfirm(
		"Unsaved Changes",
		"This tab has unsaved changes. Close anyway?",
		nil,
		buttons,
		om.theme,
	)
}

// HideUnsavedConfirm hides the unsaved confirmation.
func (om *OverlayManager) HideUnsavedConfirm() {
	om.unsavedConfirm = nil
}

// IsUnsavedConfirmVisible returns whether unsaved confirmation is visible.
func (om *OverlayManager) IsUnsavedConfirmVisible() bool {
	return om.unsavedConfirm != nil
}

// GetUnsavedConfirm returns the unsaved confirmation.
func (om *OverlayManager) GetUnsavedConfirm() *overlay.Confirm {
	return om.unsavedConfirm
}

// ShowDeleteConfirm shows the delete confirmation for the given path.
func (om *OverlayManager) ShowDeleteConfirm(path string) {
	om.deleteConfirm = true
	om.deleteTarget = path
}

// HideDeleteConfirm hides the delete confirmation.
func (om *OverlayManager) HideDeleteConfirm() {
	om.deleteConfirm = false
	om.deleteTarget = ""
}

// IsDeleteConfirmVisible returns whether delete confirmation is visible.
func (om *OverlayManager) IsDeleteConfirmVisible() bool {
	return om.deleteConfirm
}

// GetDeleteTarget returns the path to delete.
func (om *OverlayManager) GetDeleteTarget() string {
	return om.deleteTarget
}

// ============== General ==============

// IsAnyOverlayVisible returns whether any overlay is visible.
func (om *OverlayManager) IsAnyOverlayVisible() bool {
	return om.showHelp ||
		om.showSearch ||
		om.showBranchPicker ||
		om.showSettings ||
		om.IsAnyInputMode() ||
		om.IsUnsavedConfirmVisible() ||
		om.IsDeleteConfirmVisible() ||
		om.HasOverlay()
}

// ClearAllOverlays hides all overlays.
func (om *OverlayManager) ClearAllOverlays() {
	om.HideHelp()
	om.HideSearch()
	om.HideBranchPicker()
	om.HideSettings()
	om.ClearAllInputs()
	om.HideUnsavedConfirm()
	om.HideDeleteConfirm()
}

// Resize updates dimensions for overlay components.
func (om *OverlayManager) Resize(width, height int) {
	om.helpM.SetSize(width, height)
	om.searchM.SetSize(width, height)
	om.branchPickerM.SetSize(width, height)
	om.settingsM.SetSize(width, height)
}
