module Increment = struct
  let apply value =
    value + 1
end

module Runner = Math.Make (Increment)

let through_functor value =
  Runner.run value
