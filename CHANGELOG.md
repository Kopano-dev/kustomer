# CHANGELOG

## Unreleased



## v0.5.0 (2021-04-09)

- Cleanup and add args for Dockerfile.release
- Build with Go 1.16.3
- Improve code error/exit behavior when loading license files
- Avoid duplicated log output introduced while refactoring
- Add test for Kopano license product claims
- Expose a function to load license claims without key validation
- Fix refactoring mistake for license check when offline
- Trim white space from license files when loading
- Refactor license folder loader to module
- Improve robustness of kustomer key set loader
- Fix groupware.payperuse claim validation specification
- Define license checks for groupware
- Define license checks for Meet
- Use archiver claim as actually implemented
- Fix table issue
- Fix typo in edition
- Add edition for meet licenses
- Update Docker image to latest versions


## v0.4.1 (2020-09-29)

- Add exclusive indicator for product license claims
- Update Jenkins reporting plugin from checkstyle to recordIssuesg
- Fix slice claim aggregation
- Add plugins


## v0.4.0 (2020-07-27)

- Describe purpose of fields in product claims
- Add support for exclusive claims
- Add support for []string type in product license claims
- Describe x5c header license claim
- Introduce turnaccess field to Meet claim in licenses
- Adjusted for out-of-band comments from [@aroesler](https://stash.kopano.io/aroesler/)
- Add list of Kopano products and product-specific license fields


## v0.3.1 (2020-07-21)

- Set umask 0111 to allow everyone to connect to api socket


## v0.3.0 (2020-07-20)

- Add dn, sin and refactor expiry
- Add support identification number
- Change dsp -> dn
- Fix indentation
- Add human readable license display name field to license, allowing for easier identification for customer.


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

