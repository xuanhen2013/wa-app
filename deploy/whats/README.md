# whats.example.invalid deployment

This Compose stack runs independently from the existing `wa-app` instance.
It binds only the dashboard to `127.0.0.1:4399`, which is the upstream for
`whats.example.invalid`. It neither exposes gRPC nor shares the old instance's
container or data volume.

On the server, place the application runtime variables in `wa-app.env`, set
`WA_APP_IMAGE` to the loaded image tag, and run:

```sh
WA_APP_IMAGE=wa-app:goal-current docker compose up -d
```
