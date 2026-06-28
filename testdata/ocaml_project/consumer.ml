module Arithmetic = Example.Arithmetic

let calculate value =
  Example.Arithmetic.double value

let calculate_through_alias value =
  Arithmetic.double value

let calculate_with_local_open value =
  let open Example.Arithmetic in
  double value
