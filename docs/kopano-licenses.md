# Kopano Licenses

This document defines Kopano license format and the rules how they are loaded
and aggregated.

## License aggregation and replacement rules

Generally a system can have more than one license active. The following rules
apply when there are multiple licenses:

1. License files are only considered if they are valid when looking at the date
   and time claims (`iat`, `nbf`, `exp`). Means current date must be within
   `nbf` and `exp`. If `nbf` is not present, then `iat` applies as minimal date.
2. License files have the`uid` claim and only one license with the same `uid`
   claim value will be active. The active license is the one with the newest
   `iat` claim value.
3. Licenses containing `products` entries for the same product are aggregated
   when the `lid` claim is different. Product entries with the same `lid` claim
   replace each other following the same rules specified for the license file
   itself.

If the license file is not signed or if the signature is invalid, the product
specific claims are ignored and trial settings will be used by licensed
products.

Similarly, if all found licenses for a particular product are expired or not
valid yet, trial settings with be assumed.

## JWT license format

Kopano licenses can be issued as a JSON Web Token. This format contains
cryptographically secured license information signed by Kopano. JWT licenses
can contain license information for multiple Kopano products together with an
Kopano specific unique identifier for each customer and date/time information
when the license is valid. Generally JWT licenses expire and have to be
renewed regularly. Detail specification of the license format is found at
https://tools.ietf.org/html/rfc7519 and https://tools.ietf.org/html/rfc7517.

### Example JWT license

```
eyJhbGciOiJFZERTQSIsImtpZCI6InNpbW9uLXRlc3QtbGljZW5zZS1zaWduaW5nLWNhLTEtMjAyMCIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJrb3Bhbm8iLCJleHAiOjE2MjU1Mjk2MDAsImlhdCI6MTU5Mzk5MzYwMCwiaXNzIjoia29wYW5vIiwianRpIjoiODRkMWI4NjI2M2Q4YWIyMGU3ZWY5OTIzYTliYzFlMjQxMWJlMDUwMmJkMDBmMzQ5OWM4MjlmZDkzY2Y4YmE3YSIsImsiOnsicHJvZHVjdHMiOnsia3dtc2VydmVyIjp7Imdyb3VwcyI6MTAsImxpZCI6ImUzNDc0MjQ1LTNhYzQtNGJkYy04ZDQwLTQ3ZmRlYWM2M2QwOCIsInVzZXJzIjo1MH19LCJ2IjowfSwibmJmIjoxNTkzOTkzNjAwLCJzdWIiOiI4YWM0MThiMC1kM2YyLTQ4YzYtYTQyNi1jZGMzNmQyZjQ2YWIiLCJ1aWQiOiIyMTQ4M2FjOC1jMDc0LTQ1ZmYtODYyOC1mYmUxNGFmYTg4NmQifQ.ftWUUH27yKnFBtIvcHUxXgI7OPD90Gkv2YEkOqmuAdStPDV4m7IsUkOjvWPvk5x4sZ47W8xqRe8BFN3yLsSXDA
```

And in its plain form (JOSE header and claims set):

```
{
  "alg": "EdDSA",
  "kid": "test-license-signing-ca-1-2020",
  "typ": "JWT"
}
```
```
{
  "aud": "kopano",
  "exp": 1625529600,
  "iat": 1593993600,
  "iss": "kopano",
  "jti": "84d1b86263d8ab20e7ef9923a9bc1e2411be0502bd00f3499c829fd93cf8ba7a",
  "k": {
    "products": {
      "kwmserver": {
        "groups": 10,
        "lid": "e3474245-3ac4-4bdc-8d40-47fdeac63d08",
        "users": 50
      }
    },
    "v": 0
  },
  "nbf": 1593993600,
  "sub": "8ac418b0-d3f2-48c6-a426-cdc36d2f46ab",
  "uid": "21483ac8-c074-45ff-8628-fbe14afa886d"
}
```

### JWT license fields

| Key            | Value  | Description
| -------------- | ------ | -----------------------------------
| typ  (header)  | JWT    | License type, always JWT
| alg  (header)  | ES256  | JSON Web Algorithm (JWA)
| x5c  (header)  |        | Array of certificate value strings for offline validation (optional)
| iss            | kopano | Issuer identifier (must be kopano)
| aud            | kopano | Audience (must be kopano)
| sub            |        | Customer ID or customer email
| dn             |        | Human readable license display name (e.g. customer name)
| sin            |        | Support identification number
| exp            |        | Expiration time
| nbf            |        | Not before time
| iat            |        | Issued at time
| jti            |        | Unique ID for this license file
| uid            |        | Unique Kopano license file ID
| k              |        | Kopano license data mapping

### JWT Kopano license data mapping

The license data is found in the `k` claim and contains the following fields.

| Key            | Value  | Description
| -------------- | ------ | ---------------------------------------------
| v              | 0      | Kopano license data version
| products       |        | Kopano licensed product data mapping


### JWT Kopano license product data mapping

The `products` key contains a mapping where the keys identify the licensed Kopano
product and the individual values are specific to that particular product.

| Key            | Value  | Description
| -------------- | ------ | ---------------------------------------------
| lid            |        | (string) Unique Kopano license ID
| exclusive      |        | ([]string) list of claims flagged as exclusive
| ...            |        | All other fields are specific to the product

Licenses can use the `exclusive` claim to mark individual product claims to be
exclusive. When such a claim is exclusive, all other licenses for the same
product claim must have the exact same value as the license claim value for
the oldest valid license defining that product claim as exclusive. If such a
product claim value is not having an exclusive value, none of the license claims
of the product of the corresponding license is activated until the other older
license with a different value for the exclusive claim is removed.

Product specific claims that can be marked as `exclusive` are indicated in the table below.

### Kopano product specific fields

The following products and product-specific fields/claims are valid for use in the licenses.

| Product name     | Claim       | Accepted values                                        | Exclusive? | Description of purpose
| ---------------- | ----------- | ------------------------------------------------------ | ---------- | --------------------------------------------------------------------------------------------------------------------
| **groupware**    |             |                                                        |            |
|                  | edition     | (string) basic, professional, enterprise               |            | The purchased groupware edition (Basic, Professional or Enterprise)
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of active users (users who can log in)
|                  | max-shared  | (integer) 0..999999                                    |            | The maximum number of shared mailboxes
|                  | multiserver | (boolean)                                              | Yes        | Multi server allowed
|                  | multitenant | (boolean)                                              | Yes        | Multi tenant allowed
|                  | payperuse   | (boolean)                                              | Yes        | Pay per use defines whether the installation is a 'hosted' installation, invoiced based on actual usage
|                  | archiver    | (boolean)                                              | Yes        | Archiver allowed
| **meet**         |             |                                                        |            |
|                  | edition     | (string) starter, enterprise                           |            | The purchased Meet edition (Starter or Enterprise)
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of users with a Meet account
|                  | max-groups  | (integer) 0..999999                                    |            | The maximum number of simultaneous group meetings
|                  | guests      | (boolean)                                              | Yes        | Are guest users allowed in this Meet instance
|                  | sfu         | (boolean)                                              | Yes        | Is usage of the SFU allowed in this Meet instance
|                  | webinars    | (boolean)                                              | Yes        | Is usage of the webinar feature allowed in this Meet instance
|                  | turnaccess  | (boolean)                                              | Yes        | Can this subscription be used with the Kopano TURN server?
| **webapp-meet**  |             |                                                        |            |
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of users allowed to use with the meet integration plugin
| **webapp-files** |             |                                                        |            |
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of users allowed to use with the files plugin
| **webapp-smime** |             |                                                        |            |
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of users allowed to use with the smime plugin
| **webapp-mdm**   |             |                                                        |            |
|                  | max-users   | (integer) 0..999999                                    |            | The maximum number of users allowed to use with the mdm plugin

### Kopano product license checks

The license claims are checked by supported builds of the corresponding Kopano
software.

#### Kopano Meet (**meet**)

Meet licenses are validated by kwmserver. On startup and on any change of the
active license set, kwmserver checks the `max-groups`, `guests`, `sfu`,
`turnaccess` **meet** claims of the active license set with the current usage.
If the current usage is not covered, a warning is logged (no functionality is
limited).

The license status is also made available to connected clients. Meet is
displaying a snack to all connected users (including guests) if the current
usage is not covered by the active license set.

The **meet** claims `max-users` and `webinars` are currently ignored.
