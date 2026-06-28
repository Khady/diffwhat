include Math
module Alias = Math.Nested

let through_include value = Nested.transform value
let through_module_alias value = Alias.transform value
