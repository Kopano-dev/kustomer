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
https://tools.ietf.org/html/rfc7519.

### Example JWT license

```
eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJrb3Bhbm8iLCJleHAiOjE1NzA4MjUyOTIsImlhdCI6MTU2ODIzMzI5Miwic3ViIjoicGF3ZWxkZWJpa0BnbWFpbC5jb20iLCJrIjp7InYiOjAsInByb2R1Y3RzIjp7Imt3bXNlcnZlciI6eyJ1c2VycyI6NTAsImdyb3VwcyI6MTB9fX19.kfwFR593Jxi7Nk2uNGBRvbvaW0rNcI_Beud6ozFwyNceqQuX79ecgmskxK-w-YaHqHL6LFEtt8GwVvA2GD015g
```

And in its plain form (JOSE header and claims set):

```
{
  "typ": "JWT",
  "alg": "ES256",
  "kid": "kopano-license-201910-1"
}
```
```
{
  "iss": "kopano",
  "exp": 1570825292,
  "iat": 1568233292,
  "sub": "8ac418b0-d3f2-48c6-a426-cdc36d2f46ab",
  "uid": "26b55267-894e-4deb-bcf3-057f229780f0",
  "k": {
    "v": 0,
    "products": {
      "kwmserver": {
        "lid": "50220dde-ca4d-428c-8bc1-c987c8210869",
        "users": 50,
        "groups": 10
      }
    }
  }
}
```

### JWT license fields

| Key            | Value  | Description
| -------------- | ------ | -----------------------------------
| typ  (header)  | JWT    | License type, always JWT
| alg  (header)  | ES256  | JSON Web Algorithm (JWA)
| iss            | kopano | Issuer identifier
| sub            |        | Customer ID or customer email
| exp            |        | Expiration time
| nbf            |        | Not before time
| iat            |        | Issued at time
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
| lid            |        | Unique Kopano license ID
| ...            |        | All other fields are specific to the product
