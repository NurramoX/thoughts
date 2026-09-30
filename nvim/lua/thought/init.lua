-- Edit thoughts as `thought://<id>` buffers through the `thought` CLI (spec §10).
local M = {}

local config = { cmd = "thought" }

--- @param opts? { cmd?: string } cmd: the thought binary (default "thought")
function M.setup(opts)
  config = vim.tbl_extend("force", config, opts or {})
end

local function err(msg) vim.notify(msg, vim.log.levels.ERROR) end

--- The message for the CLI's stderr: a problem+json document (what --json
--- prints) reads as the CLI's human message; anything else passes through.
--- stale(current_version) words a 412.
local function describe(stderr, stale)
  local ok, p = pcall(vim.json.decode, stderr)
  if not ok or type(p) ~= "table" or type(p.title) ~= "string" then return stderr end
  if p.status == 412 and stale and p.current_version then return stale(p.current_version) end
  local detail = type(p.detail) == "string" and p.detail ~= "" and ": " .. p.detail or ""
  return "thought: " .. p.title .. detail
end

--- Runs the CLI synchronously. Returns the completed result, or nil after
--- reporting a failure (the CLI's stderr, or why it could not start).
--- stale words a 412, as for describe.
local function run(args, stdin, stale)
  local ok, res = pcall(function()
    return vim.system(vim.list_extend({ config.cmd }, args), { stdin = stdin, text = true }):wait()
  end)
  if not ok then
    err("thought: " .. tostring(res))
    return nil
  end
  if res.code ~= 0 then
    local msg = vim.trim(res.stderr or "")
    err(msg ~= "" and describe(msg, stale) or ("thought " .. args[1] .. " exited " .. res.code))
    return nil
  end
  return res
end

local function id_of(name) return name:match("^thought://(%d+)$") end

--- BufReadCmd: loads the thought's body byte-exact into buf.
function M.read(buf)
  local name = vim.api.nvim_buf_get_name(buf)
  local id = id_of(name)
  if not id then return err(("%s: the part after thought:// must be a decimal id"):format(name)) end
  local res = run({ "show", "--json", id })
  if not res then return end
  local thought = vim.json.decode(vim.split(res.stdout, "\n", { plain = true })[1])

  local eol = thought.body:sub(-1) == "\n"
  local lines = vim.split(eol and thought.body:sub(1, -2) or thought.body, "\n", { plain = true })

  local bo = vim.bo[buf]
  local undolevels = bo.undolevels
  bo.undolevels = -1 -- loading is not an undoable change
  vim.api.nvim_buf_set_lines(buf, 0, -1, true, lines)
  bo.undolevels = undolevels
  bo.fileformat = "unix"
  bo.eol = eol
  bo.fixeol = eol
  bo.buftype = "acwrite"
  bo.swapfile = false
  bo.modified = false
  vim.b[buf].thought_version = thought.version
  vim.b[buf].thought_title = thought.title
  bo.filetype = "markdown"
end

--- BufWriteCmd: writes buf to the thought named by target, guarded by the
--- Version it was read at. Never forces; on failure buf stays modified.
function M.write(buf, target)
  local id = id_of(target)
  if not id then return err(("%s: the part after thought:// must be a decimal id"):format(target)) end
  local version = vim.b[buf].thought_version
  if target ~= vim.api.nvim_buf_get_name(buf) or not version then
    return err(("%s: only the buffer loaded from it can be written there"):format(target))
  end
  local body = table.concat(vim.api.nvim_buf_get_lines(buf, 0, -1, true), "\n")
  if vim.bo[buf].eol then body = body .. "\n" end
  local res = run({ "write", id, "--version", tostring(version), "--json" }, body, function(current)
    return ("thought %s changed: you had version %d, it is now %d (:e! reloads it, dropping your changes)"):format(
      id, version, current)
  end)
  if not res then return end
  vim.b[buf].thought_version = vim.json.decode(res.stdout).version
  vim.bo[buf].modified = false
end

--- :NewThought: captures a thought with this title and opens it.
function M.new(title)
  title = vim.trim(title)
  if title == "" then return err("NewThought: a title is required") end
  local res = run({ "add", "--", title })
  if not res then return end
  local id = vim.trim(res.stdout)
  if not id:match("^%d+$") then return err("NewThought: thought add printed no id: " .. id) end
  vim.cmd.edit(vim.fn.fnameescape("thought://" .. id))
end

return M
