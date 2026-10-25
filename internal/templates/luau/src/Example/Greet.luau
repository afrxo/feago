--@load:shared

local module = {}

function module.greet(name: string): string
	return "Hello from " .. name .. "!"
end

return module
