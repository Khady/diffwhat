module type S = sig
  type t

  val open_document : t -> file:string -> unit
  val symbols : t -> file:string -> Model.entity list
  val references : t -> Model.entity -> Model.location list
end
