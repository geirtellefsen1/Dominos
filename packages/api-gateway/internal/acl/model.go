package acl

// DominionModel is the FGA authorization model from spec §3.3:
//
//     type user
//     type agent
//     type document
//       relations
//         define owner: [user, agent]
//         define reader: [user, agent] or owner
//         define writer: [user, agent] or owner
//
// Expressed as the FGA v1.1 JSON model so we can POST it verbatim to
// /stores/{id}/authorization-models without bringing in the DSL parser.
const DominionModelJSON = `{
  "schema_version": "1.1",
  "type_definitions": [
    { "type": "user" },
    { "type": "agent" },
    {
      "type": "document",
      "relations": {
        "owner":  { "this": {} },
        "reader": {
          "union": {
            "child": [
              { "this": {} },
              { "computedUserset": { "relation": "owner" } }
            ]
          }
        },
        "writer": {
          "union": {
            "child": [
              { "this": {} },
              { "computedUserset": { "relation": "owner" } }
            ]
          }
        }
      },
      "metadata": {
        "relations": {
          "owner":  { "directly_related_user_types": [{"type":"user"},{"type":"agent"}] },
          "reader": { "directly_related_user_types": [{"type":"user"},{"type":"agent"}] },
          "writer": { "directly_related_user_types": [{"type":"user"},{"type":"agent"}] }
        }
      }
    }
  ]
}`

// Relations we rely on in code.
const (
	RelOwner  = "owner"
	RelReader = "reader"
	RelWriter = "writer"
)

// Types we rely on in code.
const (
	TypeDocument = "document"
	TypeUser     = "user"
	TypeAgent    = "agent"
)
