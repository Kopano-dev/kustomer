# Kopano supported repository access with licenses

Generally supported Kopano releases require a license to be able to download
supported packages. This means the package download requires authentication
which can be provided by adding the license as authentication proof. This
document describes the supported ways how the license can be added.

## Debian

In Debian, the `apt_auth.conf` can be used to provide authentication for
configured apt sources. This means the Kopano supported repositories for the
individual products can be added as normal to the apt sources list and the
authentication is managed at a central location.

We assume the authentication for Kopano repositories is put into
`/etc/apt/auth.conf.d/kopano-supported.conf` and contains the following.

```
machine download.kopano.io login customer password ${license}
```

This assumes there is a single license file for all the products / Kopano
repositories used on this machine. The `${license}` value is the full license
value as a single line text string.

If there are multiple different license files for different products, then
the `apt_auth.conf` needs multiple lines for each repository path with the
corresponding license value.

```
machine download.kopano.io/my/kopano/core login customer password ${core_license}
machine download.kopano.io/my/kopano/meet login customer password ${meet_license}
```

For further details on the `apt_auth.conf` format see the Debian documentation
at https://manpages.debian.org/testing/apt/apt_auth.conf.5.en.html or
`man apt_auth.conf` on your system.

## Docker

To login into docker you need your groupware or meet license.

```
docker login registry.kopano.com
Username: customer
Password: ${license}
```

## Kubernetes

First create a docker-registry secret

```
kubectl create secret docker-registry kopanoregistry \
  --docker-server=registry.kopano.com \
  --docker-username=customer  \
  --docker-password=${license} \
```

in the deployment file you need to add
```
 imagePullSecrets:
      - name: kopanoregistry
```

Example
```
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kopano-core
  labels:
    name: kopano-core
spec:
  replicas: 1
  selector:
    matchLabels:
      name: kopano-core
  template:
    metadata:
      labels:
        name: kopano-core
    spec:-
      containers:
       - name:  kopano-core
         image: registry.kopano.com/kopano
         imagePullPolicy: "Always"
      imagePullSecrets:
       - name: kopanoregistry
```
