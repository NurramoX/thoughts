-- Run from anywhere: nvim --headless --clean -l nvim/tests/run.lua
--
-- A minimal test runner for the thought plugin. Each test gets a fresh fake CLI
-- state directory and a clean editor; failures are listed and make the exit
-- code non-zero.

local here = vim.fs.dirname(vim.fs.abspath(debug.getinfo(1, "S").source:sub(2)))
local root = vim.fs.dirname(here)
local fake = here .. "/fake-thought"

vim.opt.rtp:prepend(root)
vim.cmd.runtime("plugin/thought.lua")
require("thought").setup({ cmd = fake })

local state -- the current test's fake CLI state directory
local notes -- vim.notify calls made during the current test

vim.notify = function(msg, level) table.insert(notes, { msg = msg, level = level }) end

local function seed(id, thought)
  local f = assert(io.open(state .. "/" .. id .. ".json", "wb"))
  f:write(vim.json.encode(thought))
  f:close()
end

--- Makes every later CLI call exit with code, printing stderr.
local function failing(code, stderr)
  local f = assert(io.open(state .. "/fail.json", "wb"))
  f:write(vim.json.encode({ code = code, stderr = stderr }))
  f:close()
end

local function stored(id)
  local f = assert(io.open(state .. "/" .. id .. ".json", "rb"))
  local thought = vim.json.decode(f:read("a"))
  f:close()
  return thought
end

local function calls()
  local f = io.open(state .. "/calls.jsonl", "rb")
  if not f then return {} end
  local out = {}
  for line in f:lines() do table.insert(out, vim.json.decode(line)) end
  f:close()
  return out
end

local function eq(want, got, what)
  if not vim.deep_equal(want, got) then
    error(("%s: want %s, got %s"):format(what or "value", vim.inspect(want), vim.inspect(got)), 2)
  end
end

local tests = {}
local function test(name, fn) table.insert(tests, { name = name, fn = fn }) end

-- Reading

test("reads a body with a trailing newline as eol", function()
  seed(7, { title = "Borrow checker", body = "gist\n\nmore\n", version = 3 })
  vim.cmd.edit("thought://7")
  eq({ "gist", "", "more" }, vim.api.nvim_buf_get_lines(0, 0, -1, true), "lines")
  eq(true, vim.bo.eol, "eol")
  eq("unix", vim.bo.fileformat, "fileformat")
  eq("acwrite", vim.bo.buftype, "buftype")
  eq("markdown", vim.bo.filetype, "filetype")
  eq(false, vim.bo.swapfile, "swapfile")
  eq(false, vim.bo.modified, "modified")
  eq(3, vim.b.thought_version, "b:thought_version")
  eq("Borrow checker", vim.b.thought_title, "b:thought_title")
  eq({ "show", "--json", "7" }, calls()[1].argv, "argv")
end)

-- Byte-exact round-trips: open, write unchanged, the CLI gets the same bytes.

for _, case in ipairs({
  { "a trailing newline", "gist\n\n## Open questions\n\n- why?\n" },
  { "no trailing newline", "gist\nlast line" },
  { "CRLF line endings", "one\r\ntwo\r\n" },
  { "an empty body", "" },
  { "only a newline", "\n" },
  { "multibyte text", "héllo — 世界 🦀\nnaïve\n" },
}) do
  local name, body = case[1], case[2]
  test("round-trips " .. name .. " byte-exact", function()
    seed(1, { title = "t", body = body, version = 1 })
    vim.cmd.edit("thought://1")
    vim.cmd.write()
    local write = calls()[2]
    eq({ "write", "1", "--version", "1", "--json" }, write.argv, "argv")
    eq(body, write.stdin, "stdin")
    eq(false, vim.bo.modified, "modified")
  end)
end

test("reads a body without a trailing newline as noeol and nofixeol", function()
  seed(1, { title = "t", body = "a\nb", version = 1 })
  vim.cmd.edit("thought://1")
  eq({ "a", "b" }, vim.api.nvim_buf_get_lines(0, 0, -1, true), "lines")
  eq(false, vim.bo.eol, "eol")
  eq(false, vim.bo.fixeol, "fixeol")
end)

test("writes an edited buffer as its lines joined by newlines", function()
  seed(1, { title = "t", body = "a\n", version = 1 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, -1, -1, true, { "b", "c" })
  vim.cmd.write()
  eq("a\nb\nc\n", stored(1).body, "stored body")
end)

-- Versions and failures

test("each write is guarded by the version the last write returned", function()
  seed(1, { title = "t", body = "a\n", version = 4 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "b" })
  vim.cmd.write()
  eq(5, vim.b.thought_version, "b:thought_version after the first write")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "c" })
  vim.cmd.write()
  eq({ "write", "1", "--version", "5", "--json" }, calls()[3].argv, "second write argv")
  eq(6, vim.b.thought_version, "b:thought_version after the second write")
  eq("c\n", stored(1).body, "stored body")
end)

test("a stale write says what changed, points at :e! and leaves the buffer modified", function()
  seed(1, { title = "t", body = "a\n", version = 4 })
  vim.cmd.edit("thought://1")
  seed(1, { title = "t", body = "theirs\n", version = 9 })
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "mine" })
  vim.cmd.write()
  eq({ {
    msg = "thought 1 changed: you had version 4, it is now 9 (:e! reloads it, dropping your changes)",
    level = vim.log.levels.ERROR,
  } }, notes, "notes")
  eq(true, vim.bo.modified, "modified")
  eq(4, vim.b.thought_version, "b:thought_version")
  eq("theirs\n", stored(1).body, "stored body")
  for _, call in ipairs(calls()) do
    eq(nil, vim.tbl_contains(call.argv, "--force") or nil, "--force in " .. vim.inspect(call.argv))
  end
end)

test("a rejected write reports the problem's title and detail", function()
  seed(1, { title = "t", body = "a\n", version = 1 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "mine" })
  failing(5, '{"title":"Unprocessable Entity","status":422,"detail":"request body is not valid UTF-8"}')
  vim.cmd.write()
  eq({ { msg = "thought: Unprocessable Entity: request body is not valid UTF-8", level = vim.log.levels.ERROR } },
    notes, "notes")
  eq(true, vim.bo.modified, "modified")
end)

test("a problem without a detail reports just its title", function()
  seed(1, { title = "t", body = "a\n", version = 1 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "mine" })
  failing(6, '{"title":"Internal Server Error","status":500,"detail":null}')
  vim.cmd.write()
  eq({ { msg = "thought: Internal Server Error", level = vim.log.levels.ERROR } }, notes, "notes")
end)

test("stderr that is not problem+json is reported trimmed and unchanged", function()
  seed(1, { title = "t", body = "a\n", version = 1 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "mine" })
  failing(7, "thought: daemon unreachable: dial unix /run/thought.sock: connect: no such file or directory\n"
    .. "  start it with: thoughtd\n")
  vim.cmd.write()
  eq({ {
    msg = "thought: daemon unreachable: dial unix /run/thought.sock: connect: no such file or directory\n"
      .. "  start it with: thoughtd",
    level = vim.log.levels.ERROR,
  } }, notes, "notes")
  eq(true, vim.bo.modified, "modified")
end)

test(":e! reloads the thought and discards local changes", function()
  seed(1, { title = "old", body = "a\n", version = 1 })
  vim.cmd.edit("thought://1")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "mine" })
  seed(1, { title = "new", body = "x\ny", version = 2 })
  vim.cmd("edit!")
  eq({ "x", "y" }, vim.api.nvim_buf_get_lines(0, 0, -1, true), "lines")
  eq(false, vim.bo.eol, "eol")
  eq(false, vim.bo.modified, "modified")
  eq(2, vim.b.thought_version, "b:thought_version")
  eq("new", vim.b.thought_title, "b:thought_title")
  eq({ "show", "--json", "1" }, calls()[2].argv, "reload argv")
end)

test("a non-decimal id is an error and never reaches the CLI", function()
  for _, name in ipairs({ "thought://abc", "thought://12x", "thought://" }) do
    notes = {}
    vim.cmd.edit(name)
    eq(1, #notes, "notes for " .. name)
    eq(vim.log.levels.ERROR, notes[1].level, "level for " .. name)
  end
  eq({}, calls(), "calls")
end)

test("an unknown id reports the problem as the CLI would to a human", function()
  vim.cmd.edit("thought://42")
  eq({ { msg = "thought: Not Found: thought 42 not found", level = vim.log.levels.ERROR } }, notes, "notes")
end)

test("another buffer cannot be written to a thought", function()
  seed(1, { title = "t", body = "a\n", version = 1 })
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "scratch" })
  vim.cmd("write thought://1")
  eq(1, #notes, "notes")
  eq({}, calls(), "calls")
  eq("a\n", stored(1).body, "stored body")
end)

-- :NewThought

test(":NewThought captures a thought and opens it", function()
  vim.cmd("NewThought -borrow checker for configs")
  eq({ "add", "--", "-borrow checker for configs" }, calls()[1].argv, "add argv")
  eq("thought://1", vim.api.nvim_buf_get_name(0), "buffer name")
  eq("-borrow checker for configs", vim.b.thought_title, "b:thought_title")
  eq("acwrite", vim.bo.buftype, "buftype")
  vim.api.nvim_buf_set_lines(0, 0, -1, true, { "gist" })
  vim.cmd.write()
  eq("gist", stored(1).body, "stored body")
end)

test(":NewThought without a title is an error", function()
  local ok = pcall(vim.cmd, "NewThought")
  eq(false, ok, "ok")
  eq({}, calls(), "calls")
end)

local failed = 0
for _, t in ipairs(tests) do
  state = vim.fn.tempname()
  vim.fn.mkdir(state, "p")
  vim.env.FAKE_THOUGHT_DIR = state
  notes = {}
  local ok, err = pcall(t.fn)
  vim.cmd("silent! %bwipeout!")
  vim.fn.delete(state, "rf")
  if ok then
    io.write("ok   ", t.name, "\n")
  else
    failed = failed + 1
    io.write("FAIL ", t.name, "\n     ", tostring(err), "\n")
  end
end
io.write(("%d tests, %d failed\n"):format(#tests, failed))
os.exit(failed == 0 and 0 or 1)
