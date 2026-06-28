let direct value =
  Math.Nested.transform value

let through_local_alias value =
  let module Nested = Math.Nested in
  Nested.transform value

let through_local_open value =
  let open Math.Nested in
  transform value
