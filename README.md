# fleetlock-consul

A [FleetLock](https://coreos.github.io/zincati/development/fleetlock/protocol/) server backed by [Consul KV](https://developer.hashicorp.com/consul/docs/dynamic-app-config/kv).

FleetLock is a simple lock protocol used by [Zincati](https://coreos.github.io/zincati/) to coordinate reboots across a cluster of [Fedora CoreOS](https://docs.fedoraproject.org/en-US/fedora-coreos/) nodes. Only one node per group can hold the lock at a time, ensuring that rolling OS updates don't take down the entire cluster.

## How it works

A node acquires the lock before rebooting and releases it after coming back online:

```
Node-1               fleetlock-consul              Consul KV
  |                        |                           |
  |-- POST /v1/pre-reboot ->                           |
  |                        |-- session create --------> |
  |                        |-- acquire mutex lock ----> |
  |                        |-- get data key ----------> |
  |                        |   (no lock held)           |
  |                        |-- put data key "node-1" -> |
  |                        |-- release mutex lock ----> |
  |<--------- 200 OK -----|                            |
  |                        |                           |
  |   ... node reboots and comes back ...              |
  |                        |                           |
  |-- POST /v1/steady-state ->                         |
  |<--------- 200 OK -----|                            |
  |                        |                           |
  |              (background unlock cleaner)           |
  |                        |-- session create --------> |
  |                        |-- acquire mutex lock ----> |
  |                        |-- get data key ----------> |
  |                        |   (value = "node-1")       |
  |                        |-- delete data key -------> |
  |                        |-- release mutex lock ----> |
```

Meanwhile, if another node tries to lock while `node-1` holds it:

```
Node-2               fleetlock-consul              Consul KV
  |                        |                           |
  |-- POST /v1/pre-reboot ->                           |
  |                        |-- get data key ----------> |
  |                        |   (value = "node-1")       |
  |<--------- 409 --------|                            |
  |   {"kind":"failed_lock"}                           |
```

### Two-level locking

The server uses two levels of keys in Consul KV:

- **Mutex lock** (`group::lock`) - a short-lived Consul session lock that protects read-modify-write operations on the data key. TTL 15s, auto-deleted on session expiry.
- **Data lock** (`group`) - a plain KV key whose value is the node ID holding the lock. This persists until explicitly deleted on unlock.

This separation ensures that the data lock survives Consul session expiry while still providing safe concurrent access.

### Background unlock

The unlock endpoint returns `200 OK` immediately and processes the actual unlock in the background with retries. This is important because [Zincati only calls steady-state once on startup](https://coreos.github.io/zincati/usage/updates-strategy/#lock-based-strategy) - if the call fails (e.g. Consul is temporarily unreachable), the stale lock would block all other nodes indefinitely.

## Why Consul

This project is designed for clusters that already run [HashiCorp Consul](https://developer.hashicorp.com/consul) for service discovery or configuration. If you have Consul, you get a distributed lock backend for free - no additional infrastructure needed.

Good fit:
- Clusters already running Consul (Nomad + Consul, Kubernetes + Consul)
- Small to medium clusters (3-50 nodes)
- Environments where simplicity matters more than high-throughput locking

If you don't run Consul, consider [FleetLock with etcd](https://github.com/poseidon/fleetlock) or [airlock](https://github.com/coreos/airlock).

## Configuration

All settings are configured via environment variables with the `FLEETLOCK_` prefix:

| Variable                   | Default                             | Description                              |
|----------------------------|-------------------------------------|------------------------------------------|
| `FLEETLOCK_HTTP_LISTEN`    | `{private_ip}:9090, 127.0.0.1:9090` | Comma-separated listen addresses         |
| `FLEETLOCK_DEFAULT_GROUP`  | `default`                           | Default lock group                       |
| `FLEETLOCK_CONSUL_ADDRESS` | `127.0.0.1:8500`                    | Consul HTTP address                      |
| `FLEETLOCK_CONSUL_TOKEN`   |                                     | Consul ACL token                         |
| `FLEETLOCK_CONSUL_AUTH`    |                                     | Consul HTTP basic auth (`user:password`) |

## Deployment

### Docker

```bash
docker run -d --network host \
  -e FLEETLOCK_CONSUL_ADDRESS=127.0.0.1:8500 \
  axxapy/fleetlock-consul
```

### Fedora CoreOS (Butane)

```yaml
variant: fcos
version: "1.5.0"
systemd:
  units:
    - name: fleetlock.service
      enabled: true
      contents: |
        [Unit]
        Description=FleetLock server
        After=network-online.target consul.service
        Requires=network-online.target

        [Service]
        ExecStart=/usr/local/bin/fleetlock-consul
        Environment=FLEETLOCK_CONSUL_ADDRESS=127.0.0.1:8500
        Restart=always

        [Install]
        WantedBy=multi-user.target
```

Configure Zincati to use the FleetLock strategy:

```toml
[identity]
rollout_wariness = 0.5

[updates]
strategy = "fleet_lock"

[updates.fleet_lock]
base_url = "http://127.0.0.1:9090"
```

See the [Zincati fleet_lock documentation](https://coreos.github.io/zincati/usage/updates-strategy/#lock-based-strategy) for details.

## Development

```bash
make help       # list all targets
make test       # unit tests
make e2e        # end-to-end tests (docker compose)
make coverage   # test coverage report
make docker     # build docker image
```

## References

- [FleetLock protocol spec](https://coreos.github.io/zincati/development/fleetlock/protocol/)
- [Zincati update strategies](https://coreos.github.io/zincati/usage/updates-strategy/)
- [Consul KV](https://developer.hashicorp.com/consul/docs/dynamic-app-config/kv)
- [Consul sessions and locking](https://developer.hashicorp.com/consul/docs/dynamic-app-config/sessions)
