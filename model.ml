type position = { line : int; character : int }
type range = { start : position; end_ : position }

type entity_kind =
  | Function
  | Method
  | Constructor
  | Value
  | Module
  | Type
  | Other

type entity = {
  uri : string;
  name : string;
  container : string list;
  kind : entity_kind;
  range : range;
  selection_range : range;
  provider : string;
}

type location = { uri : string; range : range }

let position_of_lsp (position : Lsp.Types.Position.t) =
  { line = position.line; character = position.character }

let range_of_lsp (range : Lsp.Types.Range.t) =
  { start = position_of_lsp range.start; end_ = position_of_lsp range.end_ }

let location_of_lsp (location : Lsp.Types.Location.t) =
  { uri = Lsp.Uri.to_string location.uri; range = range_of_lsp location.range }

let range_intersects_lines range lines =
  List.exists
    (fun line ->
      let line = line - 1 in
      range.start.line <= line
      && (line < range.end_.line
         || (line = range.end_.line && range.end_.character > 0)))
    lines
