-- Auto Pairs Plugin for Teak
-- Automatically closes brackets and quotes

local M = {}

-- Default configuration
M.config = {
    pairs = {
        ["("] = ")",
        ["["] = "]",
        ["{"] = "}",
        ['"'] = '"',
        ["'"] = "'",
        ["`"] = "`",
    },
    skip_whitespace = true,
}

-- Setup function
function M.setup(config)
    M.config = vim.tbl_extend("force", M.config, config or {})
    
    -- Register insert mode keybindings for each pair
    for open, close in pairs(M.config.pairs) do
        keymap.set("i", open, function()
            M.insert_pair(open, close)
        end)
    end
    
    editor.echo("Auto pairs plugin loaded!")
end

-- Insert a pair of characters
function M.insert_pair(open, close)
    -- Insert both characters
    buffer.insert(open .. close)
    
    -- Move cursor back between the pair
    local line, col = buffer.get_cursor()
    buffer.set_cursor(line, col - 1)
end

-- Check if cursor is before a closing character
function M.is_before_close(close)
    local line, col = buffer.get_cursor()
    local current_line = buffer.get_line(line)
    
    if col >= #current_line then
        return false
    end
    
    return current_line:sub(col + 1, col + 1) == close
end

-- Teardown function
function M.teardown()
    -- Remove keybindings (would need proper cleanup API)
    editor.echo("Auto pairs plugin unloaded")
end

return M
