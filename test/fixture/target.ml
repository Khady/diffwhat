module type S = sig
  val transform : int -> int
end

module Make (X : S) = struct
  let apply x = X.transform x
end

module Increment = struct
  let transform x = x + 1
end

module Decrement = struct
  let transform x = x - 1
end

module Alias = Increment
module Increment_app = Make (Increment)
module Decrement_app = Make (Decrement)

let direct_call x = Increment.transform x
let alias_call x = Alias.transform x
let through_functor x = Increment_app.apply x
let unrelated x = Decrement.transform x
let shadowed_call transform x = transform x
