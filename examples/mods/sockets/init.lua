-- sockets.table      every socket, listening ones first, then by local port
-- sockets.listening  only what is listening or bound
-- sockets.count      for a bar: the total, with a breakdown
local parse = spot.use("ss")

local function listening(s) return s.state_name == "LISTEN" or s.state_name == "UNCONN" end
local function port(s) return tonumber(s.key:match(":(%d+)$")) or 0 end
local function rank(s) return listening(s) and 0 or (s.state_name == "ESTAB" and 1 or 2) end

local function sockets()
  local r = spot.run({ "ss", "-H", "-tuna" })
  if r.status ~= 0 then error("ss: " .. r.stderr) end
  local all = parse(r.stdout)
  table.sort(all, function(a, b)
    if rank(a) ~= rank(b) then return rank(a) < rank(b) end
    if port(a) ~= port(b) then return port(a) < port(b) end
    return a.label < b.label
  end)
  return all
end

local function rows(only)
  local out = {}
  for _, s in ipairs(sockets()) do
    if not only or only(s) then out[#out + 1] = { label = s.label, key = s.key } end
  end
  return out
end

spot.value("table", { ttl = 2000 }, function() return rows() end)
spot.value("listening", { ttl = 2000 }, function() return rows(listening) end)

spot.value("count", { every = 5000 }, function()
  local n = { total = 0, listening = 0, established = 0, udp = 0 }
  for _, s in ipairs(sockets()) do
    n.total = n.total + 1
    if listening(s) then n.listening = n.listening + 1 end
    if s.state_name == "ESTAB" then n.established = n.established + 1 end
    if s.proto == "udp" then n.udp = n.udp + 1 end
  end
  return {
    text = tostring(n.total),
    tooltip = ("%d sockets\n  %d listening\n  %d established\n  %d UDP"):format(n.total, n.listening, n.established, n.udp),
    class = "sockets",
  }
end)
