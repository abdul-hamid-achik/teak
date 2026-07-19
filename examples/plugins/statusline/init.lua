-- Statusline Plugin for Teak
-- Refreshes Teak's status message with the active buffer position.

local function refresh_status()
    local path = buffer.get_filepath() or "[No Name]"
    local line, col = buffer.get_cursor()
    editor.set_status("Teak | " .. path .. " | " .. line .. ":" .. col)
end

function setup()
    editor.command("statusline.refresh", refresh_status)
    keymap.set("n", "<leader>ss", "statusline.refresh", {
        desc = "Refresh the plugin statusline",
    })
    autocmd.register("CursorMoved", refresh_status)
end
