return function(input, options)
  local rows = {}
  for line in input:gmatch("[^\n]+") do
    -- netid, state, two queue sizes (numbers), local and peer address
    local proto, state, addr, peer = line:match("^(%a[%w_]*)%s+([%u%-%d]+)%s+%d+%s+%d+%s+(%S+)%s+(%S+)")
    if proto then
      rows[#rows + 1] = { label = ("%-4s %-9s %s  %s"):format(proto, state, addr, peer), key = addr }
    end
  end
  -- Text that is not ss output must not look like "no sockets".
  if #rows == 0 and input:match("%S") then error("this is not output of ss -H -tuna") end
  return rows
end
