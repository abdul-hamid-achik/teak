-- Auto Pairs Plugin for Teak
--
-- Teak currently dispatches plugin mappings from its normal editor context.
-- This example therefore inserts a pair on demand instead of intercepting
-- every typed character in an insert mode that Teak does not expose yet.

local function insert_parens()
    buffer.insert("()")
    local line, col = buffer.get_cursor()
    buffer.set_cursor(line, col - 1)
    editor.set_status("Auto pairs: inserted ()")
end

function setup()
    editor.command("autopairs.insert_parens", insert_parens)
    keymap.set("n", "<leader>ap", "autopairs.insert_parens", {
        desc = "Insert parentheses and place the cursor inside",
    })
end
