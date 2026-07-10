---
name: module-path-mismatch
description: wintermute Go module path differs from its git repo host path
metadata: 
  node_type: memory
  type: project
  originSessionId: 11d7455b-1c91-4d97-a05f-aa90e65cabb8
---

The repo lives at `github.com/bsmr/wintermute` but the Go module path is
`go.muehmer.eu/wintermute`. Import internal packages as
`go.muehmer.eu/wintermute/internal/pkg/...`, not the github path. `go install`
target is `go.muehmer.eu/wintermute/cmd/wm@latest`.

**Why:** vanity import path decoupled from the hosting platform.
**How to apply:** when adding imports or docs, use the go.muehmer.eu path; don't
infer the import path from the github repo URL.
