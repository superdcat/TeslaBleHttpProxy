# Environment variables

You can optionally set environment variables to override the default behavior.

## logLevel

This is the log level. Options: debug (Default: info)

## scanTimeout

This is the number of seconds to scan for BLE devices. If set to 0, the scan will continue until a device is found or the context is cancelled. (Default: 5) If the vehicle is sometimes not found, consider increasing this value. It also bounds the scan of the `connection_status` route (5 seconds when set to 0, which would otherwise never end); above about 13 seconds a vehicle that is not seen makes that route answer `503 context deadline exceeded`, because the request is limited to 15 seconds.

## cacheMaxAge

This is the number of seconds for the HTTP Cache-Control header `max-age` value. It is used for body controller state responses to tell HTTP clients (browsers, proxies, etc.) how long they can cache the response. If set to 0, cache headers are disabled (no-cache). Note: This does not affect VehicleData caching, which uses `vehicleDataCacheTime` instead. (Default: 5)

## vehicleDataCacheTime

This is the number of seconds to cache VehicleData endpoint responses in memory. Each endpoint (e.g., `charge_state`, `climate_state`) is cached separately per VIN, allowing efficient serving of frequently requested vehicle data without establishing a BLE connection. If a request is made within the cache time, the cached response is returned immediately. If set to 0, in-memory caching is disabled. (Default: 30)

## httpListenAddress

This is the address and port to listen for HTTP requests. (Default: :8080)

## apiToken

Optional API token (default: empty, no authentication). When set, `/api/1/...` and `/api/proxy/1/...` require `Authorization: Bearer <token>` (except `/api/proxy/1/version` and `/api/proxy/1/capabilities`), and the pages, `/api/logs*` and the key routes require HTTP Basic authentication with any user name and the token as password. A blank value leaves authentication disabled. Generate a token with `openssl rand -hex 32`. Never put it in a URL. The proxy has to be restarted to apply a change. Setting it is recommended: without it, anyone who can reach the proxy can send commands and read the vehicle data, including the position (`location_data`) and the location of the charge and preconditioning schedules (`charge_schedule_data`, `preconditioning_schedule_data`); the proxy logs a reminder at startup when it is unset.

## btAdapter

Bluetooth adapter to use, from `hci0` to `hci15` (lower case, no leading zero; for example `hci1`). Default: empty, the default adapter, opened at the first scan as before. When set, the adapter is opened at startup: an invalid value, or an adapter that does not exist or cannot be opened, stops the proxy with an error naming the value (check the names with `btmgmt info` or `hciconfig -a`). Linux only. Opening the adapter needs the `CAP_NET_ADMIN` capability, as for the default adapter.

## connectionTimeout

Number of seconds a BLE connection stays open after a command, from 10 to 120 (Default: 29, the value of 2.3.0). The delay is counted from the opening of the connection and is not restarted by the commands that follow; the first command always keeps a 29 second budget, even with a shorter value. A command for another VIN closes the connection at once, so a longer value does not delay the other vehicles, but the vehicle keeps a BLE slot busy longer. An invalid value is replaced by 29 and a warning names it. The `operated` flag of `connection_status` and the age of its `rssi` follow this value.

## releaseAdapterWhenIdle

When `true` (also accepted: `1`, `t`, `TRUE`; not `yes` or `on`), the Bluetooth adapter is given back to the system (BlueZ) once the queue is idle: after the connection of the last command is closed, with no command waiting or to retry, and after an adapter key send from the dashboard. It is reopened (`btAdapter`, or the default adapter) at the next scan, which adds a few moments to each first command. Default: `false`. Because the adapter is only given back when the connection closes, that is at least `connectionTimeout` seconds after it opened: set `connectionTimeout=10` for a shared adapter that has to be free quickly. Limits: if the Bluetooth controller does not answer, closing it can block; if closing fails (a warning is logged) restart the proxy. Whether BlueZ sees the adapter again after the release depends on the Bluetooth library (`btmgmt info` tells), so check it on your setup before relying on it. An invalid value is ignored (release stays disabled) and a warning names it. With btAdapter, the adapter is opened at startup to check it, then given back at once until the first command.

# Example

## Docker compose
You can set the environment variables in your docker-compose.yml file. Example:

```
environment:
  - logLevel=debug
  - scanTimeout=5
  - cacheMaxAge=10
  - vehicleDataCacheTime=60
  - httpListenAddress=:5687
  - apiToken=${TESLA_PROXY_API_TOKEN}
  - btAdapter=hci1
  - connectionTimeout=10
  - releaseAdapterWhenIdle=true
```

Define `TESLA_PROXY_API_TOKEN` in your environment or `.env` file first (for example with the output of `openssl rand -hex 32`); never paste a token published in documentation.

This will set the log level to debug, the scanTimeout to 5 seconds, the HTTP cache max age to 10 seconds, the VehicleData cache time to 60 seconds, the HTTP listen address to :5687, the API token to the value of `TESLA_PROXY_API_TOKEN`, the Bluetooth adapter to `hci1` (shared with another service: given back when idle, connections kept 10 seconds).

## Command line

You can also set the environment variables in the command line when starting the program. Example:

```
logLevel=debug scanTimeout=5 cacheMaxAge=10 vehicleDataCacheTime=60 httpListenAddress=:5687 apiToken="$TOKEN" btAdapter=hci1 connectionTimeout=10 releaseAdapterWhenIdle=true ./TeslaBleHttpProxy
```
