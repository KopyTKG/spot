-- The columns of `ss -H -tuna`: netid, state, two queue sizes, local, peer.
return function(input, options)
  local rows = {}
  for line in input:gmatch("[^\n]+") do
    local proto, state, addr, peer = line:match("^(%a[%w_]*)%s+([%u%-%d]+)%s+%d+%s+%d+%s+(%S+)%s+(%S+)")
    if proto then
      rows[#rows + 1] = {
        -- padded, so that the peer addresses line up in monospace
        label = ("%-4s %-9s %-24s %s"):format(proto, state, addr, peer),
        key = addr,
        proto = proto,
        state_name = state,
        peer = peer,
      }
    end
  end
  -- Text that is not ss output must not look like "no sockets".
  if #rows == 0 and input:match("%S") then error("this is not output of ss -H -tuna") end
  return rows
end
