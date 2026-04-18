INSERT INTO schemas (id, json_schema) VALUES
('email.v1', $json$
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "email.v1",
  "type": "object",
  "required": ["messageId", "mailboxUser", "from", "to", "subject", "receivedAt"],
  "additionalProperties": false,
  "properties": {
    "messageId":   { "type": "string", "minLength": 1 },
    "mailboxUser": { "type": "string", "format": "email" },
    "from":        { "type": "string", "format": "email" },
    "to":          { "type": "array", "items": { "type": "string", "format": "email" }, "minItems": 1 },
    "cc":          { "type": "array", "items": { "type": "string", "format": "email" } },
    "subject":     { "type": "string" },
    "bodyText":    { "type": "string" },
    "bodyHtml":    { "type": "string" },
    "receivedAt":  { "type": "string", "format": "date-time" },
    "headers":     { "type": "object", "additionalProperties": { "type": "string" } }
  }
}
$json$)
ON CONFLICT (id) DO NOTHING;

INSERT INTO schemas (id, json_schema) VALUES
('draft.v1', $json$
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "title": "draft.v1",
  "type": "object",
  "required": ["inReplyToDocumentId", "to", "subject", "body", "generatedByAgent", "generatedAt", "status"],
  "additionalProperties": false,
  "properties": {
    "inReplyToDocumentId": { "type": "string", "format": "uuid" },
    "to":                  { "type": "array", "items": { "type": "string", "format": "email" }, "minItems": 1 },
    "cc":                  { "type": "array", "items": { "type": "string", "format": "email" } },
    "subject":             { "type": "string" },
    "body":                { "type": "string" },
    "generatedByAgent":    { "type": "string", "minLength": 1 },
    "generatedAt":         { "type": "string", "format": "date-time" },
    "status":              { "type": "string", "enum": ["pending", "sending", "approved", "rejected", "sent", "error"] },
    "sentMessageId":       { "type": "string" }
  }
}
$json$)
ON CONFLICT (id) DO NOTHING;
