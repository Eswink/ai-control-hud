# V2 Migration Issue Map

The Go migration is tracked as a new issue chain separate from the completed Python/Android milestones.

```text
G0 contract + golden fixtures
        │
        ▼
G1 Go mock agent/API
        │
        ├───────────────┐
        ▼               ▼
G2 ZCode collector   G3 CommandCode collector
        │               │
        └───────┬───────┘
                ▼
           G4 shadow parity
                ▼
           G5 Windows cutover
                ▼
      G6 service + secret store
                ▼
       G7 cross-platform release
```

The implementation branch `feat/go-agent-bootstrap` currently targets G0 and G1.
