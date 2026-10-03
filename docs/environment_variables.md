# Environment variables

You can optionally set environment variables to override the default behavior.

## logLevel

This is the log level. Options: debug (Default: info)

## scanTimeout

This is the number of seconds to scan for BLE devices. If set to 0, the scan will continue until a device is found or the context is cancelled. (Default: 5) If the vehicle is sometimes not found, consider increasing this value.

## cacheMaxAge

This is the number of seconds for the HTTP Cache-Control header `max-age` value. It is used for body controller state responses to tell HTTP clients (browsers, proxies, etc.) how long they can cache the response. If set to 0, cache headers are disabled (no-cache). Note: This does not affect VehicleData caching, which uses `vehicleDataCacheTime` instead. (Default: 5)

## vehicleDataCacheTime

This is the number of seconds to cache VehicleData endpoint responses in memory. Each endpoint (e.g., `charge_state`, `climate_state`) is cached separately per VIN, allowing efficient serving of frequently requested vehicle data without establishing a BLE connection. If a request is made within the cache time, the cached response is returned immediately. If set to 0, in-memory caching is disabled. (Default: 30)

## httpListenAddress

This is the address and port to listen for HTTP requests. (Default: :8080)

## apiToken

Optional API token (default: empty, no authentication). When set, `/api/1/...` and `/api/proxy/1/...` require `Authorization: Bearer <token>` (except `/api/proxy/1/version` and `/api/proxy/1/capabilities`), and the pages, `/api/logs*` and the key routes require HTTP Basic authentication with any user name and the token as password. A blank value leaves authentication disabled. Generate a token with `openssl rand -hex 32`. Never put it in a URL. The proxy has to be restarted to apply a change.

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
```

Define `TESLA_PROXY_API_TOKEN` in your environment or `.env` file first (for example with the output of `openssl rand -hex 32`); never paste a token published in documentation.

This will set the log level to debug, the scanTimeout to 5 seconds, the HTTP cache max age to 10 seconds, the VehicleData cache time to 60 seconds, the HTTP listen address to :5687, and the API token to the value of `TESLA_PROXY_API_TOKEN`.

## Command line

You can also set the environment variables in the command line when starting the program. Example:

```
logLevel=debug scanTimeout=5 cacheMaxAge=10 vehicleDataCacheTime=60 httpListenAddress=:5687 apiToken="$TOKEN" ./TeslaBleHttpProxy
```
