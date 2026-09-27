# Running advance-wars (prebuilt binary)

Prebuilt Linux binaries of the **advance-wars** NEX secure server for a home-lab LAN.

1. Download `advance-wars-linux-amd64` (PC/VPS) or `advance-wars-linux-arm64` (Pi/ARM).
2. Put it beside this branch's runtime files (`cert.pem`, `key.pem`,
   `nextendo_secret.key`). The server reads them from the working directory.
3. Run it (AUTH_PORT 443 needs privilege; override for a local run):

```sh
chmod +x advance-wars-linux-amd64
sudo ./advance-wars-linux-amd64            # or: AUTH_PORT=8443 ./advance-wars-linux-amd64
```

Or from a clone: `./run.sh`. Dashboard: `http://<host>:8097`. TLS self-signed
cert is accepted under Prelude/nextendo-nx's `disable_ca_verification`.
