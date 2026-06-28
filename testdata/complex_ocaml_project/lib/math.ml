module Nested = struct
  let transform value =
    value * 2
end

module type Operation = sig
  val apply : int -> int
end

module Make (Operation : Operation) = struct
  let run value =
    Operation.apply value
end

let top_level value =
  Nested.transform value
