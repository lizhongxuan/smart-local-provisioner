# Storage Provisioner

## Overview

Local Path Provisioner provides a way for the Kubernetes users to utilize the local storage in each node. Based on the user configuration, the Local Path Provisioner will create `hostPath` based persistent volume on the node automatically. It utilizes the features introduced by Kubernetes [Local Persistent Volume feature](https://kubernetes.io/blog/2018/04/13/local-persistent-volumes-beta/), but make it a simpler solution than the built-in `local` volume feature in Kubernetes.

## Compare to built-in Local Persistent Volume feature in Kubernetes

### Pros
Dynamic provisioning the volume using hostPath.
* Currently the Kubernetes [Local Volume provisioner](https://github.com/kubernetes-sigs/sig-storage-local-static-provisioner) cannot do dynamic provisioning for the local volumes.

### Cons
1. No support for the volume capacity limit currently.
    1. The capacity limit will be ignored for now.

## Requirement
Kubernetes v1.12+.

## Configuration

### Customize the ConfigMap

The configuration of the provisioner is a json file `config.json`, a Pod template `helperPod.yaml` and two bash scripts `setup` and `teardown`, stored in a config map, e.g.:
```
kind: ConfigMap
apiVersion: v1
metadata:
  name: local-path-config
  namespace: local-path-storage
data:
  config.json: |-
        {
                "nodePathMap":[
                {
                        "node":"DEFAULT_PATH_FOR_NON_LISTED_NODES",
                        "paths":["/opt/local-path-provisioner"]
                },
                {
                        "node":"yasker-lp-dev1",
                        "paths":["/opt/local-path-provisioner", "/data1"]
                },
                {
                        "node":"yasker-lp-dev3",
                        "paths":[]
                }
                ]
        }
  setup: |-
        #!/bin/sh
        set -eu
        mkdir -m 0777 -p "$VOL_DIR"
  teardown: |-
        #!/bin/sh
        set -eu
        rm -rf "$VOL_DIR"
  helperPod.yaml: |-
        apiVersion: v1
        kind: Pod
        metadata:
          name: helper-pod
        spec:
          containers:
          - name: helper-pod
            image: busybox

```

#### `config.json`

##### Definition
`nodePathMap` is the place user can customize where to store the data on each node.
1. If one node is not listed on the `nodePathMap`, and Kubernetes wants to create volume on it, the paths specified in `DEFAULT_PATH_FOR_NON_LISTED_NODES` will be used for provisioning.
2. If one node is listed on the `nodePathMap`, the specified paths in `paths` will be used for provisioning.
    1. If one node is listed but with `paths` set to `[]`, the provisioner will refuse to provision on this node.
    2. If more than one path was specified, the path would be chosen randomly when provisioning.

##### Rules
The configuration must obey following rules:
1. `config.json` must be a valid json file.
2. A path must start with `/`, a.k.a an absolute path.
2. Root directory(`/`) is prohibited.
3. No duplicate paths allowed for one node.
4. No duplicate node allowed.

#### Scripts `setup` and `teardown` and the `helperPod.yaml` template

* The `setup` script is run before the volume is created, to prepare the volume directory on the node.
* The `teardown` script is run after the volume is deleted, to cleanup the volume directory on the node.
* The `helperPod.yaml` template is used to create a helper Pod that runs the `setup` or `teardown` script.

The scripts receive their input as environment variables:

| Environment variable | Description |
| -------------------- | ----------- |
| `VOL_DIR` | Volume directory that should be created or removed. |
| `VOL_MODE` | The PersistentVolume mode (`Block` or `Filesystem`). |
| `VOL_SIZE_BYTES` | Requested volume size in bytes. |
