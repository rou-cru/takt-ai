# Interface Contract

| Field | Holds |
| --- | --- |
| Interface name | The identifier this boundary is referenced by elsewhere |
| Direction | Which side produces and which side consumes across this boundary |
| Shape/Schema | The exact frozen data shape or call signature, machine-checkable, not prose |
| Stability | What can still change without breaking consumers, and what is frozen |
| Consumers | Which nodes or components call or read this interface |
