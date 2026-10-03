# wimaha 2.3.0 reference answers

`body_controller_state-*.json` are byte-for-byte copies of the fixtures of the JeedomTeslaBLE
plugin (`tests/fixtures/proxy-2.3.0/`). The answers are rebuilt from the 2.3.0 code, not captured
from a running proxy, and use a fake VIN. Keep them identical to the plugin copies.

The keys of the fixtures are in French (`requete`, `reponse`, `chemin`, `statut_http`, `corps`):
they are shared with the plugin tests and read by `body_controller_state_test.go`.
