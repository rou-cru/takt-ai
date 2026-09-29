# Design Tokens

| Field | Holds |
| --- | --- |
| Token name | The token's identifier — its path/key in the token tree, unique within its group |
| `$type` | The DTCG token type (e.g. color, dimension, fontFamily, duration) that defines how `$value` is structured and interpreted |
| `$value` | The token's concrete value in the shape its `$type` requires, or a reference to another token instead of a literal, when this token is an alias |
| Alias/reference | Whether `$value` points to another token (and which one, by name) rather than holding a literal — empty when this token is a literal |
| Description | What the token represents and where it traces back to in the Brand Guidelines or Style Guide it encodes — not a redefinition of it |
