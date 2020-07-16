# CHANGELOG

## Unreleased



## v0.2.0 (2020-07-16)

- Update sse library
- Disable keep-alive on unix socket API
- Fix log for product field
- Disable HTTP keep alive on the unix socket request commands
- Add expirations per product in aggregation
- Add claims watch endpoint using HTTP SSE protocol
- Make claims-gen code importable as Go module
- Add support to delete unused listen socket on startup


## v0.1.1 (2020-07-14)

- Log a warning, when encountering license files without a sub claim


## v0.1.0 (2020-07-10)

- Embedd Kopano license trust by default
- Implement systemd sd_notify callback
- Add heartbeat and reload support
- Move API models into sub folder to allow dedicated import
- Add raw active claims API endpoint
- Add API to fetch active aggregated product claims
- Support dynamic switch between offline/online validation
- Implement license validation
- Use leaf certificate for license signing
- Improve license loading and avoid constant logging
- Implement license API on unix socket
- Build with Go 1.14.4
- Update 3rd party dependencies
- Update license ranger and generate 3rd party licenses from vendor folder
- Build with Go 1.14
- Load and parse license files
- Prepare for license loading
- Use sub in configuration but hash if it is an email
- Ignore more development stuff
- Use email in configuration instead of sub
- Add configuration file
- Use survey client to gather and send data
- Add Docker and Kubernetes instructions
- Fix formatting
- Add email info to JWT license
- Add Dockerfile to run kustomerd along with kopano-docker
- Add documentation for licenses and bootstrap


## v0.0.1 (2019-09-26)

- Add Jenkinsfile
- Add bin script and systemd service
- Make signal channel buffered
- Fix linter path
- Add README
- Initial commit

