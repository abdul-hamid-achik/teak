-- Statusline Plugin for Teak
-- A customizable status line plugin

local M = {}

-- Plugin configuration
M.config = {
    show_mode = true,
    show_file = true,
    show_cursor = true,
    separator = " | ",
}

-- Default setup function
function M.setup(config)
    M.config = vim.tbl_extend("force", M.config, config or {})
    
    -- Register a custom command
    editor.command("statusline_config", function()
        editor.echo("Statusline config: " .. vim.inspect(M.config))
    end)
    
    -- Register keybinding
    keymap.set("n", "<leader>sc", function()
        editor.command("statusline_config")
    end, { desc = "Show statusline config" })
    
    -- Register autocommand to update status on cursor move
    autocmd.register("CursorMoved", function()
        M.update()
    end)
    
    editor.echo("Statusline plugin loaded!")
end

-- Update status line
function M.update()
    local mode = editor.get_mode()
    local filepath = buffer.get_filepath() or "[No Name]"
    local dirty = buffer.is_dirty() and " [+]" or ""
    local line, col = buffer.get_cursor()
    
    local parts = {}
    
    if M.config.show_mode then
        table.insert(parts, mode:upper())
    end
    
    if M.config.show_file then
        table.insert(parts, filepath .. dirty)
    end
    
    if M.config.show_cursor then
        table.insert(parts, string.format("Line %d, Col %d", line, col))
    end
    
    local status = table.concat(parts, M.config.separator)
    editor.set_status(status)
end

-- Teardown function (called when plugin is unloaded)
function M.teardown()
    editor.echo("Statusline plugin unloaded")
end

return M
