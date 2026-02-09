# Examples

This module contains runnable examples for each logger adapter and router integration.

Run from this directory:

```bash
cd cmd/examples
go run ./adapter-slog
go run ./adapter-zap
go run ./adapter-zerolog
go run ./router-std
go run ./router-gin
go run ./router-echo
go run ./router-fiber
go run ./router-fiberv3
go run ./sampling-inbuilt
go run ./sampling-custom
```

All examples now use a consistent request route shape:

- net/http: `/users/{id}`
- gin/echo/fiber: `/users/:id`

Port map:

- `adapter-slog` -> `:8101`
- `adapter-zap` -> `:8102`
- `adapter-zerolog` -> `:8103`
- `router-std` -> `:8104`
- `router-gin` -> `:8105`
- `router-echo` -> `:8106`
- `router-fiber` -> `:8107`
- `router-fiberv3` -> `:8108`
- `sampling-inbuilt` -> `:8109`
- `sampling-custom` -> `:8110`

Sample curl commands (use one after starting the matching app):

```bash
curl -i http://localhost:8101/users/u_123
curl -i http://localhost:8102/users/u_123
curl -i http://localhost:8103/users/u_123
curl -i http://localhost:8104/users/u_123
curl -i http://localhost:8105/users/u_123
curl -i http://localhost:8106/users/u_123
curl -i http://localhost:8107/users/u_123
curl -i http://localhost:8108/users/u_123
curl -i http://localhost:8109/users/u_123
curl -i http://localhost:8110/users/u_123
```

Useful variants to exercise additional API helpers:

```bash
# triggers SetLevel/GetLevel path
curl -i "http://localhost:8101/users/u_123?debug=1"

# triggers Error path (returns 500 and logs error fields)
curl -i "http://localhost:8101/users/u_123?fail=1"

# built-in sampler preset demo:
# logs VIP path immediately
curl -i "http://localhost:8109/users/vip/u_123"

# custom sampler demo:
# logs enterprise tier from event fields
curl -i "http://localhost:8110/users/u_123?tier=enterprise"
```
