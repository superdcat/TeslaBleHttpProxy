# TeslaBleHttpProxy (superdcat fork)

> **This is a maintained fork** of [wimaha/TeslaBleHttpProxy](https://github.com/wimaha/TeslaBleHttpProxy), with changes
> ported from [Lenart12/TeslaBleHttpProxy](https://github.com/Lenart12/TeslaBleHttpProxy) (see [NOTICE](NOTICE)).
> Docker image: **`ghcr.io/superdcat/tesla-ble-http-proxy`** (linux/amd64, arm64, arm/v7, arm/v6).
>
> - **Drop-in replacement for wimaha 2.3.0**: same routes, same response envelope, same accepted bodies — evcc and the
>   Jeedom plugin [Tesla BLE](https://jeedomdocs.decastro.fr/teslable/) keep working unchanged. Changes are additions only.
> - Built on the latest official [vehicle-command](https://github.com/teslamotors/vehicle-command) SDK.
> - Adds: strict request body validation (no more false success), `GET /api/proxy/1/capabilities`, optional API token,
>   `body_controller_state` through the BLE queue, abandoned requests no longer executed, and more vehicle commands
>   (climate, seats, trunks, charge schedules…) and data endpoints.
> - Versions are `X.Y.Z-tb.N` (wimaha base version + fork release number). `latest` = last release, `edge` = last build
>   of `main` (for testing).

TeslaBleHttpProxy is a program written in Go that receives HTTP requests and forwards them via Bluetooth to a Tesla vehicle. The program can, for example, be easily used together with [evcc](https://github.com/evcc-io/evcc).

The program stores the received requests in a queue and processes them one by one. This ensures that only one Bluetooth connection to the vehicle is established at a time.

## Table of Contents

- [How to install](#how-to-install)
  - [Docker compose](#docker-compose)
  - [Build yourself](#build-yourself)
- [Generate key for vehicle](#generate-key-for-vehicle)
- [Setup EVCC](#setup-evcc)
- [Authentication (optional, superdcat fork)](#authentication-optional-superdcat-fork)
- [API](#api)
  - [Vehicle Commands](#vehicle-commands)
  - [Vehicle Data](#vehicle-data)
  - [Body Controller State](#body-controller-state)
  - [Version of Proxy](#version-of-proxy)
  - [Capabilities of Proxy (superdcat fork)](#capabilities-of-proxy-superdcat-fork)
- [Troubleshooting](#troubleshooting)

## How to install

You can either compile and use the Go program yourself or install it in a Docker container. ([detailed instruction](docs/installation.md))

### Docker compose

Below you will find the necessary contents for your `docker-compose.yml`:

```
services:
  tesla-ble-http-proxy:
    image: ghcr.io/superdcat/tesla-ble-http-proxy:latest   # or pin a release: ghcr.io/superdcat/tesla-ble-http-proxy:X.Y.Z-tb.N
    container_name: tesla-ble-http-proxy
    volumes:
      - ~/TeslaBleHttpProxy/key:/key
      - /var/run/dbus:/var/run/dbus
    restart: always
    privileged: true
    network_mode: host
    cap_add:
      - NET_ADMIN
      - SYS_ADMIN
```

Please remember to create an empty folder where the keys can be stored later. In this example, it is `~/TeslaBleHttpProxy/key`.

Pull and start TeslaBleHttpProxy with `docker compose up -d`.

**Migrating from the wimaha image:** only change the `image:` line, then run `docker compose pull && docker compose up -d`.
The `key` folder is kept: no new pairing is needed. To go back, restore the previous `image:` line.

**Updates:** restarting the Pi does **not** update the image. Run `docker compose pull && docker compose up -d` (with a
pinned tag, change the tag first). Release notes: [Releases](https://github.com/superdcat/TeslaBleHttpProxy/releases).

Note that you can optionally set environment variables to override the default behavior. See [environment variables](docs/environment_variables.md) for more information.

**Key Security:** Private keys are protected by UNIX file permissions (0600 - owner read/write only). Ensure the key directory has proper permissions and is not accessible to unauthorized users.

### Build yourself

Download the code and save it in a folder named 'TeslaBleHttpProxy'. From there, you can easily compile the program.

```
go build .
./TeslaBleHttpProxy
```

Please remember to create an empty folder called `key` where the keys can be stored later.

Note that you can optionally set environment variables to override the default behavior. See [environment variables](docs/environment_variables.md) for more information.

## Generate key for vehicle

*(Here, the simple, automatic method is described. Besides the automatic method, you can also generate the keys [manually](docs/manually_gen_key.md).)*

**Security Recommendation:** We recommend using the **Charging Manager** role for security. It provides limited access suitable for charging management and works perfectly with [evcc tesla-ble template](https://docs.evcc.io/docs/devices/vehicles#tesla-ble). The Charging Manager role can:
- Read vehicle data
- Authorize charging-related commands: `wake`, `charge_start`, `charge_stop`, `set_charging_amps`

The **Owner** role provides full access to all vehicle functions (unlock, start, etc.) and should only be used if you need non-charging functions.

To generate the required keys browse to `http://YOUR_IP:8080/dashboard`. In the dashboard you will see that the keys are missing:

<img src="docs/proxy1.png" alt="Picture of the Dashboard with missing keys." width="40%" height="40%" style="box-shadow: 0 0 10px rgba(0, 0, 0, 0.1); margin-bottom: 10px;">

Please click on `Generate` for the **Charging Manager** role (recommended for security). The keys will be automatically generated and saved. The Charging Manager key will be set as active by default.

<img src="docs/proxy2b.png" alt="Picture of the Dashboard with success message and keys." width="40%" height="40%"><br/>
<img src="docs/proxy2.png" alt="Picture of the Dashboard with success message and keys." width="40%" height="40%" style="box-shadow: 0 0 10px rgba(0, 0, 0, 0.1); margin-bottom: 10px;">

After that please enter your VIN under `Setup Vehicle`. Before you proceed make sure your vehicle is awake! So you have to manually wake the vehicle before you send the key to the vehicle.

<img src="docs/proxy3.png" alt="Picture of Setup Vehicle Part of the Dashboard." width="40%" height="40%" style="box-shadow: 0 0 10px rgba(0, 0, 0, 0.1); margin-bottom: 10px;">

The key is now sent to the vehicle. To complete the process, confirm by tapping your NFC card on the center console. (Note: There will be no message on the Tesla screen before confirmation with the NFC card.)

<img src="docs/proxy6.png" alt="Picture of success message sent add-key request." width="40%" height="40%">

You can now close the dashboard and use the proxy. 🙂

## Setup EVCC

You can use the following configuration in evcc (recommended):

```
vehicles:
  - name: tesla
    type: template
    template: tesla-ble
    title: Your Tesla (optional)
    capacity: 60 # Akkukapazität in kWh (optional)
    vin: VIN # Erforderlich für BLE-Verbindung
    url: IP # URL des Tesla BLE HTTP Proxy
    port: 8080 # Port des Tesla BLE HTTP Proxy (optional)
```

If you want to use this proxy only for commands, and not for vehicle data, you can use the following configuration. The vehicle data is then fetched via the Tesla API by evcc.

```
- name: model3
    type: template
    template: tesla
    title: Tesla
    icon: car
    commandProxy: http://YOUR_IP:8080
    accessToken: YOUR_ACCESS_TOKEN
    refreshToken: YOUR_REFRSH_TOKEN
    capacity: 60
    vin: YOUR_VIN
```

(Hint for multiple vehicle support: https://github.com/wimaha/TeslaBleHttpProxy/issues/40)

With the optional `apiToken` (see [Authentication](#authentication-optional-superdcat-fork)) the `tesla-ble` template stops working, because it has no parameter to send an `Authorization` header, and `commandProxy` is not compatible either. Leave `apiToken` unset if you use evcc.

## Authentication (optional, superdcat fork)

By default the proxy has no authentication: anyone who can reach its port can send commands to the vehicle. Set the environment variable `apiToken` to protect it (generate a token with `openssl rand -hex 32`; see [environment variables](docs/environment_variables.md#apitoken)). When it is empty or unset, nothing changes. A change needs a restart.

With a token set:

- `/api/1/...` and `/api/proxy/1/...` require `Authorization: Bearer <token>`, except `/api/proxy/1/version` and `/api/proxy/1/capabilities`, which stay open.
- The pages (`/dashboard`, `/logs`), `/api/logs*` and the key routes (`/gen_keys`, `/remove_keys`, `/activate_key`, `/send_key`) require HTTP Basic authentication: any user name, the token as password. A browser asks for it.
- `/` (redirect) and `/static/` stay open.
- A refused request gets HTTP 401, the usual envelope with `"reason":"unauthorized"` and a `WWW-Authenticate` header. Each refusal is logged (method, route, client address; never the credentials).

Examples (leading and trailing spaces of the token are ignored):

```
curl -H "Authorization: Bearer $TOKEN" http://IP:8080/api/1/vehicles/VIN/vehicle_data
curl -u any:$TOKEN "http://IP:8080/api/logs"
```

Never put the token in a URL. The proxy speaks plain HTTP: use a reverse proxy with TLS if the network is not trusted.

## API

### Vehicle Commands

The program uses the same interfaces as the Tesla [Fleet API](https://developer.tesla.com/docs/fleet-api#vehicle-commands). Currently, the following requests are supported: 

- wake_up
- charge_start
- charge_stop
- set_charging_amps
- set_charge_limit
- auto_conditioning_start
- auto_conditioning_stop
- charge_port_door_open
- charge_port_door_close
- flash_lights
- honk_horn
- door_lock
- door_unlock
- set_sentry_mode
- set_temps (superdcat fork)
- set_preconditioning_max (superdcat fork)
- set_climate_keeper_mode (superdcat fork)
- set_cabin_overheat_protection (superdcat fork)
- set_cop_temp (superdcat fork)
- set_bioweapon_mode (superdcat fork)
- remote_seat_heater_request (superdcat fork)
- remote_seat_cooler_request (superdcat fork)
- remote_auto_seat_climate_request (superdcat fork)
- remote_steering_wheel_heater_request (superdcat fork)
- actuate_trunk (superdcat fork)
- window_control (superdcat fork)

By default, the program will return immediately after sending the command to the vehicle. If you want to wait for the command to complete, you can set the `wait` parameter to `true`.

**Wake Up Behavior:** Commands **automatically wake up** the vehicle if it is asleep. You don't need to manually wake the vehicle or use any parameters - the proxy handles this automatically to ensure commands execute successfully.

#### Example Request

*(All requests with method POST.)*

Start charging:
`http://localhost:8080/api/1/vehicles/{VIN}/command/charge_start`

Start charging and wait for the command to complete:
`http://localhost:8080/api/1/vehicles/{VIN}/command/charge_start?wait=true`

Stop charging:
`http://localhost:8080/api/1/vehicles/{VIN}/command/charge_stop`

Set charging amps to 5A:
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_charging_amps` with body `{"charging_amps": "5"}`

Explicitly wake up the vehicle:
`http://localhost:8080/api/1/vehicles/{VIN}/command/wake_up`

Set the cabin temperature (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_temps` with body `{"driver_temp": 21.5, "passenger_temp": 20}`

`driver_temp` is required; `passenger_temp` is optional and defaults to the driver setpoint. Values are in degrees Celsius (15 to 28, inclusive), whatever the region of the vehicle, as a number or a numeric string with a decimal point (`"21.5"`, not `"21,5"`). Other values are refused with HTTP 503 before the command is queued. These commands need the Owner role (a Charging Manager key is refused by the vehicle).

Start or stop maximum preconditioning (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_preconditioning_max` with body `{"on": true}`

`on` is required; `manual_override` is optional and defaults to `false`.

Set the climate keeper mode, dog or camp mode (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_climate_keeper_mode` with body `{"climate_keeper_mode": 2}`

`climate_keeper_mode` is required: 0 off, 1 on, 2 dog, 3 camp (an integer or an integer string; fractions and other values are refused with HTTP 503 before the command is queued). `manual_override` is ignored by this command: the proxy always sends `true`.

Enable or disable cabin overheat protection (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_cabin_overheat_protection` with body `{"on": true, "fan_only": false}`

`on` is required; `fan_only` is optional and defaults to `false`.

Set the cabin overheat protection temperature (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_cop_temp` with body `{"cop_temp": 1}`

`cop_temp` is required: 0 = 30 C (90 F), 1 = 35 C (95 F), 2 = 40 C (100 F), as documented in the Tesla [Fleet API](https://developer.tesla.com/docs/fleet-api/endpoints/vehicle-commands) (`set_cop_temp`). This command only sets the threshold; it does not turn the protection on (use `set_cabin_overheat_protection`).

Enable or disable bioweapon defense mode (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/set_bioweapon_mode` with body `{"on": true}`

`on` is required; `manual_override` is optional and defaults to `false`.

These four commands are expected to need the Owner role (a Charging Manager key should be refused by the vehicle).

Heat a seat (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/remote_seat_heater_request` with body `{"heater": 0, "level": 3}`

`heater` and `level` are required. `heater` is 0 front left, 1 front right, 2 second row left, 3 second row left back, 4 second row center, 5 second row right, 6 second row right back, 7 third row left, 8 third row right. `level` is 0 off, 1 low, 2 medium, 3 high.

Ventilate a front seat (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/remote_seat_cooler_request` with body `{"seat_position": 1, "seat_cooler_level": 2}`

`seat_position` is 1 front left or 2 front right (numbering starts at 1, unlike `heater`, as in the Fleet API and Home Assistant). `seat_cooler_level` is 0 off, 1 low, 2 medium, 3 high: the proxy follows the Fleet API, whereas the official Tesla proxy subtracts 1 from it (vehicle-command issue #50).

Switch the automatic seat and climate mode of a front seat (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/remote_auto_seat_climate_request` with body `{"auto_seat_position": 1, "auto_climate_on": true}`

`auto_seat_position` is 1 front left or 2 front right; `auto_climate_on` is required.

Heat the steering wheel (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/remote_steering_wheel_heater_request` with body `{"on": true}`

All the keys are required. The integer keys (`heater`, `level`, `seat_position`, `seat_cooler_level`, `auto_seat_position`) accept an integer or an integer string (`2.0` is accepted); fractions and out-of-range values are refused with HTTP 503 before the command is queued. `auto_climate_on` and `on` accept a boolean or `"true"`/`"false"`. There is no alias: `remote_seat_heater_request` does not read `seat_position`. The climate must be on, as documented by the Fleet API. Seats of the third row and the second row backrests only exist on equipped models; otherwise the vehicle refuses the command. These commands are expected to need the Owner role (a Charging Manager key should be refused by the vehicle).

Open the rear trunk or the frunk (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/actuate_trunk` with body `{"which_trunk": "rear"}` or `{"which_trunk": "front"}`

`which_trunk` is required (there is no default: an empty body is refused with HTTP 503 before the command is queued). `rear` **toggles** the trunk: it opens a closed trunk and closes an open power trunk, so read `body_controller_state` (`closure_statuses.rear_trunk`) before sending it again. A failure of `rear` is **never retried** (a retry after a lost reply would undo the opening) and may close the BLE connection (the next command then reconnects, which takes a few seconds); with `wait=false` no error is reported at all, so do not resend blindly. `front` opens the frunk; there is no remote way to close it, and it keeps the 3 attempts of the other commands.

Vent or close the windows (superdcat fork):
`http://localhost:8080/api/1/vehicles/{VIN}/command/window_control` with body `{"command": "vent"}` or `{"command": "close"}`

`command` is required. `lat` and `lon` (degrees, `lat` -90 to 90, `lon` -180 to 180) are accepted for compatibility with the Fleet API and validated, but **not sent** to the vehicle, which needs no position over BLE: omit them. A request body is logged by the proxy, so avoid sending coordinates. These two commands wake the vehicle up like the others, are expected to need the Owner role (a Charging Manager key should be refused by the vehicle), and open the vehicle to anyone who can reach the proxy: set an `apiToken`. Values of `which_trunk` and `command` are case-insensitive and surrounding spaces are ignored; anything else is refused with HTTP 503.

### Vehicle Data

The vehicle data is fetched from the vehicle and returned in the response in the same format as the [Fleet API](https://developer.tesla.com/docs/fleet-api/endpoints/vehicle-endpoints#vehicle-data). Since a ble connection has to be established to fetch the data, it takes a few seconds before the data is returned.

**Caching:** VehicleData responses are cached in memory for faster subsequent requests. Each endpoint (e.g., `charge_state`, `climate_state`) is cached separately per VIN. The cache time can be configured via the `vehicleDataCacheTime` environment variable (default: 30 seconds). If all requested endpoints are cached and valid, the response is returned immediately without establishing a BLE connection.

**Wake Up Behavior:** By default, the car is **not** automatically woken up before fetching vehicle data. This allows for efficient data retrieval when the vehicle is already awake. If your vehicle is asleep and you need to wake it up first, you can use the `wakeup=true` parameter. The proxy uses intelligent caching to minimize unnecessary wakeup calls - if the vehicle was confirmed awake within the last 9 minutes, the sleep status check is skipped.

#### Example Request

*(All requests with method GET.)*

Get vehicle data:
`http://localhost:8080/api/1/vehicles/{VIN}/vehicle_data`

Get vehicle data with automatic wakeup:
`http://localhost:8080/api/1/vehicles/{VIN}/vehicle_data?wakeup=true`

Currently you will receive the following data:

- charge_state
- climate_state

If you want to receive specific data, you can add the endpoints to the request. For example:

`http://localhost:8080/api/1/vehicles/{VIN}/vehicle_data?endpoints=charge_state`

Get specific data with automatic wakeup:
`http://localhost:8080/api/1/vehicles/{VIN}/vehicle_data?endpoints=charge_state&wakeup=true`

This is recommended if you want to receive data frequently, since it will reduce the time it takes to receive the data.

### Body Controller State

The body controller state is fetched from the vehicle and returnes the state of the body controller. The request does not wake up the vehicle. In this fork it goes through the BLE command queue (VCSEC domain): it waits for the command in progress and reuses its connection for the same VIN, and answers `503` with `context deadline exceeded` when the queue stays busy for more than 15 seconds (`features.body_controller_state_queued` in the capabilities). The following information is returned:

- `vehicleLockState`
  - `VEHICLELOCKSTATE_UNLOCKED`
  - `VEHICLELOCKSTATE_LOCKED`
  - `VEHICLELOCKSTATE_INTERNAL_LOCKED`
  - `VEHICLELOCKSTATE_SELECTIVE_UNLOCKED`
- `vehicleSleepStatus`
  - `VEHICLE_SLEEP_STATUS_UNKNOWN`
  - `VEHICLE_SLEEP_STATUS_AWAKE`
  - `VEHICLE_SLEEP_STATUS_ASLEEP`
- `userPresence`
  - `VEHICLE_USER_PRESENCE_UNKNOWN`
  - `VEHICLE_USER_PRESENCE_NOT_PRESENT`
  - `VEHICLE_USER_PRESENCE_PRESENT`

#### Request

*(All requests with method GET.)*

Get body controller state:
`http://localhost:8080/api/1/vehicles/{VIN}/body_controller_state`

### Version of Proxy

Get version of proxy:
`http://localhost:8080/api/proxy/1/version`

The response will contain the version of the proxy (`version`) and, in this fork, the `flavor` (`superdcat`).

### Capabilities of Proxy (superdcat fork)

Get what this proxy supports:
`http://localhost:8080/api/proxy/1/capabilities`

The route answers without a vehicle, without Bluetooth and without an installed key. The `response` object contains:

- `api`: version of the `/api/proxy/1/` routes (stays 1 while changes only add things).
- `version`, `flavor`: as in the version route.
- `commands`: the Fleet vehicle commands supported by the proxy (the legacy route commands `vehicle_data` and `session_info` are not listed).
- `vehicle_data_endpoints`: the endpoints accepted by `vehicle_data`.
- `proxy_routes`: the proxy-specific routes under `/api/proxy/1/` (last path segment).
- `features`: `strict_body_validation`, `body_controller_state_queued`, `auth_required`.
- `key_role`: role of the active key (`owner` or `charging_manager`), or an empty string when no key is installed for it.

`auth_required` is `true` when `apiToken` is set. The version and capabilities routes stay open with a token (`key_role` included), so that a client can discover the proxy before sending its token.

The lists are sorted but must be read as sets (the order is not part of the contract). The wimaha and Lenart12 proxies answer 404 on this route.

## Troubleshooting

### Vehicle Requirements

TeslaBleHttpProxy requires your Tesla vehicle to support **Phone Key** functionality, as it relies on Bluetooth Low Energy (BLE) for communication. Most Tesla models from 2021 onward support Phone Key, but some older models (e.g., Model X 2015–2020) may not. Please verify Phone Key support in your Tesla app or consult Tesla's [Vehicle Keys Support Page](https://www.tesla.com/support/tesla-vehicle-keys) before setting up the proxy.

### Connection Timeouts

Due to BLE's power-saving design, Tesla vehicles may terminate connections after ~30 seconds, causing "connection timeout" logs. This is normal, and the proxy reconnects automatically, ensuring EVCC or other integrations work without issues. Keep the proxy device within ~5-10 meters of the vehicle for reliable connections.

### BLE Device Limit (Maximum 3 Devices)

**Problem:** Intermittent connection failures, undefined loss of connections, or unreliable behavior with evcc.

**Solution:** Tesla vehicles only accept a **maximum of 3 BLE keys/devices simultaneously**. If you exceed this limit (e.g., multiple phones, smartwatches, and the proxy), you'll experience connection issues.

**Fix:** Close the Tesla app on mobile phones when not needed, especially if you're also wearing a smartwatch with the Tesla app. The proxy counts as one device, so limit other active BLE connections accordingly.

This is a Tesla vehicle constraint, not a limitation of TeslaBleHttpProxy.
