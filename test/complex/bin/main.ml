let wrapped_direct = Toolkit.Math.Nested.transform 4
let wrapped_reexport = Toolkit.Reexport.Nested.transform 5
let wrapped_alias = Toolkit.Reexport.Alias.transform 6
let wrapped_functor = Toolkit.Functor_ops.Runner.run 7

let () =
  Printf.printf "%d %d %d %d\n" wrapped_direct wrapped_reexport wrapped_alias
    wrapped_functor
