# Kopano license CA and signing

This document defines a way how to manage a CA for signing licenses and how
to utilize the kustomerd claims generator to create and sign licenses.

This document uses the [Step CLI](https://github.com/smallstep/cli) utility for
all related to crypto (`step-cli` in $PATH).

## Certificate authority

The CA is the topmost self signed root certificate. It is valid for a long time
and used to create intermediate CA's which are then used to sign the license
data.

### Create Root CA

```
step-cli certificate create "Test Root CA (2020)" test-root-ca.crt test-root-ca.key --profile=root-ca --kty=OKP --crv=Ed25519 --not-after=$(date -I --date="+10 years")T00:00:00Z --not-before=$(date -I --date="today")T00:00:00Z
```

### Create signer certificate and sign with Root CA

This is a leaf certificate, to allow the certificate to be used for signatures.

```
step-cli certificate create "Test License Signer 1 (2020)" test-license-signer-1-2020.crt test-license-signer-1-2020.key --profile=leaf --ca=./test-root-ca.crt --ca-key=./test-root-ca.key --kty=OKP --crv=Ed25519 --not-after=$(date -I --date="+2 years")T00:00:00Z --not-before=$(date -I --date="today")T00:00:00Z
```

### Extract public key as JWK from intermediate CA cert and add it to JWKS

```
step-cli crypto jwk create test-license-signer-1-2020.jwk test-license-signer-1-2020.private.jwk --from-pem=test-license-signer-1-2020.crt --kid=test-license-signer-1-2020
cat test-license-signer-1-2020.jwk | step-cli crypto jwk keyset add test-license-signing.jwks
```


## Create and sign license

To simplify the creation of the license claims, kustomerd offers a claims
generator API (/api/v1/claims-gen) which can be easily used with `curl`.

```
curl -s --unix-socket /run/kopano-kustomerd/api.sock 'http://localhost/api/v1/claims-gen?myproduct-a.' | step-cli crypto jwt sign --key=test-license-signer-1-2020.key --kid=test-license-signer-1-2020 --sub="kustomer-42" --iss="kopano" --aud="kopano" --exp=$(date --date="+1 year 00:00:00Z" "+%s") --iat=$(date --date="today 00:00:00Z" "+%s") --nbf=$(date --date="today 00:00:00Z" "+%s") > myproduct-a-kustomer-42.license
```

All the license relevant claims (like sub, iat, nbf, kid) are defined when
signing the claims with the `step-cli` utility. For details how to add claims
below.

### Sign license for offline use

In special cases, it might be required to issue a license which can be validated
offline. To do this, the leaf certificate used to sign the license is included
in the license. Simply add the `--x5c-cert` parameter to the command above
(`step-cli crypto sign ...`) and point it to the location of the certificate
used to sign the key.

```
... --x5c-cert=test-license-signer-1-2020.crt
```

If that field is present, and its not possible to fetch the key set remotely,
the certificate is used to do a local validation.

### Fixed claims-gen parameters

The following parameters are controlling the top level claims of the license and
cam be passed as query parameters.

| Parameter | Value | Description |
| --------- | ----- | ----------- |
| uid       |       | Unique Kopano license file ID, if not given a random value is generated |


### Extra dynamic claims-gen parameters

All other parameters are product bound using the `prefix.key:type=value` format.
For example the query parameter `myproduct.` just enables the product with a
random `lid` value for that product. Equally `myproduct.lid=custom-lid` can be
used to set a custom id. Adding the type is optional and defaults to `string`
type.

| Type | Description |
| ---- | ----------- |
| string | String type (default when not given) |
| int | 64-bit integer (base 10) |
| float | 64-bit float |
| bool | boolean, converts values like 0, 1, true, false to their corresponding boolean value |

All keys with the same prefix end up in the same product key in the license
claims. Duplicated keys are not supported.


## Inspect and validate license

```
cat myproduct-a-kustomer-42.license | step-cli crypto jwt verify --iss=kopano --aud=kopano --key test-license-signer-1-2020.jwk
```



