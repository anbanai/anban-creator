# Agent Runtime Network Diagnostics

Use this procedure when a managed runtime reports `network_unavailable` for
bootstrap, progress, artifact, MCP, or completion requests. Run checks from the
Agent container or Pod network; a successful request from the Server host or a
developer machine does not establish runtime reachability.

## Docker executor

Find the retained Agent container and inspect its configured server URL and
network attachment without printing its environment or workload token:

```sh
docker ps -a --filter label=anban.execution_id="$EXECUTION_ID"
docker inspect --format '{{json .NetworkSettings.Networks}}' "$AGENT_CONTAINER"
docker inspect --format '{{json .Config.Cmd}}' "$AGENT_CONTAINER"
```

Use the URL host and port from the command to check DNS and TCP from that
container:

```sh
docker exec "$AGENT_CONTAINER" node -e '
  const dns = require("node:dns").promises;
  const net = require("node:net");
  const url = new URL(process.argv[1]);
  dns.lookup(url.hostname).then((address) => {
    console.log(`dns=${address.address}`);
    const socket = net.connect(Number(url.port || (url.protocol === "https:" ? 443 : 80)), address.address);
    socket.setTimeout(5000);
    socket.once("connect", () => { console.log("tcp=connected"); socket.end(); });
    socket.once("timeout", () => { console.error("tcp=timeout"); socket.destroy(); process.exitCode = 1; });
    socket.once("error", (error) => { console.error(`tcp=${error.code || "error"}`); process.exitCode = 1; });
  }).catch((error) => { console.error(`dns=${error.code || "error"}`); process.exitCode = 1; });
' "$SERVER_URL"
```

If the runtime was already removed, start a short-lived diagnostic container
with the same runtime image and Docker network. Do not mount the task workspace
or token into it.

## Kubernetes executor

Check the configured Agent Server URL, Server Service endpoints, and egress
policy:

```sh
kubectl -n "$ANBAN_NAMESPACE" get service "$SERVER_SERVICE"
kubectl -n "$ANBAN_NAMESPACE" get endpointslices \
  -l "kubernetes.io/service-name=$SERVER_SERVICE"
kubectl -n "$ANBAN_NAMESPACE" get networkpolicy
kubectl -n "$ANBAN_NAMESPACE" describe pod "$AGENT_POD"
```

Run the same DNS/TCP check inside the Agent container using its configured
Server URL. Compare Service DNS with endpoint readiness and inspect cluster DNS,
routing, and egress policy when lookups or connections fail.

## Interpreting results

- `EAI_AGAIN` or `ENOTFOUND`: inspect runtime-network DNS and the configured
  hostname.
- `ENETUNREACH`, `EHOSTUNREACH`, or a timeout: inspect routing, network
  attachment, egress policy, and destination port reachability.
- `ECONNREFUSED`: DNS/routing reached an address, but no process accepted the
  configured port; verify the Server listener and Service endpoints.
- DNS and TCP succeed but HTTP fails: inspect Server availability, TLS and CA
  configuration, endpoint path, and authentication. Do not copy workload
  tokens into diagnostic commands or logs.

Progress updates are best-effort telemetry. A progress delivery failure should
be diagnosed from the structured runtime log fields (`operation`, `code`,
`attempts`, and, when available, the allowlisted `network_code`); it must not by
itself terminate Agent work.
