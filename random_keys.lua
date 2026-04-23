-- random_keys.lua
-- This script tells wrk to request a random key between 1 and 1000 on every single hit
math.randomseed(os.time())

request = function()
   -- Generate a random number to append to the key string
   local random_id = math.random(1, 1000)
   local path = "/get?key=key" .. random_id
   
   -- Return the dynamically generated GET request
   return wrk.format("GET", path)
end