if vim.g.loaded_thought then return end
vim.g.loaded_thought = true

local group = vim.api.nvim_create_augroup("thought", {})

vim.api.nvim_create_autocmd("BufReadCmd", {
  group = group,
  pattern = "thought://*",
  desc = "Load a thought from the thought CLI",
  callback = function(ev) require("thought").read(ev.buf) end,
})

vim.api.nvim_create_autocmd("BufWriteCmd", {
  group = group,
  pattern = "thought://*",
  desc = "Write a thought through the thought CLI",
  callback = function(ev) require("thought").write(ev.buf, ev.match) end,
})

vim.api.nvim_create_user_command("NewThought", function(opts) require("thought").new(opts.args) end, {
  nargs = "+",
  desc = "Capture a new thought with this title and open it",
})
