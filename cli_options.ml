let validate ~staged ~patch ~range =
  if patch && (staged || Option.is_some range) then
    Error "--patch cannot be combined with --staged or a revision"
  else if staged && Option.is_some range then
    Error "--staged and a commit range cannot be used together"
  else Ok ()
